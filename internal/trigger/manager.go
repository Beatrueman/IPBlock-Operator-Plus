package trigger

import (
	"context"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sync"
)

var (
	triggerMu sync.RWMutex
	triggers  = make(map[string]Trigger)
)

// 注册 Trigger 实例
func Register(ctx context.Context, t Trigger) {
	logger := logf.FromContext(ctx)
	triggerMu.Lock()
	defer triggerMu.Unlock()

	name := t.Name()
	if _, ok := triggers[name]; ok {
		logger.Info("Trigger already registered, overwriting!")
	} else {
		logger.Info("Registering trigger")
	}
	triggers[name] = t
}

// 启动所有注册的触发器
func StartAll(ctx context.Context) {
	logger := logf.FromContext(ctx)
	triggerMu.Lock()
	defer triggerMu.Unlock()

	for name, t := range triggers {
		go func(name string, t Trigger) {
			logger.Info("Starting trigger", "name", name)
			if err := t.Start(ctx); err != nil {
				logger.Error(err, "Failed to start trigger", "name", name)
			}
		}(name, t)
	}
}

// 停止所有注册的触发器
func StopAll(ctx context.Context) {
	logger := logf.FromContext(ctx)
	triggerMu.Lock()
	defer triggerMu.Unlock()

	for name, t := range triggers {
		logger.Info("Stopping trigger", "name", name)
		if err := t.Stop(ctx); err != nil {
			logger.Error(err, "Failed to stop trigger", "name", name)
		}
	}
}
