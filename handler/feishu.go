package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
	appSync "github.com/garyzheng0714-lang/fbif-wechat-article/sync"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]interface{}{
		"success": false,
		"error":   msg,
	})
}

// HealthHandler returns service health status.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	cursor, _ := appSync.ReadCursor()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "ok",
		"tokenStatus": wechat.GetTokenStatus(),
		"cursor":      cursor,
	})
}

// SyncHandler triggers a daily sync.
func SyncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	log.Println("[Feishu Route] Triggering daily sync...")
	if err := appSync.RunDailySync(); err != nil {
		log.Printf("[Feishu Route] Daily sync failed: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	cursor, _ := appSync.ReadCursor()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Daily sync completed",
		"cursor":  cursor,
	})
}

// BackfillHandler triggers backfill sync.
func BackfillHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	log.Println("[Feishu Route] Triggering backfill sync...")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Backfill started. Check server logs for progress.",
	})

	go func() {
		if err := appSync.RunBackfillSync(); err != nil {
			log.Printf("[Feishu Route] Backfill failed: %v", err)
		}
	}()
}

// CursorHandler returns current cursor state.
func CursorHandler(w http.ResponseWriter, r *http.Request) {
	cursor, _ := appSync.ReadCursor()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"cursor":  cursor,
	})
}

// ResetHandler clears all data and cursor.
func ResetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	log.Println("[Feishu Route] Resetting all data...")

	type tableSpec struct {
		name      string
		getID     func() (string, error)
	}

	tables := []tableSpec{
		{"文章主表", func() (string, error) { return config.Env.FeishuBitableTableID, nil }},
		{"每日文章数据", func() (string, error) { return feishu.GetOrCreateTable("每日文章数据") }},
		{"粉丝增长", func() (string, error) { return feishu.GetOrCreateTable("粉丝增长") }},
		{"每日阅读概况", func() (string, error) { return feishu.GetOrCreateTable("每日阅读概况") }},
		{"分享场景", func() (string, error) { return feishu.GetOrCreateTable("分享场景") }},
	}

	deleted := make(map[string]int)
	for _, t := range tables {
		tableID, err := t.getID()
		if err != nil {
			log.Printf("[Feishu Route] Failed to get table ID for %s: %v", t.name, err)
			continue
		}
		n, err := feishu.ClearTableRecords(tableID)
		if err != nil {
			log.Printf("[Feishu Route] Failed to clear %s: %v", t.name, err)
		}
		deleted[t.name] = n
	}

	appSync.DeleteCursor()

	log.Printf("[Feishu Route] Reset complete: %v", deleted)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "All data cleared and cursor reset",
		"deleted": deleted,
	})
}
