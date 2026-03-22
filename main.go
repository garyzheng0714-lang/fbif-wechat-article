package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
	appSync "github.com/garyzheng0714-lang/fbif-wechat-article/sync"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

func main() {
	config.Init()
	configureRuntime()

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
	mux.HandleFunc("/api/feishu/cursor", requireAPIKey(cursorHandler))

	stopCh := make(chan struct{})
	appSync.StartScheduler(stopCh)
	appSync.StartMediaWorker(stopCh)

	go func() {
		log.Println("[Startup] Running initial sync...")
		if err := appSync.RunDailySync(); err != nil {
			log.Printf("[Startup] Daily sync failed: %v", err)
		}
	}()

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
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "ok",
		"tokenStatus": wechat.GetTokenStatus(),
		"cursor":      cursor,
	})
}

func syncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "POST only"})
		return
	}

	log.Println("[Route] Triggering daily sync...")
	if err := appSync.RunDailySync(); err != nil {
		log.Printf("[Route] Daily sync failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	cursor, _ := appSync.ReadCursor()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Daily sync completed",
		"cursor":  cursor,
	})
}

func cursorHandler(w http.ResponseWriter, r *http.Request) {
	cursor, _ := appSync.ReadCursor()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"cursor":  cursor,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
