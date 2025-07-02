package engine

//
import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type IptablesAdapter struct {
	GatewayHost string
}

func (iptables *IptablesAdapter) Ban(ip string, isParmanent bool, durationSeconds int) (string, error) {
	// 构造url
	url := fmt.Sprintf("http://%s/limit?ip=%s", iptables.GatewayHost, ip)

	log.Printf("调用限流接口: %s", url)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("调用限流接口失败: %v", err)
		return "", err
	}

	defer resp.Body.Close()

	var result struct {
		IP     string
		Status string
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("解析返回信息失败: %v", err)
		return "", fmt.Errorf("解析返回信息失败: %v", err)
	}

	if result.Status == "limited" {
		return result.Status, nil
	}

	return result.Status, fmt.Errorf("限流失败: %s", result.Status)
}

func (iptables *IptablesAdapter) UnBan(ip string) (string, error) {
	// 构造url
	url := fmt.Sprintf("http://%s/unlimit?ip=%s", iptables.GatewayHost, ip)

	log.Printf("调用解限流接口: %s", url)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("调用解限流接口失败: %v", err)
		return "", err
	}

	defer resp.Body.Close()

	var result struct {
		IP     string
		Status string
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("解析返回信息失败: %v", err)
		return "", fmt.Errorf("解析返回信息失败: %v", err)
	}

	if result.Status == "unlimited" {
		return result.Status, nil
	}

	return result.Status, fmt.Errorf("解限流失败: %s", result.Status)
}
