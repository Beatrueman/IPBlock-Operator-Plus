package utils

import "sync"

// IPLock 用于管理 IP 维度的并发锁，防止重复处理
type IPLock struct {
	global sync.Mutex             // global 是一个全局锁，用来保护 locks 这个 map，防止并发读写它时发生竞态。
	locks  map[string]*sync.Mutex // 用来记录每个 IP 对应的独立锁。
}

// 创建新的 IP 锁管理器
func NewIPLock() *IPLock {
	return &IPLock{
		locks: make(map[string]*sync.Mutex),
	}
}

// Lock 获取某个 IP 的锁
func (i *IPLock) Lock(ip string) {
	i.global.Lock()         // 先加全局锁，防止多个 goroutine 同时访问 map
	lock, ok := i.locks[ip] // 查看该 ip 是否已经有锁
	if !ok {
		lock = &sync.Mutex{} // 没有就新建一个锁
		i.locks[ip] = lock   // 加入 map
	}
	i.global.Unlock() // 释放全局锁
	lock.Lock()       // 调用具体 IP 的 lock.Lock()，阻塞等待获取锁
}

// Unlock 释放某个 IP 的锁
func (i *IPLock) Unlock(ip string) {
	i.global.Lock()
	if lock, ok := i.locks[ip]; ok {
		lock.Unlock()
	}
	i.global.Unlock()
}
