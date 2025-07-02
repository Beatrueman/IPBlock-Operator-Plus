/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"github/Beatrueman/ipblock-operator/internal/config"
	"github/Beatrueman/ipblock-operator/internal/engine"
	"github/Beatrueman/ipblock-operator/internal/notify/lark"
	"github/Beatrueman/ipblock-operator/internal/trigger"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/cache"
	"os"
	"path/filepath"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	opsv1 "github/Beatrueman/ipblock-operator/api/v1"
	"github/Beatrueman/ipblock-operator/internal/controller"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(opsv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

type TriggerConfig struct {
	Name string `yaml:"name"`
	Addr string `yaml:"addr,omitempty"`
	Path string `yaml:"path,omitempty"`
}

// 解析 trigger 字符串为 YAML 列表
func parseTriggers(yamlStr string) ([]TriggerConfig, error) {
	var triggers []TriggerConfig
	err := yaml.Unmarshal([]byte(yamlStr), &triggers)
	if err != nil {
		return nil, err
	}
	return triggers, nil
}

// 选择触发器
func CreateTriggerByConfig(cfg TriggerConfig, mgr ctrl.Manager) trigger.Trigger {
	switch cfg.Name {
	case "grafana":
		return &trigger.GrafanaTrigger{
			Client: mgr.GetClient(),
			Addr:   cfg.Addr,
			Path:   cfg.Path,
		}
	// TODO 其他触发器 ...
	default:
		return nil
	}
}

func watchConfigMap(ctx context.Context, mgr ctrl.Manager, reconciler *controller.IPBlockReconciler) {
	go func() {
		watcher, err := mgr.GetCache().GetInformer(ctx, &corev1.ConfigMap{})
		if err != nil {
			log.Log.Error(err, "Failed to create ConfigMap watcher")
			return
		}

		// 加载通知中心
		loadNotify := func(cm *corev1.ConfigMap) {
			notifyType := cm.Data["notifyType"]
			webhookURL := cm.Data["notifyWebhookURL"]

			templates := make(map[string]string)
			for k, v := range cm.Data {
				if strings.HasPrefix(k, "notifyTemplate_") {
					eventType := strings.TrimPrefix(k, "notifyTemplate_")
					templates[eventType] = v
				}
			}

			if notifyType == "lark" && webhookURL != "" && len(templates) > 0 {
				larkNotify, err := lark.NewLarkNotify(webhookURL, templates)
				if err != nil {
					log.Log.Error(err, "Failed to create LarkNotify instance")
					reconciler.Notifier = nil
					return
				}
				reconciler.Notifier = larkNotify
				log.Log.Info("LarkNotify has been initialized")
				return
			}

			//TODO 其他通知方式...

			reconciler.Notifier = nil

			log.Log.Info("notifyType: " + notifyType)
			log.Log.Info("No valid notify config found, notifications disabled")
		}

		// 加载触发中心
		loadTriggers := func(cm *corev1.ConfigMap) {
			triggersYaml, ok := cm.Data["trigger"]
			if !ok || len(triggersYaml) == 0 {
				log.Log.Info("No triggers configured, skip")
				return
			}

			triggerConfigs, err := parseTriggers(triggersYaml)
			if err != nil {
				log.Log.Error(err, "Failed to parse triggers")
				return
			}

			trigger.StopAll(ctx)

			for _, cfg := range triggerConfigs {
				t := CreateTriggerByConfig(cfg, mgr)
				if t != nil {
					trigger.Register(ctx, t)
					log.Log.Info("Registered trigger", "name", cfg.Name)
				} else {
					log.Log.Info("Unknown trigger, skipping", "name", cfg.Name)
				}
			}

			trigger.StartAll(ctx)
		}

		// 使用controller-runtime提供的事件监听器(Informer)，注册一个"当资源发生变化时需要执行的函数"
		watcher.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				newCm := obj.(*corev1.ConfigMap)
				if newCm.Name == reconciler.CmName && newCm.Namespace == reconciler.CmNamespace {
					if host := newCm.Data["gatewayHost"]; host != "" {
						log.Log.Info("ConfigMap has been created", "newGatewayHost", host)
						reconciler.UpdateGatewayHost(host)
					}
					if wl := config.LoadWhitelistFromConfigMap(newCm); wl != nil {
						reconciler.UpdateWhitelist(wl)
						log.Log.Info("Whitelist has been initialized", "whitelist", wl.StringSlice())
					}
					if name := newCm.Data["engine"]; name != "" {
						reconciler.AdapterName = name
						reconciler.Adapter = engine.NewAdapter(name, reconciler.GatewayHost)
						log.Log.Info("Adapter has been initialized", "name", name)
					}
					// 加载触发器
					loadTriggers(newCm)
					// 加载 Notify 相关配置
					loadNotify(newCm)

				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				newCm := newObj.(*corev1.ConfigMap)
				if newCm.Name == reconciler.CmName && newCm.Namespace == reconciler.CmNamespace {
					// 热更新gatewayhost
					if newHost := newCm.Data["gatewayHost"]; newHost != "" {
						log.Log.Info("ConfigMap has been updated", "newGatewayHost", newHost)
						reconciler.UpdateGatewayHost(newHost)
					}

					if wl := config.LoadWhitelistFromConfigMap(newCm); wl != nil {
						reconciler.UpdateWhitelist(wl)
						log.Log.Info("whitelist has been updated", "whitelist", wl.StringSlice())
					}

					if name := newCm.Data["engine"]; name != "" {
						reconciler.AdapterName = name
						reconciler.Adapter = engine.NewAdapter(name, reconciler.GatewayHost)
						log.Log.Info("Adapter has been updated", "name", name)
					}
					// 加载触发器
					loadTriggers(newCm)
					loadNotify(newCm)
				}
			},
		})
	}()
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		var err error
		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(webhookCertPath, webhookCertName),
			filepath.Join(webhookCertPath, webhookCertKey),
		)
		if err != nil {
			setupLog.Error(err, "Failed to initialize webhook certificate watcher")
			os.Exit(1)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: webhookTLSOpts,
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(metricsCertPath, metricsCertName),
			filepath.Join(metricsCertPath, metricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "e03e04e1.yiiong.top",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// 1.注册并启动 Reconciler
	reconciler := &controller.IPBlockReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		Recorder:    mgr.GetEventRecorderFor("ipblock-operator"),
		CmName:      "ipblock-operator-config",
		CmNamespace: "default",
	}

	ctx := context.Background()
	watchConfigMap(ctx, mgr, reconciler)

	if err := reconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "IPBlock")
		os.Exit(1)
	}

	//// 2.注册 Trigger
	//gTrigger := &trigger.GrafanaTrigger{
	//	Client: mgr.GetClient(),
	//}
	//trigger.Register(ctx, gTrigger)
	//
	//// 3.启动 Trigger
	//trigger.StartAll(ctx)

	// 4.启动 Manager， 阻塞主线程
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	if metricsCertWatcher != nil {
		setupLog.Info("Adding metrics certificate watcher to manager")
		if err := mgr.Add(metricsCertWatcher); err != nil {
			setupLog.Error(err, "unable to add metrics certificate watcher to manager")
			os.Exit(1)
		}
	}

	if webhookCertWatcher != nil {
		setupLog.Info("Adding webhook certificate watcher to manager")
		if err := mgr.Add(webhookCertWatcher); err != nil {
			setupLog.Error(err, "unable to add webhook certificate watcher to manager")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
