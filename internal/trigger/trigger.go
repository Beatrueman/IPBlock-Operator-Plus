package trigger

import (
	"context"
)

// Trigger 是封禁事件的触发器接口，Start 启动监听任务， Stop
type Trigger interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
