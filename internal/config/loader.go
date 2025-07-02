package config

import (
	"github/Beatrueman/ipblock-operator/internal/policy"
	corev1 "k8s.io/api/core/v1"
	"strings"
)

func LoadWhitelistFromConfigMap(cm *corev1.ConfigMap) *policy.Whitelist {
	whitelistStr := cm.Data["whitelist"]
	lines := strings.Split(whitelistStr, "\n")
	cleaned := []string{}
	for _, l := range lines {
		if trimmed := strings.TrimSpace(l); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return policy.NewWhitelist(cleaned)
}
