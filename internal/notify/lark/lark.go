package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

type LarkNotify struct {
	WebhookURL string
	Client     *http.Client
	Template   map[string]string // 卡片json模板
}

// 创建一个飞书实例
func NewLarkNotify(webhookURL string, templatePaths map[string]string) (*LarkNotify, error) {
	templates := make(map[string]string)
	for eventType, path := range templatePaths {
		data, err := ioutil.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read template for '%s' failed: %w", eventType, err)
		}
		templates[eventType] = string(data)
	}

	return &LarkNotify{
		WebhookURL: webhookURL,
		Client: &http.Client{
			Timeout: time.Second * 5,
		},
		Template: templates,
	}, nil
}

func (l *LarkNotify) Notify(ctx context.Context, eventType string, vars map[string]string) error {
	//logger := logf.FromContext(ctx)

	template, ok := l.Template[eventType]
	if !ok {
		return fmt.Errorf("no template found for event type: %s", eventType)
	}

	bodyStr := template
	for k, v := range vars {
		bodyStr = strings.ReplaceAll(bodyStr, "${"+k+"}", v)
	}

	var cardContent map[string]interface{}
	if err := json.Unmarshal([]byte(bodyStr), &cardContent); err != nil {
		return err
	}

	payload := map[string]interface{}{
		"msg_type": "interactive",
		"card":     cardContent,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", l.WebhookURL, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("notify failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
