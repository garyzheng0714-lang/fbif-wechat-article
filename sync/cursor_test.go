package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCursorReadWriteDelete(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// Read returns nil when no cursor exists
	cursor, err := ReadCursor()
	if err != nil {
		t.Fatalf("ReadCursor on empty dir: %v", err)
	}
	if cursor != nil {
		t.Fatal("Expected nil cursor on empty dir")
	}

	// Write and read back
	expected := &SyncCursor{
		OldestSyncedDate:          "2026-01-01",
		NewestSyncedDate:          "2026-03-17",
		BackfillComplete:          false,
		PublishedScannedPages:     8,
		PublishedBackfillComplete: true,
	}
	if err := WriteCursor(expected); err != nil {
		t.Fatalf("WriteCursor: %v", err)
	}

	cursor, err = ReadCursor()
	if err != nil {
		t.Fatalf("ReadCursor after write: %v", err)
	}
	if cursor == nil {
		t.Fatal("Expected non-nil cursor")
	}
	if cursor.OldestSyncedDate != expected.OldestSyncedDate {
		t.Errorf("OldestSyncedDate = %q, want %q", cursor.OldestSyncedDate, expected.OldestSyncedDate)
	}
	if cursor.NewestSyncedDate != expected.NewestSyncedDate {
		t.Errorf("NewestSyncedDate = %q, want %q", cursor.NewestSyncedDate, expected.NewestSyncedDate)
	}
	if cursor.BackfillComplete != expected.BackfillComplete {
		t.Errorf("BackfillComplete = %v, want %v", cursor.BackfillComplete, expected.BackfillComplete)
	}
	if cursor.PublishedScannedPages != expected.PublishedScannedPages {
		t.Errorf("PublishedScannedPages = %d, want %d", cursor.PublishedScannedPages, expected.PublishedScannedPages)
	}
	if cursor.PublishedBackfillComplete != expected.PublishedBackfillComplete {
		t.Errorf("PublishedBackfillComplete = %v, want %v", cursor.PublishedBackfillComplete, expected.PublishedBackfillComplete)
	}

	// Update cursor
	expected.BackfillComplete = true
	if err := WriteCursor(expected); err != nil {
		t.Fatalf("WriteCursor update: %v", err)
	}

	cursor, err = ReadCursor()
	if err != nil {
		t.Fatalf("ReadCursor after update: %v", err)
	}
	if !cursor.BackfillComplete {
		t.Error("BackfillComplete should be true after update")
	}

	// Delete cursor
	DeleteCursor()
	cursor, err = ReadCursor()
	if err != nil {
		t.Fatalf("ReadCursor after delete: %v", err)
	}
	if cursor != nil {
		t.Fatal("Expected nil cursor after delete")
	}
}

func TestCursorMalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	os.WriteFile(filepath.Join(tmpDir, ".sync-cursor.json"), []byte("{bad json}"), 0644)

	_, err := ReadCursor()
	if err == nil {
		t.Fatal("Expected error for malformed JSON")
	}
}
