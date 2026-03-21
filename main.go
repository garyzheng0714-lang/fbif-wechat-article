package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
	"github.com/garyzheng0714-lang/fbif-wechat-article/handler"
	appSync "github.com/garyzheng0714-lang/fbif-wechat-article/sync"
)

func main() {
	config.Init()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handler.HealthHandler)
	// All other endpoints require API key authentication
	mux.HandleFunc("/api/feishu/sync", handler.RequireAPIKey(handler.SyncHandler))
	mux.HandleFunc("/api/feishu/backfill", handler.RequireAPIKey(handler.BackfillHandler))
	mux.HandleFunc("/api/feishu/cursor", handler.RequireAPIKey(handler.CursorHandler))
	mux.HandleFunc("/api/feishu/reset", handler.RequireAPIKey(handler.ResetHandler))
	mux.HandleFunc("/api/feishu/migrate", handler.RequireAPIKey(handler.MigrateHandler))
	mux.HandleFunc("/api/feishu/sync-content", handler.RequireAPIKey(handler.SyncContentHandler))

	stopCh := make(chan struct{})
	appSync.StartScheduler(stopCh)

	go func() {
		log.Println("[Startup] Running initial sync...")
		if err := appSync.RunDailySync(); err != nil {
			log.Printf("[Startup] Daily sync failed: %v", err)
		}
		if err := appSync.RunBackfillSync(); err != nil {
			log.Printf("[Startup] Backfill failed: %v", err)
		}
	}()

	addr := fmt.Sprintf(":%d", config.Env.ServerPort)
	log.Printf("Server running on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
