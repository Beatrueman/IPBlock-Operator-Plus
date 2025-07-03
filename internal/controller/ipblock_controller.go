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

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github/Beatrueman/ipblock-operator/internal/engine"
	"github/Beatrueman/ipblock-operator/internal/notify"
	"github/Beatrueman/ipblock-operator/internal/policy"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	opsv1 "github/Beatrueman/ipblock-operator/api/v1"
)

// IPBlockReconciler reconciles a IPBlock object
const (
	LOG_LEVEL = 2
)

type IPBlockReconciler struct {
	client.Client                      // 客户端通信
	Scheme        *runtime.Scheme      // 序列化和反序列化
	Recorder      record.EventRecorder // Event记录器
	Adapter       engine.Adapter       // 封禁适配器接口
	AdapterName   string
	GatewayHost   string
	CmName        string
	CmNamespace   string
	Whitelist     *policy.Whitelist // ConfigMap读取
	mu            sync.RWMutex      // 读写锁
	Notifier      notify.Notifier   // 通知接口
}

func (r *IPBlockReconciler) UpdateWhitelist(wl *policy.Whitelist) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Whitelist = wl
}

func (r *IPBlockReconciler) UpdateGatewayHost(newHost string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.GatewayHost = newHost
}

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=ops.yiiong.top,resources=ipblocks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ops.yiiong.top,resources=ipblocks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ops.yiiong.top,resources=ipblocks/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the IPBlock object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
// 处理CRD各种事件的具体业务逻辑, req包含标识当前对象的信息：名称和命名空间
func (r *IPBlockReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	var ipblock opsv1.IPBlock
	if err := r.Get(ctx, req.NamespacedName, &ipblock); err != nil {
		logger.Error(err, "无法获取 IPBlock 资源", "name", req.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ip := ipblock.Spec.IP

	// 初始化 Phase 为 pending
	if ipblock.Status.Phase == "" {
		ipblock.Status.Phase = "pending"
		_ = r.Status().Update(ctx, &ipblock)
	}

	// ==== Step 1: 手动解封优先处理 ====
	if ipblock.Spec.Unblock {
		if ipblock.Status.Result == "unblocked" {
			logger.V(LOG_LEVEL).Info("已手动解封，跳过重复处理", "ip", ip)
			return ctrl.Result{}, nil
		}
		msg, err := r.Adapter.UnBan(ip)
		if err != nil {
			logger.Error(err, "手动解封失败", "ip", ip)
			r.UpdateIPBlockStatus(ctx, &ipblock, func(obj *opsv1.IPBlock) {
				obj.Status.Result = "failed"
				obj.Status.Message = "手动解封失败: " + err.Error()
			})
			// 错误通知
			if r.Notifier != nil {
				go func() {
					err := r.Notifier.Notify(ctx, "common", map[string]string{
						"alarm_time": time.Now().Format("2006-01-02 15:04:05"),
						"msg":        err.Error(),
					})
					if err != nil {
						logf.Log.Error(err, "通知失败")
					}
				}()
			}
		} else {
			logger.Info("手动解封成功", "ip", ip)
			r.Recorder.Event(&ipblock, corev1.EventTypeNormal, "ManualUnblock", "IP manually unblocked")
			r.UpdateIPBlockStatus(ctx, &ipblock, func(obj *opsv1.IPBlock) {
				obj.Status.Result = "unblocked"
				obj.Status.Message = msg
				obj.Status.UnblockedAt = time.Now().Format(time.RFC3339)
				obj.Status.Phase = "expired"
			})
			if r.Notifier != nil {
				go func() {
					err := r.Notifier.Notify(ctx, "resolve", map[string]string{
						"alarm_time": time.Now().Format("2006-01-02 15:04:05"),
						"ip":         ip,
					})
					if err != nil {
						logf.Log.Error(err, "发送解封通知失败", "ip", ip)
					}
				}()
			}
		}

		// 先保存原始副本
		patch := client.MergeFrom(ipblock.DeepCopy())

		ipblock.Spec.Unblock = false

		// Patch 更新 Spec
		if err := r.Patch(ctx, &ipblock, patch); err != nil {
			logger.Error(err, "Patch 更新 Spec（清除 unblock）失败")
			return ctrl.Result{}, err
		}

		statusPatch := client.MergeFrom(ipblock.DeepCopy())

		if err := r.Status().Patch(ctx, &ipblock, statusPatch); err != nil {
			logger.Error(err, "Patch 更新 Status（手动解封）失败")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// ==== Step 2: 白名单跳过（只在非 trigger 情况下判断）====
	if r.Whitelist != nil && r.Whitelist.IsWhitelisted(ip) {
		if ipblock.Status.Phase != "skipped" {
			r.Recorder.Event(&ipblock, corev1.EventTypeNormal, "WhitelistSkip", fmt.Sprintf("IP %s is in whitelist", ip))
			r.UpdateIPBlockStatus(ctx, &ipblock, func(obj *opsv1.IPBlock) {
				obj.Status.Phase = "skipped"
				obj.Status.Result = "skipped"
				obj.Status.Message = "IP is whitelisted, skipping ban"
			})
		}
		logger.V(1).Info("跳过封禁，目标 IP 在白名单中", "ip", ip)
		return ctrl.Result{}, nil
	}

	// ==== Step 3: 手动强制封禁 ====
	triggered := false
	if ipblock.Spec.Trigger {
		logger.Info("触发强制重新封禁")
		ipblock.Spec.Trigger = false
		triggered = true
		_ = r.Update(ctx, &ipblock)
	}

	// ==== Step 4: 幂等判断（状态无变化 + 未触发）====
	currentHash, err := HashSpec(ipblock.Spec)
	if err != nil {
		logger.Error(err, "计算哈希失败")
		return ctrl.Result{}, err
	}

	if ipblock.Status.Phase != "pending" && ipblock.Status.LastSpecHash == currentHash && !triggered {
		switch ipblock.Status.Phase {
		case "active":
			logger.V(LOG_LEVEL).Info("IP 已封禁，跳过", "ip", ip)
		case "expired":
			logger.V(LOG_LEVEL).Info("IP 已解封，未变更", "ip", ip)
		case "skipped":
			logger.V(LOG_LEVEL).Info("IP 已跳过，未变更", "ip", ip)
		default:
			logger.V(LOG_LEVEL).Info("状态已处理，跳过", "phase", ipblock.Status.Phase)
		}
		return ctrl.Result{}, nil
	}

	// ==== Step 5: 封禁操作 ====
	logger.Info("处理封禁请求", "ip", ip, "reason", ipblock.Spec.Reason, "duration", ipblock.Spec.Duration)

	isPermanent := ipblock.Spec.Duration == ""
	var banSeconds int
	if !isPermanent {
		dur, err := time.ParseDuration(ipblock.Spec.Duration)
		if err != nil {
			r.UpdateIPBlockStatus(ctx, &ipblock, func(obj *opsv1.IPBlock) {
				obj.Status.Phase = "failed"
				obj.Status.Result = "failed"
				obj.Status.Message = "非法 duration: " + err.Error()
			})
			return ctrl.Result{}, err
		}
		banSeconds = int(dur.Seconds())
	}

	if r.Adapter == nil {
		logger.Error(nil, "Adapter 未初始化，无法封禁 IP")
		return ctrl.Result{}, nil
	}

	result, err := r.Adapter.Ban(ip, isPermanent, banSeconds)
	if err != nil {
		logger.Error(err, "封禁失败", "ip", ip)
		r.UpdateIPBlockStatus(ctx, &ipblock, func(obj *opsv1.IPBlock) {
			obj.Status.Result = "failed"
			obj.Status.Message = err.Error()
			obj.Status.Phase = "failed"
		})
		// 错误通知
		if r.Notifier != nil {
			go func() {
				err := r.Notifier.Notify(ctx, "common", map[string]string{
					"alarm_time": time.Now().Format("2006-01-02 15:04:05"),
					"msg":        err.Error(),
				})
				if err != nil {
					logf.Log.Error(err, "通知失败")
				}
			}()
		}
	} else {
		logger.Info("封禁成功", "ip", ip)
		r.Recorder.Event(&ipblock, corev1.EventTypeNormal, "BanSuccess", "IP ban succeeded")
		r.UpdateIPBlockStatus(ctx, &ipblock, func(obj *opsv1.IPBlock) {
			obj.Status.Result = "success"
			obj.Status.Message = result
			obj.Status.BlockedAt = time.Now().Format(time.RFC3339)
			obj.Status.LastSpecHash = currentHash
			obj.Status.Phase = "active"
		})

		// 提取 Reason 中的 count
		countExtra := func(reason string) string {
			parts := strings.Split(ipblock.Spec.Reason, ":")
			if len(parts) < 2 {
				return ""
			}

			count := strings.TrimSpace(parts[1])
			countParts := strings.Fields(count)
			if len(countParts) > 0 {
				return countParts[0]
			}
			return count
		}

		if r.Notifier != nil {
			logger.Info("Notifier found, sending ban notification", "ip", ip)
			err := r.Notifier.Notify(ctx, "ban", map[string]string{
				"alarm_time": time.Now().Format("2006-01-02 15:04:05"),
				"ip":         ip,
				"count":      countExtra(ipblock.Spec.Reason), // 这里填实际count
			})
			if err != nil {
				logger.Error(err, "发送封禁通知失败", "ip", ip)
			} else {
				logger.Info("发送封禁通知成功", "ip", ip)
			}
		} else {
			logger.Info("Notifier is nil, skipping ban notification")
		}

		// 启动自动解封
		if !isPermanent && ipblock.Status.UnblockedAt == "" {
			go r.scheduleAutoUnblock(ipblock.DeepCopy())
		}
	}

	return ctrl.Result{}, nil
}

// 计算当前Spec的Hash
func HashSpec(spec opsv1.IPBlockSpec) (string, error) {
	b, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}

	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h), nil
}

// 用于解决对象版本冲突引发的更新问题
func (r *IPBlockReconciler) UpdateIPBlockStatus(ctx context.Context, ipblock *opsv1.IPBlock, updateFn func(*opsv1.IPBlock)) (*opsv1.IPBlock, error) {
	logger := logf.FromContext(ctx)
	var latest opsv1.IPBlock
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: ipblock.Namespace,
			Name:      ipblock.Name,
		}, &latest); err != nil {
			logger.Error(err, "无法获取最新 IPBlock 状态用于更新", "name", ipblock.Name)
			return err
		}

		updateFn(&latest)
		if err := r.Status().Update(ctx, &latest); err != nil {
			logger.Error(err, "状态更新失败", "name", ipblock.Name)
			return err
		}
		return nil
	})
	if err != nil {
		logger.Error(err, "重试后状态更新失败", "name", ipblock.Name)
	}
	return &latest, nil
}

