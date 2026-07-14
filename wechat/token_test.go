package wechat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
)

func TestFetchTokenUsesOfficialStableTokenWithoutPuttingSecretInURL(t *testing.T) {
	oldURL := stableTokenURL
	oldClient := httpClient
	oldConfig := config.Env
	defer func() {
		stableTokenURL = oldURL
		httpClient = oldClient
		config.Env = oldConfig
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.RawQuery != "" {
			t.Fatalf("credentials leaked into URL query: %s", r.URL.RawQuery)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["appid"] != "appid" || body["secret"] != "secret" || body["grant_type"] != "client_credential" {
			t.Fatalf("request body = %+v", body)
		}
		_, _ = w.Write([]byte(`{"access_token":"stable","expires_in":7200}`))
	}))
	defer server.Close()

	stableTokenURL = server.URL
	httpClient = server.Client()
	config.Env.WechatAppID = "appid"
	config.Env.WechatSecret = "secret"
	token, err := fetchToken()
	if err != nil {
		t.Fatal(err)
	}
	if token.accessToken != "stable" {
		t.Fatalf("token = %+v", token)
	}
}
