package archive

import (
	"context"
	"testing"
	"time"
)

func TestHistoricalCoverageUsesMultiSourceIdentityUnionAndApprovalGate(t *testing.T) {
	ctx := context.Background()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))

	content := []byte(`{"total_count":1,"item_count":1,"item":[{"article_id":"publish-object-1","update_time":100,"content":{"news_item":[{"title":"有指标正文","content":"<p>正文一</p>","url":"https://mp.weixin.qq.com/s/a?mid=100&idx=1"},{"title":"仅正文","content":"<p>正文二</p>","url":"https://mp.weixin.qq.com/s/b?mid=100&idx=2"}]}}]}`)
	if _, err := store.SaveContentPage(ctx, "freepublish", content, now); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContentDetail(ctx, "freepublish", "publish-object-1", []byte(`{"news_item":[{"title":"有指标正文","content":"<p>正文一</p>","url":"https://mp.weixin.qq.com/s/a?mid=100&idx=1"},{"title":"仅正文","content":"<p>正文二</p>","url":"https://mp.weixin.qq.com/s/b?mid=100&idx=2"}]}`), now); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkContentPageSuccess(ctx, "freepublish", 1, 1, true, now); err != nil {
		t.Fatal(err)
	}

	fetches := []FetchRecord{
		{Endpoint: "getarticleread", Category: "article", BeginDate: "2026-07-14", EndDate: "2026-07-14", ResponseJSON: []byte(`{"list":[{"ref_date":"2026-07-14","msgid":"100_1","detail":{"read_user":8}},{"ref_date":"2026-07-14","msgid":"200_1","detail":{"read_user":1}}]}`), Success: true, FetchedAt: now},
		{Endpoint: "getarticleshare", Category: "article", BeginDate: "2026-07-14", EndDate: "2026-07-14", ResponseJSON: []byte(`{"list":[{"ref_date":"2026-07-14","msgid":"100_1","detail":{"share_user":2}}]}`), Success: true, FetchedAt: now},
		{Endpoint: "getarticletotaldetail", Category: "article", BeginDate: "2026-07-14", EndDate: "2026-07-14", ResponseJSON: []byte(`{"list":[{"ref_date":"2026-07-14","msgid":"100_1","title":"有指标正文","content_url":"https://mp.weixin.qq.com/s/a?mid=100&idx=1","detail_list":[{"stat_date":"2026-07-14","read_user":8}]}]}`), Success: true, FetchedAt: now},
	}
	for _, fetch := range fetches {
		if _, err := store.SaveFetch(ctx, fetch); err != nil {
			t.Fatal(err)
		}
		if err := store.MarkSuccess(ctx, fetch.Endpoint, fetch.Category, fetch.BeginDate, fetch.EndDate, "2025-10-31", now); err != nil {
			t.Fatal(err)
		}
		if err := store.MarkBackfillComplete(ctx, fetch.Endpoint, fetch.Category, now); err != nil {
			t.Fatal(err)
		}
	}
	requirements := []HistoricalCoverageRequirement{
		{Endpoint: "getarticleread", EarliestDate: "2025-11-01"},
		{Endpoint: "getarticleshare", EarliestDate: "2025-11-01"},
		{Endpoint: "getarticletotaldetail", EarliestDate: "2025-11-01"},
	}

	report, err := store.AuditHistoricalCoverage(ctx, requirements, now)
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.KnownArticleIdentities != 3 || report.Counts.FreePublishObjects != 1 || report.Counts.FreePublishArticleItems != 2 {
		t.Fatalf("multi-source counts = %+v", report.Counts)
	}
	if report.Counts.ContentAndMetricIdentities != 1 || report.Counts.ContentOnlyIdentities != 1 || report.Counts.MetricOnlyIdentities != 1 {
		t.Fatalf("association counts = %+v", report.Counts)
	}
	if !report.EligibleForUserApproval || report.Verified || report.BaseSyncAllowed || report.Status != "awaiting_user_approval" {
		t.Fatalf("unapproved coverage must not open Base gate: %+v", report)
	}

	if err := store.SaveHistoricalCoverageApproval(ctx, "test-user", now); err != nil {
		t.Fatal(err)
	}
	report, err = store.AuditHistoricalCoverage(ctx, requirements, now)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Verified || !report.BaseSyncAllowed || report.Status != "verified" {
		t.Fatalf("approved complete coverage must be verified: %+v", report)
	}

	if err := store.MarkFailure(ctx, "getarticleread", "article", "temporary failure", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	report, err = store.AuditHistoricalCoverage(ctx, requirements, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if report.Verified || report.BaseSyncAllowed || report.Status != "regressed" {
		t.Fatalf("coverage regression must close Base gate: %+v", report)
	}
}
