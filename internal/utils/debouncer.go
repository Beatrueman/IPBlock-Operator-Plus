package utils

type Debouncer interface {
	// 返回是否允许本次处理
	ShouldAllow(key string) bool
}
