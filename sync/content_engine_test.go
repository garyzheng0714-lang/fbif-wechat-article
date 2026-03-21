package sync

import (
	"testing"
)

func TestContentStatusConstants(t *testing.T) {
	if contentStatusPending != "pending" {
		t.Errorf("contentStatusPending = %q, want 'pending'", contentStatusPending)
	}
	if contentStatusDone != "done" {
		t.Errorf("contentStatusDone = %q, want 'done'", contentStatusDone)
	}
	if contentStatusFailed != "failed" {
		t.Errorf("contentStatusFailed = %q, want 'failed'", contentStatusFailed)
	}
}

func TestArticleContentFields(t *testing.T) {
	expectedNames := []string{
		"文章内容",
		"content_status",
		"last_fetch_time",
		"fetch_retry_count",
		"fetch_error",
	}
	if len(articleContentFields) != len(expectedNames) {
		t.Fatalf("len(articleContentFields) = %d, want %d", len(articleContentFields), len(expectedNames))
	}
	for i, name := range expectedNames {
		if articleContentFields[i].Name != name {
			t.Errorf("articleContentFields[%d].Name = %q, want %q", i, articleContentFields[i].Name, name)
		}
	}
}

func TestContentSyncResult(t *testing.T) {
	r := &ContentSyncResult{Total: 10, Updated: 7, Failed: 3}
	if r.Total != 10 {
		t.Errorf("Total = %d, want 10", r.Total)
	}
	if r.Updated != 7 {
		t.Errorf("Updated = %d, want 7", r.Updated)
	}
	if r.Failed != 3 {
		t.Errorf("Failed = %d, want 3", r.Failed)
	}
}
