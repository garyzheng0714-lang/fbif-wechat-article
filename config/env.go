package config

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	NodeEnv               string
	ServerPort            int
	WechatAppID           string
	WechatSecret          string
	FeishuAppID           string
	FeishuAppSecret       string
	FeishuBitableAppToken string
	FeishuBitableTableID  string
	APIKey                string // Bearer token for HTTP endpoints
}

var Env Config

func Init() {
	loadDotEnv()

	Env = Config{
		NodeEnv:               getEnvDefault("NODE_ENV", "development"),
		ServerPort:            getEnvInt("SERVER_PORT", 3002),
		WechatAppID:           os.Getenv("WECHAT_APPID"),
		WechatSecret:          os.Getenv("WECHAT_SECRET"),
		FeishuAppID:           os.Getenv("FEISHU_APP_ID"),
		FeishuAppSecret:       os.Getenv("FEISHU_APP_SECRET"),
		FeishuBitableAppToken: os.Getenv("FEISHU_BITABLE_APP_TOKEN"),
		FeishuBitableTableID:  os.Getenv("FEISHU_BITABLE_TABLE_ID"),
		APIKey:                os.Getenv("API_KEY"),
	}

	if Env.WechatAppID == "" || Env.WechatSecret == "" {
		log.Println("[Warning] WECHAT_APPID or WECHAT_SECRET not set. API calls will fail.")
	}
}

func loadDotEnv() {
	candidates := []string{
		filepath.Join(".", ".env"),
		filepath.Join("..", ".env"),
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			v = strings.Trim(v, `"'`)
			if os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
		return
	}
}

func getEnvDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func getEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
