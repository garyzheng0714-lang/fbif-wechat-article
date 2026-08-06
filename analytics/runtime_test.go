package analytics

import (
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

func TestNextScheduledCollectionRunsAfterDelayedMetricsBecomeAvailable(t *testing.T) {
	before := time.Date(2026, 7, 15, 8, 4, 59, 0, wechat.ShanghaiLoc())
	wantToday := time.Date(2026, 7, 15, 8, 5, 0, 0, wechat.ShanghaiLoc())
	if got := nextScheduledCollection(before); !got.Equal(wantToday) {
		t.Fatalf("before 08:05 got %s, want %s", got, wantToday)
	}

	after := time.Date(2026, 7, 15, 8, 5, 0, 0, wechat.ShanghaiLoc())
	wantTomorrow := time.Date(2026, 7, 16, 8, 5, 0, 0, wechat.ShanghaiLoc())
	if got := nextScheduledCollection(after); !got.Equal(wantTomorrow) {
		t.Fatalf("at 08:05 got %s, want %s", got, wantTomorrow)
	}
}

func TestSumQuotaCountsUsesIndependentEndpointReservations(t *testing.T) {
	counts := map[string]int{
		"freepublish_batchget":     8,
		"datacube_getarticleread":  126,
		"datacube_getarticleshare": 125,
	}
	if got := sumQuotaCounts(counts); got != 259 {
		t.Fatalf("sum=%d, want 259", got)
	}
}

func TestArchivedQuotaKeyMatchesClientReservationKeys(t *testing.T) {
	if got := archivedQuotaKey("getarticleread"); got != "datacube_getarticleread" {
		t.Fatalf("data cube key=%q", got)
	}
	if got := archivedQuotaKey("freepublish_batchget"); got != "freepublish_batchget" {
		t.Fatalf("content key=%q", got)
	}
}

func TestDailyCollectionNeverStartsBefore0805Shanghai(t *testing.T) {
	loc := wechat.ShanghaiLoc()
	if dailyCollectionReady(time.Date(2026, 7, 15, 8, 4, 59, 0, loc)) {
		t.Fatal("D-1 collection must not start before 08:05")
	}
	if !dailyCollectionReady(time.Date(2026, 7, 15, 8, 5, 0, 0, loc)) {
		t.Fatal("D-1 collection should be allowed at 08:05")
	}
}

func TestLayoutMonitoringWindowIsInclusive(t *testing.T) {
	loc := wechat.ShanghaiLoc()
	for _, value := range []time.Time{
		time.Date(2026, 7, 15, 8, 30, 0, 0, loc),
		time.Date(2026, 7, 15, 18, 30, 0, 0, loc),
	} {
		if !withinLayoutMonitoringWindow(value) {
			t.Fatalf("%s should be inside monitoring window", value)
		}
	}
	for _, value := range []time.Time{
		time.Date(2026, 7, 15, 8, 29, 59, 0, loc),
		time.Date(2026, 7, 15, 18, 30, 1, 0, loc),
	} {
		if withinLayoutMonitoringWindow(value) {
			t.Fatalf("%s should be outside monitoring window", value)
		}
	}
}

func TestNextLayoutPollStaysInsideMonitoringWindow(t *testing.T) {
	loc := wechat.ShanghaiLoc()
	interval := 15 * time.Minute
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{time.Date(2026, 7, 15, 8, 0, 0, 0, loc), time.Date(2026, 7, 15, 8, 30, 0, 0, loc)},
		{time.Date(2026, 7, 15, 12, 1, 0, 0, loc), time.Date(2026, 7, 15, 12, 15, 0, 0, loc)},
		{time.Date(2026, 7, 15, 18, 15, 0, 0, loc), time.Date(2026, 7, 15, 18, 30, 0, 0, loc)},
		{time.Date(2026, 7, 15, 18, 30, 0, 0, loc), time.Date(2026, 7, 16, 8, 30, 0, 0, loc)},
	}
	for _, tc := range cases {
		if got := nextLayoutPoll(tc.now, interval); !got.Equal(tc.want) {
			t.Fatalf("now=%s got=%s want=%s", tc.now, got, tc.want)
		}
	}
}
