package utils

import (
	"crypto/md5"
	"encoding/hex"
)

func GenCRName(ip string) string {
	hash := md5.Sum([]byte(ip))
	return "ipblock-" + hex.EncodeToString(hash[:8]) // 16位
}
