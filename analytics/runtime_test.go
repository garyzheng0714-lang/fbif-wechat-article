package analytics

import (
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

func TestQuotaAwareCallBudgetReservesEveryRemainingLayoutPoll(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, wechat.ShanghaiLoc())
	budget, reserve := quotaAwareCallBudget(2000, 815, now, 15*time.Minute, true, 0)
	if reserve != 52 || budget != 763 {
		t.Fatalf("budget=%d reserve=%d, want 763/52", budget, reserve)
	}
}

func TestQuotaAwareCallBudgetHonorsConfiguredCapAndContentMinimum(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, wechat.ShanghaiLoc())
	budget, reserve := quotaAwareCallBudget(500, 815, now, 15*time.Minute, true, 0)
	if budget != 500 || reserve != 52 {
		t.Fatalf("configured cap not honored: budget=%d reserve=%d", budget, reserve)
	}
	budget, reserve = quotaAwareCallBudget(400, 1, now, 15*time.Minute, true, 2)
	if budget != 1 || reserve != 52 {
		t.Fatalf("content must use the one still-available call without crossing the hard cap: budget=%d reserve=%d", budget, reserve)
	}
}

func TestQuotaAwareCallBudgetWithoutLayoutUsesAllUsableQuota(t *testing.T) {
	budget, reserve := quotaAwareCallBudget(2000, 815, time.Now(), 15*time.Minute, false, 0)
	if budget != 815 || reserve != 0 {
		t.Fatalf("budget=%d reserve=%d, want 815/0", budget, reserve)
	}
}

func TestNextScheduledCollectionStartsShortlyAfterQuotaRefresh(t *testing.T) {
	before := time.Date(2026, 7, 15, 0, 4, 59, 0, wechat.ShanghaiLoc())
	wantToday := time.Date(2026, 7, 15, 0, 5, 0, 0, wechat.ShanghaiLoc())
	if got := nextScheduledCollection(before); !got.Equal(wantToday) {
		t.Fatalf("before 00:05 got %s, want %s", got, wantToday)
	}

	after := time.Date(2026, 7, 15, 0, 5, 0, 0, wechat.ShanghaiLoc())
	wantTomorrow := time.Date(2026, 7, 16, 0, 5, 0, 0, wechat.ShanghaiLoc())
	if got := nextScheduledCollection(after); !got.Equal(wantTomorrow) {
		t.Fatalf("at 00:05 got %s, want %s", got, wantTomorrow)
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

func TestRemainingLayoutPollsOnlyReserveCurrentWorkday(t *testing.T) {
	loc := wechat.ShanghaiLoc()
	interval := 15 * time.Minute
	cases := []struct {
		now  time.Time
		want int
	}{
		{time.Date(2026, 7, 15, 0, 5, 0, 0, loc), 41},
		{time.Date(2026, 7, 15, 12, 0, 0, 0, loc), 26},
		{time.Date(2026, 7, 15, 18, 30, 0, 0, loc), 0},
		{time.Date(2026, 7, 15, 20, 0, 0, 0, loc), 0},
	}
	for _, tc := range cases {
		if got := remainingLayoutPollsToday(tc.now, interval); got != tc.want {
			t.Fatalf("now=%s polls=%d want=%d", tc.now, got, tc.want)
		}
	}
}
