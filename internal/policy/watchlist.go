package policy

import (
	"log"
	"net"
)

// 白名单机制
// 单IP白名单
// CIDR白名单
// 标签匹配

type Whitelist struct {
	ipNets []*net.IPNet // IPNet的指针切片，保存CIDR网段
	ips    []net.IP     // 精确IP白名单
}

func NewWhitelist(ipList []string) *Whitelist {
	// 初始化空结构体
	w := &Whitelist{
		ipNets: make([]*net.IPNet, 0),
		ips:    make([]net.IP, 0),
	}

	for _, ip := range ipList {
		if _, ipNet, err := net.ParseCIDR(ip); err == nil {
			w.ipNets = append(w.ipNets, ipNet)
		} else if parsed := net.ParseIP(ip); parsed != nil {
			w.ips = append(w.ips, parsed)
		} else {
			log.Printf("忽略无效白名单项: %s", ip)
		}
	}
	return w
}

func (w *Whitelist) IsWhitelisted(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, ipnet := range w.ipNets {
		if ipnet.Contains(parsed) {
			return true
		}
	}
	for _, i := range w.ips {
		if i.Equal(parsed) {
			return true
		}
	}
	return false
}

// 打印所有白名单内容
func (w *Whitelist) StringSlice() []string {
	list := make([]string, 0)

	// 遍历单个 IP
	for _, ip := range w.ips {
		list = append(list, ip.String())
	}

	// 遍历 CIDR
	for _, ipNet := range w.ipNets {
		list = append(list, ipNet.String())
	}

	return list
}
