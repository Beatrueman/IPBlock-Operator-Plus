package utils

import (
	"k8s.io/utils/lru"
	"sync"
	"time"
)

type lruDebouncer struct {
	cache *lru.Cache
	ttl   time.Duration
	mu    sync.Mutex
}

func NewLRUDebouncer(size int, ttl time.Duration) *lruDebouncer {
	c := lru.New(size)
	return &lruDebouncer{
		cache: c,
		ttl:   ttl,
	}
}

// ShouldAllow
// 1.给 cache 加上互斥锁，防止并发请求中对其访问和修改。
// 2.在缓存中检查是否有该 IP 的记录
// 3.有的话，就检查它上次触发时间 ts 和现在 time.Since(ts) 的间隔。
// 4.如果间隔时间小于设定的 TTL，说明是重复触发了，就返回 false，拒绝本次操作。
func (d *lruDebouncer) ShouldAllow(ip string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if ts, ok := d.cache.Get(ip); ok {
		if time.Since(ts.(time.Time)) < d.ttl {
			return false
		}
	}
	d.cache.Add(ip, time.Now())
	return true
}