// 定时解封
func (r *IPBlockReconciler) scheduleAutoUnblock(ipblock *opsv1.IPBlock) {
	durationStr := ipblock.Spec.Duration
	d, err := time.ParseDuration(durationStr)
	if err != nil {
		logf.Log.Error(err, "自动解封失败：duration 无效", "ip", ipblock.Spec.IP)
		return
	}

	logf.Log.Info("启动自动解封计时器", "ip", ipblock.Spec.IP, "duration", durationStr)
	time.Sleep(d)

	ctx := context.Background()
	msg, err := r.Adapter.UnBan(ipblock.Spec.IP)

	r.UpdateIPBlockStatus(ctx, ipblock, func(obj *opsv1.IPBlock) {
		obj.Status.Phase = "expired"
		obj.Status.Result = "unblocked"
		obj.Status.UnblockedAt = time.Now().Format(time.RFC3339)

		ip := obj.Spec.IP
		if err != nil {
			logf.Log.Error(err, "自动解封失败", "ip", ip)
			obj.Status.Message = "解封失败: " + err.Error()
			// 发送事件
			r.Recorder.Event(obj, corev1.EventTypeWarning, "AutoUnblockFailed", obj.Status.Message)
			// 错误通知
			if r.Notifier != nil {
				go func() {
					err := r.Notifier.Notify(ctx, "common", map[string]string{
						"alarm_time": time.Now().Format("2006-01-02 15:04:05"),
						"msg":        err.Error(),
					})
					if err != nil {
						logf.Log.Error(err, "通知失败")
					}
				}()
			}
		} else {
			logf.Log.Info("自动解封成功", "ip", ip)
			obj.Status.Message = msg
			// 发送事件
			r.Recorder.Event(obj, corev1.EventTypeNormal, "AutoUnblockSuccess", "IP 自动解封成功")
			// 解封通知
			if r.Notifier != nil {
				go func() {
					err := r.Notifier.Notify(ctx, "resolve", map[string]string{
						"alarm_time": time.Now().Format("2006-01-02 15:04:05"),
						"ip":         ip,
					})
					if err != nil {
						logf.Log.Error(err, "发送解封通知失败", "ip", ip)
					}
				}()
			}

		}
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *IPBlockReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opsv1.IPBlock{}).
		Named("ipblock").
		Complete(r)
}
