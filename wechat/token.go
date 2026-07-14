package wechat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
)

type tokenState struct {
	accessToken string
	expiresAt   time.Time
}

var (
	cachedToken    *tokenState
	tokenMu        sync.Mutex
	httpClient     = &http.Client{Timeout: 30 * time.Second}
	stableTokenURL = "https://api.weixin.qq.com/cgi-bin/stable_token"
)

func fetchToken() (*tokenState, error) {
	cfg := config.Env
	if cfg.WechatAppID == "" || cfg.WechatSecret == "" {
		return nil, fmt.Errorf("WeChat credentials not configured")
	}

	body, err := json.Marshal(map[string]interface{}{
		"grant_type":    "client_credential",
		"appid":         cfg.WechatAppID,
		"secret":        cfg.WechatSecret,
		"force_refresh": false,
	})
	if err != nil {
		return nil, fmt.Errorf("encode stable token request: %w", err)
	}
	resp, err := httpClient.Post(stableTokenURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("fetch stable token: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read stable token response: %w", err)
	}

	var data struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := json.Unmarshal(responseBody, &data); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("WeChat stable token HTTP %d: %s", resp.StatusCode, data.ErrMsg)
	}
	if data.ErrCode != 0 {
		return nil, fmt.Errorf("WeChat token error %d: %s", data.ErrCode, data.ErrMsg)
	}
	if data.AccessToken == "" || data.ExpiresIn <= 0 {
		return nil, fmt.Errorf("WeChat stable token response missing access_token or expires_in")
	}

	expiresAt := time.Now().Add(time.Duration(data.ExpiresIn-600) * time.Second)
	log.Printf("[Token] Fetched new access_token, expires in %ds", data.ExpiresIn)

	return &tokenState{accessToken: data.AccessToken, expiresAt: expiresAt}, nil
}

func GetToken() (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()

	if cachedToken != nil && time.Now().Before(cachedToken.expiresAt) {
		return cachedToken.accessToken, nil
	}
	t, err := fetchToken()
	if err != nil {
		return "", err
	}
	cachedToken = t
	return t.accessToken, nil
}

func RefreshTokenNow() (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()

	t, err := fetchToken()
	if err != nil {
		return "", err
	}
	cachedToken = t
	return t.accessToken, nil
}

func GetTokenStatus() string {
	tokenMu.Lock()
	defer tokenMu.Unlock()

	if cachedToken == nil {
		return "uninitialized"
	}
	if time.Now().Before(cachedToken.expiresAt) {
		return "valid"
	}
	return "expired"
}
