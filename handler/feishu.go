package handler

import (
	"encoding/json"
	"log"
	"net/http"

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
	cfg, _ := appSync.ReadConfig()
	tableSuffix := ""
	if cfg != nil {
		tableSuffix = cfg.TableSuffix
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "ok",
		"tokenStatus": wechat.GetTokenStatus(),
		"cursor":      cursor,
		"tableSuffix": tableSuffix,
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
		{"文章主表", appSync.ArticleMasterTableID},
		{"每日文章数据", func() (string, error) { return feishu.GetOrCreateTable(appSync.TableName("每日文章数据")) }},
		{"粉丝增长", func() (string, error) { return feishu.GetOrCreateTable(appSync.TableName("粉丝增长")) }},
		{"每日阅读概况", func() (string, error) { return feishu.GetOrCreateTable(appSync.TableName("每日阅读概况")) }},
		{"分享场景", func() (string, error) { return feishu.GetOrCreateTable(appSync.TableName("分享场景")) }},
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

// SyncContentHandler fetches plain-text content for articles that have a URL
// but no 文章内容 yet. Runs asynchronously and returns immediately.
//
// POST /api/feishu/sync-content
func SyncContentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	log.Println("[Feishu Route] Starting article content sync in background...")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Content sync started. Check server logs for progress.",
	})

	go func() {
		result, err := appSync.SyncArticleContent()
		if err != nil {
			log.Printf("[Feishu Route] Content sync failed: %v", err)
			return
		}
		log.Printf("[Feishu Route] Content sync done: total=%d updated=%d failed=%d",
			result.Total, result.Updated, result.Failed)
	}()
}

// MigrateHandler switches to a new set of tables (via table suffix), resets the
// cursor, and kicks off a full re-sync. Old tables are left completely untouched.
//
// POST /api/feishu/migrate
// Body: {"table_suffix": "_v2"}
func MigrateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var body struct {
		TableSuffix string `json:"table_suffix"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.TableSuffix == "" {
		writeError(w, http.StatusBadRequest, "table_suffix is required")
		return
	}

	if err := appSync.WriteConfig(&appSync.SyncConfig{TableSuffix: body.TableSuffix}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	appSync.DeleteCursor()

	log.Printf("[Feishu Route] Migration to tableSuffix=%q: cursor reset, starting full re-sync", body.TableSuffix)

	go func() {
		if err := appSync.RunDailySync(); err != nil {
			log.Printf("[Feishu Route] Migration daily sync failed: %v", err)
		}
		if err := appSync.RunBackfillSync(); err != nil {
			log.Printf("[Feishu Route] Migration backfill failed: %v", err)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"tableSuffix": body.TableSuffix,
		"message":     "Config saved, cursor reset, full re-sync started in background. Old tables are untouched.",
	})
}
