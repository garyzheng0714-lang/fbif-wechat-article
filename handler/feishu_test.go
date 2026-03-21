package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
)

func init() {
	// Ensure config package is initialized (config.Env is safe to read if nil)
}

func TestRequireAPIKey_NoKeyConfigured(t *testing.T) {
	// Save original and restore after test
	orig := config.Env.APIKey
	config.Env.APIKey = ""
	defer func() { config.Env.APIKey = orig }()

_called := false
	h := RequireAPIKey(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !_called {
		t.Error("expected handler to be called when APIKey is empty")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestRequireAPIKey_ValidBearerToken(t *testing.T) {
	orig := config.Env.APIKey
	config.Env.APIKey = "secret123"
	defer func() { config.Env.APIKey = orig }()

_called := false
	h := RequireAPIKey(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !_called {
		t.Error("expected handler to be called with correct Bearer token")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestRequireAPIKey_ValidAPIKeyHeader(t *testing.T) {
	orig := config.Env.APIKey
	config.Env.APIKey = "secret456"
	defer func() { config.Env.APIKey = orig }()

_called := false
	h := RequireAPIKey(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", "secret456")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !_called {
		t.Error("expected handler to be called with correct X-API-Key")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestRequireAPIKey_MissingToken(t *testing.T) {
	orig := config.Env.APIKey
	config.Env.APIKey = "secret123"
	defer func() { config.Env.APIKey = orig }()

_called := false
	h := RequireAPIKey(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if _called {
		t.Error("expected handler NOT to be called with missing token")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestRequireAPIKey_WrongToken(t *testing.T) {
	orig := config.Env.APIKey
	config.Env.APIKey = "secret123"
	defer func() { config.Env.APIKey = orig }()

_called := false
	h := RequireAPIKey(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer wrongtoken")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if _called {
		t.Error("expected handler NOT to be called with wrong token")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}
