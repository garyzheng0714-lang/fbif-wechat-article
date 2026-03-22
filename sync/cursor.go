package sync

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

type SyncCursor struct {
	OldestSyncedDate          string `json:"oldestSyncedDate"`
	NewestSyncedDate          string `json:"newestSyncedDate"`
	BackfillComplete          bool   `json:"backfillComplete"`
	PublishedScannedPages     int    `json:"publishedScannedPages,omitempty"`
	PublishedBackfillComplete bool   `json:"publishedBackfillComplete,omitempty"`
	MaterialNewsOffset        int    `json:"materialNewsOffset,omitempty"`
	MaterialBackfillComplete  bool   `json:"materialBackfillComplete,omitempty"`
}

func getCursorPaths() []string {
	cwd, _ := os.Getwd()
	return []string{
		filepath.Join(cwd, ".sync-cursor.json"),
		filepath.Join(cwd, "..", ".sync-cursor.json"),
	}
}

func ReadCursor() (*SyncCursor, error) {
	for _, p := range getCursorPaths() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cursor SyncCursor
		if err := json.Unmarshal(data, &cursor); err != nil {
			return nil, fmt.Errorf("parse cursor file %s: %w", p, err)
		}
		return &cursor, nil
	}
	return nil, nil
}

func WriteCursor(cursor *SyncCursor) error {
	paths := getCursorPaths()
	data, err := json.MarshalIndent(cursor, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cursor: %w", err)
	}
	if err := os.WriteFile(paths[0], data, 0644); err != nil {
		return fmt.Errorf("write cursor: %w", err)
	}
	log.Printf(
		"[Cursor] Saved: oldest=%s newest=%s complete=%v published_pages=%d published_done=%v material_offset=%d material_done=%v",
		cursor.OldestSyncedDate,
		cursor.NewestSyncedDate,
		cursor.BackfillComplete,
		cursor.PublishedScannedPages,
		cursor.PublishedBackfillComplete,
		cursor.MaterialNewsOffset,
		cursor.MaterialBackfillComplete,
	)
	return nil
}

func DeleteCursor() {
	for _, p := range getCursorPaths() {
		if err := os.Remove(p); err == nil {
			log.Printf("[Cursor] Deleted: %s", p)
		}
	}
}
