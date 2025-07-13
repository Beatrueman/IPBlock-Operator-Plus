package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

type XDPAdapter struct {
	GatewayHost string
}

func (xdp *XDPAdapter) Ban(ip string, isPermanent bool, durationSeconds int) (string, error) {
	banType := 0 // 默认暂时封禁
	if isPermanent {
		banType = 1
	}

	// 构造url
	url := fmt.Sprintf("http://%s/update?cidr=%s&ban_type=%d&ban_time=%d", xdp.GatewayHost, ip, banType, durationSeconds)

	log.Printf("调用封禁接口: %s", url)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("调用封禁接口失败: %v", err)
		return "", err
	}

	defer resp.Body.Close()

	var result struct {
		Message string `json:"message"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("解析返回信息失败: %v", err)
		return "", fmt.Errorf("解析返回信息失败: %v", err)
	}

	msg := result.Message
	if strings.Contains(msg, "Successfully added") || strings.Contains(msg, "have been updated") {
		return msg, nil
	}

	return msg, fmt.Errorf("封禁失败: %s", msg)
}

// 解封接口
func (xdp *XDPAdapter) UnBan(ip string) (string, error) {
	url := fmt.Sprintf("http://%s/remove?cidr=%s", xdp.GatewayHost, ip)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	var result struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	msg := result.Message
	if strings.Contains(msg, "Successfully removed") || strings.Contains(msg, "not exists") {
		return msg, nil
	}
	return msg, fmt.Errorf("解封失败：%s", msg)

}
