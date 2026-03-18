package feishu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
)

type tokenState struct {
	tenantAccessToken string
	expiresAt         time.Time
}

var (
	cachedToken *tokenState
	tokenMu     sync.Mutex
	httpClient  = &http.Client{Timeout: 30 * time.Second}
)

func fetchTenantToken() (*tokenState, error) {
	cfg := config.Env
	if cfg.FeishuAppID == "" || cfg.FeishuAppSecret == "" {
		return nil, fmt.Errorf("Feishu credentials not configured. Set FEISHU_APP_ID and FEISHU_APP_SECRET")
	}

	body, _ := json.Marshal(map[string]string{
		"app_id":     cfg.FeishuAppID,
		"app_secret": cfg.FeishuAppSecret,
	})

	resp, err := httpClient.Post(
		"https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("fetch feishu token: %w", err)
	}
	defer resp.Body.Close()

	var data struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode feishu token response: %w", err)
	}
	if data.Code != 0 {
		return nil, fmt.Errorf("Feishu token error: %s", data.Msg)
	}

	log.Printf("[Feishu] Fetched tenant_access_token, expires in %ds", data.Expire)

	return &tokenState{
		tenantAccessToken: data.TenantAccessToken,
		expiresAt:         time.Now().Add(time.Duration(data.Expire-300) * time.Second),
	}, nil
}

func GetToken() (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()

	if cachedToken != nil && time.Now().Before(cachedToken.expiresAt) {
		return cachedToken.tenantAccessToken, nil
	}
	t, err := fetchTenantToken()
	if err != nil {
		return "", err
	}
	cachedToken = t
	return t.tenantAccessToken, nil
}
