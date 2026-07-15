package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/analytics"
	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
	"github.com/garyzheng0714-lang/fbif-wechat-article/officialbase"
	appSync "github.com/garyzheng0714-lang/fbif-wechat-article/sync"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

var officialRuntime *analytics.Runtime
var officialBaseSyncer *officialbase.Syncer

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("wechat-sync official-api-collector")
		return
	}
	config.Init()
	configureRuntime()
	if analytics.Enabled() {
		var err error
		officialRuntime, err = analytics.NewRuntimeFromEnv()
		if err != nil {
			log.Fatalf("initialize official API collector: %v", err)
		}
		defer officialRuntime.Close()
	} else {
		log.Println("[Warning] Official API collector explicitly disabled")
	}
	if len(os.Args) > 1 && os.Args[1] == "collect-once" {
		if officialRuntime == nil {
			log.Fatal("official API collector disabled")
		}
		result, err := officialRuntime.Run(context.Background())
		_ = json.NewEncoder(os.Stdout).Encode(result)
		if err != nil {
			log.Fatalf("official API collection completed with errors: %v", err)
		}
		return
	}

	mux := http.NewServeMux()
	if !ossConfigured() {
		mediaRoot := os.Getenv("PUBLIC_MEDIA_DIR")
		if mediaRoot == "" {
			mediaRoot = "./media"
		}
		if err := os.MkdirAll(mediaRoot, 0755); err != nil {
			log.Fatalf("create media dir: %v", err)
		}
		mux.Handle("/media/", http.StripPrefix("/media/", http.FileServer(http.Dir(mediaRoot))))
		log.Printf("Local media root enabled: %s", mediaRoot)
	} else {
		log.Printf("OSS media mode enabled: %s", strings.TrimSpace(os.Getenv("OSS_BUCKET_DOMAIN")))
	}
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/feishu/sync", requireAPIKey(syncHandler))
	mux.HandleFunc("/api/feishu/official-sync", requireAPIKey(officialBaseSyncHandler))
	mux.HandleFunc("/api/feishu/cursor", requireAPIKey(cursorHandler))
	mux.HandleFunc("/api/wechat/official/status", requireAPIKey(officialStatusHandler))
	mux.HandleFunc("/api/wechat/official/coverage", requireAPIKey(officialCoverageHandler))
	mux.HandleFunc("/api/wechat/official/endpoints", requireAPIKey(officialEndpointsHandler))
	mux.HandleFunc("/api/wechat/official/collect", requireAPIKey(officialCollectHandler))
	mux.HandleFunc("/api/wechat/official/call", requireAPIKey(officialCallHandler))

	stopCh := make(chan struct{})
	if featureEnabled("ENABLE_FEISHU_SYNC", false) {
		log.Println("[Safety] Legacy direct Feishu sync remains disabled: SQLite-first and historical coverage approval are mandatory")
	}
	if officialRuntime != nil {
		officialRuntime.Start(stopCh)
	}
	if officialbase.Enabled() {
		if officialRuntime == nil {
			log.Println("[OfficialBase] disabled because official API collector is unavailable")
		} else {
			officialBaseSyncer = officialbase.NewFromEnv(officialRuntime.Store)
			officialBaseSyncer.BeforeSync = officialRuntime.RequireHistoricalCoverageForBaseSync
			officialbase.Start(stopCh, officialBaseSyncer)
		}
	}

	addr := fmt.Sprintf(":%d", config.Env.ServerPort)
	log.Printf("Server running on http://localhost%s", addr)
	log.Printf("Server timezone: %s", wechat.ShanghaiLoc())
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func officialBaseSyncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "POST only"})
		return
	}
	if officialBaseSyncer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"success": false, "error": "official Base sync disabled"})
		return
	}
	result, err := officialBaseSyncer.Sync(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "result": result, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
}

func configureRuntime() {
	limitMB := 512
	if raw := strings.TrimSpace(os.Getenv("GO_MEMORY_LIMIT_MB")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limitMB = n
		}
	}
	debug.SetMemoryLimit(int64(limitMB) * 1024 * 1024)
	log.Printf("Go memory limit set to %dMB", limitMB)
}

func ossConfigured() bool {
	return strings.TrimSpace(os.Getenv("OSS_ACCESS_KEY_ID")) != "" &&
		strings.TrimSpace(os.Getenv("OSS_ACCESS_KEY_SECRET")) != "" &&
		strings.TrimSpace(os.Getenv("OSS_BUCKET")) != "" &&
		strings.TrimSpace(os.Getenv("OSS_BUCKET_DOMAIN")) != ""
}

func featureEnabled(key string, defaultValue bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	return value == "1" || strings.EqualFold(value, "true")
}

func requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if config.Env.APIKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		token := ""
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}
		if token == "" {
			token = r.Header.Get("X-API-Key")
		}
		if token != config.Env.APIKey {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"success": false,
				"error":   "invalid or missing API key",
			})
			return
		}
		next.ServeHTTP(w, r)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	cursor, _ := appSync.ReadCursor()
	statusCode := http.StatusServiceUnavailable
	statusLabel := "unhealthy"
	officialStatus := interface{}(map[string]interface{}{
		"ready":  false,
		"reason": "官方 API 采集器已禁用",
	})
	if officialRuntime != nil {
		status, err := officialRuntime.Status(r.Context())
		if err != nil {
			officialStatus = map[string]interface{}{"ready": false, "error": err.Error()}
		} else {
			officialStatus = status
			if status.Ready {
				statusCode = http.StatusOK
				statusLabel = "ok"
			}
		}
	}
	writeJSON(w, statusCode, map[string]interface{}{
		"status":      statusLabel,
		"tokenStatus": wechat.GetTokenStatus(),
		"cursor":      cursor,
		"officialAPI": officialStatus,
	})
}

func syncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "POST only"})
		return
	}

	writeJSON(w, http.StatusGone, map[string]interface{}{
		"success": false,
		"error":   "legacy direct Feishu sync is permanently disabled; collect into SQLite first, then use /api/feishu/official-sync after historicalCoverage.verified=true",
	})
}

func cursorHandler(w http.ResponseWriter, r *http.Request) {
	cursor, _ := appSync.ReadCursor()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"cursor":  cursor,
	})
}

func officialStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "GET only"})
		return
	}
	if officialRuntime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"success": false, "error": "official API collector disabled"})
		return
	}
	status, err := officialRuntime.Status(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	statusCode := http.StatusOK
	if !status.Ready {
		statusCode = http.StatusServiceUnavailable
	}
	writeJSON(w, statusCode, map[string]interface{}{"success": status.Ready, "status": status})
}

func officialCoverageHandler(w http.ResponseWriter, r *http.Request) {
	if officialRuntime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"success": false, "error": "official API collector disabled"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		report, err := officialRuntime.HistoricalCoverage(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "coverage": report})
	case http.MethodPost:
		var input struct {
			ContractVersion string `json:"contractVersion"`
			ApprovedBy      string `json:"approvedBy"`
			Confirm         string `json:"confirm"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid JSON body"})
			return
		}
		current, err := officialRuntime.HistoricalCoverage(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		if input.ContractVersion != current.ContractVersion || input.Confirm != "确认历史覆盖审计并启用Base同步" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success":  false,
				"error":    "contractVersion or explicit confirmation phrase does not match",
				"coverage": current,
			})
			return
		}
		report, err := officialRuntime.ApproveHistoricalCoverage(r.Context(), input.ApprovedBy)
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"success": false, "coverage": report, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "coverage": report})
	case http.MethodDelete:
		report, err := officialRuntime.RevokeHistoricalCoverageApproval(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "coverage": report})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "GET, POST or DELETE only"})
	}
}

func officialEndpointsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "GET only"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"analytics": wechat.AllDataCubeEndpoints(),
		"content":   wechat.AllContentEndpoints(),
	})
}

func officialCollectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "POST only"})
		return
	}
	if officialRuntime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"success": false, "error": "official API collector disabled"})
		return
	}
	result, err := officialRuntime.Run(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "result": result, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
}

func officialCallHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "POST only"})
		return
	}
	if officialRuntime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"success": false, "error": "official API collector disabled"})
		return
	}
	var request struct {
		Endpoint  string          `json:"endpoint"`
		BeginDate string          `json:"begin_date"`
		EndDate   string          `json:"end_date"`
		Payload   json.RawMessage `json:"payload"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if _, ok := wechat.DataCubeEndpointByName(request.Endpoint); ok {
		if request.BeginDate == "" || request.EndDate == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "begin_date and end_date are required"})
			return
		}
		calls, err := officialRuntime.Analytics.CollectRange(r.Context(), request.Endpoint, request.BeginDate, request.EndDate)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "calls": calls, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "calls": calls})
		return
	}
	if _, ok := wechat.ContentEndpointByName(request.Endpoint); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "unknown or non-whitelisted endpoint"})
		return
	}
	var payload interface{}
	if len(request.Payload) > 0 && string(request.Payload) != "null" {
		if err := json.Unmarshal(request.Payload, &payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid payload"})
			return
		}
	}
	response, err := officialRuntime.Content.CallAndArchive(r.Context(), request.Endpoint, payload)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	var body interface{}
	if json.Valid(response.Body) {
		body = json.RawMessage(response.Body)
	} else {
		body = base64.StdEncoding.EncodeToString(response.Body)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"httpStatus": response.HTTPStatus,
		"body":       body,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
