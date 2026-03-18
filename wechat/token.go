package wechat

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
)

type tokenState struct {
	accessToken string
	expiresAt   time.Time
}

var (
	cachedToken *tokenState
	tokenMu     sync.Mutex
	httpClient  = &http.Client{Timeout: 30 * time.Second}
)

func fetchToken() (*tokenState, error) {
	cfg := config.Env
	if cfg.WechatAppID == "" || cfg.WechatSecret == "" {
		return nil, fmt.Errorf("WeChat credentials not configured")
	}

	u, _ := url.Parse("https://api.weixin.qq.com/cgi-bin/token")
	q := u.Query()
	q.Set("grant_type", "client_credential")
	q.Set("appid", cfg.WechatAppID)
	q.Set("secret", cfg.WechatSecret)
	u.RawQuery = q.Encode()

	resp, err := httpClient.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("fetch token: %w", err)
	}
	defer resp.Body.Close()

	var data struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if data.ErrCode != 0 {
		return nil, fmt.Errorf("WeChat token error %d: %s", data.ErrCode, data.ErrMsg)
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
