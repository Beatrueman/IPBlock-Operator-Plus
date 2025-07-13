package trigger

import (
	"context"
	"encoding/json"
	"fmt"
	utils "github/Beatrueman/ipblock-operator/internal/utils"
	"net/http"
	"sync"

	opsv1 "github/Beatrueman/ipblock-operator/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type GrafanaTrigger struct {
	Client    client.Client
	server    *http.Server
	mu        sync.Mutex
	Addr      string          // 监听地址
	Path      string          // 监听路由，填写在 alert 联络点里
	Debouncer utils.Debouncer // 防抖，防止同个 IP Webhook多次，生成多个相同 IP 的 CR
	IPLocker  *utils.IPLock   // 防止竞争
}

func (g *GrafanaTrigger) Name() string {
	return "grafana"
}

func (g *GrafanaTrigger) Start(ctx context.Context) error {
	logger := logf.FromContext(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc(g.Path, g.handleWebhook)

	g.server = &http.Server{
		Addr:    g.Addr,
		Handler: mux,
	}

	go func() {
		if err := g.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(err, "[grafana] ListenAndServe error")
		}
	}()
	logger.Info("[grafana] Trigger HTTP server started on port" + g.server.Addr)

	// 监听 ctx 结束
	go func() {
		<-ctx.Done()
		_ = g.Stop(context.Background())
	}()

	return nil
}

func (g *GrafanaTrigger) Stop(ctx context.Context) error {
	logger := logf.FromContext(ctx)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.server != nil {
		logger.Info("[grafana] Shutting down HTTP server")
		return g.server.Shutdown(ctx)
	}
	return nil
}

// 告警结构体
type GrafanaAlert struct {
	Alerts []struct {
		Status string                 `json:"status"`
		Labels map[string]string      `json:"labels"`
		Values map[string]interface{} `json:"values"`
	} `json:"alerts"`
}

func (g *GrafanaTrigger) handleWebhook(w http.ResponseWriter, r *http.Request) {
	logger := logf.FromContext(r.Context())
	var payload GrafanaAlert
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	for _, alert := range payload.Alerts {
		if alert.Status != "firing" {
			continue
		}
		ip := alert.Labels["ip"]
		var count string
		if val, ok := alert.Values["A"]; ok {
			count = fmt.Sprintf("%.0f", val.(float64))
		}
		if ip == "" {
			continue
		}

		// 避免并发时的竞争和死锁
		func(ip string) {
			g.IPLocker.Lock(ip)
			defer g.IPLocker.Unlock(ip)
			// 防抖，防止重复创建 CR
			if !g.Debouncer.ShouldAllow(ip) {
				logger.Info("[grafana] Skip duplicate IPBlock within TTL", "ip", ip)
				return
			}

			crName := utils.GenCRName(ip)

			// 先判断 CR 是否已经存在
			var existing opsv1.IPBlock
			err := g.Client.Get(context.Background(), client.ObjectKey{
				Name:      crName,
				Namespace: "default",
			}, &existing)
			if err == nil {
				logger.Info("[grafana] IPBlock already exists, skip creation", "ip", ip)
				return
			}

			// 如果 NotFound，继续创建
			if err != nil && !apierrors.IsNotFound(err) {
				logger.Error(err, "[grafana] Error checking IPBlock existence", "ip", ip)
				return
			}

			// 创建 IPBlock CR
			ipblock := &opsv1.IPBlock{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crName,
					Namespace: "default",
				},
				Spec: opsv1.IPBlockSpec{
					IP:       ip,
					Reason:   "Grafana告警触发，IP在1min内访问次数:" + count,
					Source:   "grafana",
					Duration: "10m",
				},
			}

			if err := g.Client.Create(context.Background(), ipblock); err != nil {
				logger.Error(err, "[grafana] Create IPBlock error", "ip", ip)
			} else {
				logger.Info("[grafana] Created IPBlock successfully", "ip", ip)
			}

		}(ip)

	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))

}
