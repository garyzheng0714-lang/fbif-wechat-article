package schedule

import (
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

func at(day, hour, minute, second int) time.Time {
	return time.Date(2026, 7, day, hour, minute, second, 0, wechat.ShanghaiLoc())
}

func TestDefaultCollectionRunsDailyAt0805(t *testing.T) {
	daily := Default.Collection
	cases := []struct{ now, want time.Time }{
		{at(15, 8, 4, 59), at(15, 8, 5, 0)},
		{at(15, 8, 5, 0), at(16, 8, 5, 0)},
	}
	for _, tc := range cases {
		if got := daily.Next(tc.now); !got.Equal(tc.want) {
			t.Fatalf("now=%s next=%s want=%s", tc.now, got, tc.want)
		}
	}
	if daily.Begun(at(15, 8, 4, 59)) || !daily.Begun(at(15, 8, 5, 0)) {
		t.Fatal("D-1 collection must start exactly at 08:05")
	}
}

func TestEveryConfiguredTimeRuns(t *testing.T) {
	daily, ok := parseDaily("17:30,08:05")
	if !ok || daily.String() != "08:05,17:30" {
		t.Fatalf("parsed=%v ok=%v", daily, ok)
	}
	if got := daily.Next(at(15, 9, 0, 0)); !got.Equal(at(15, 17, 30, 0)) {
		t.Fatalf("second time of day skipped: %s", got)
	}
	if got := daily.Next(at(15, 17, 30, 0)); !got.Equal(at(16, 8, 5, 0)) {
		t.Fatalf("after last time got %s", got)
	}
}

func TestDefaultLayoutPollKeepsPreHubTicks(t *testing.T) {
	poll := Default.LayoutPoll
	cases := []struct{ now, want time.Time }{
		{at(15, 8, 0, 0), at(15, 8, 30, 0)},
		{at(15, 12, 1, 0), at(15, 12, 15, 0)},
		{at(15, 18, 15, 0), at(15, 18, 30, 0)},
		{at(15, 18, 30, 0), at(16, 8, 30, 0)},
	}
	for _, tc := range cases {
		if got := poll.Next(tc.now); !got.Equal(tc.want) {
			t.Fatalf("now=%s next=%s want=%s", tc.now, got, tc.want)
		}
	}
	for _, inside := range []time.Time{at(15, 8, 30, 0), at(15, 18, 30, 0)} {
		if !poll.Covers(inside) {
			t.Fatalf("%s should be inside the polling span", inside)
		}
	}
	for _, outside := range []time.Time{at(15, 8, 29, 59), at(15, 18, 30, 1), at(15, 18, 35, 0)} {
		if poll.Covers(outside) {
			t.Fatalf("%s should be outside the polling span", outside)
		}
	}
}

func TestWindowEndIsExclusiveLikeTheHub(t *testing.T) {
	window, ok := parseWindow("08:30-18:30")
	if !ok {
		t.Fatal("window not parsed")
	}
	poll := Every{Window: window, Interval: 15 * time.Minute}
	if got := poll.Next(at(15, 18, 0, 0)); !got.Equal(at(15, 18, 15, 0)) {
		t.Fatalf("got %s", got)
	}
	if got := poll.Next(at(15, 18, 15, 0)); !got.Equal(at(16, 8, 30, 0)) {
		t.Fatalf("18:30 is the window end and must not run, got %s", got)
	}
	if poll.Covers(at(15, 18, 20, 0)) {
		t.Fatal("span ends at the last tick 18:15")
	}
}

func TestWindowAcrossMidnight(t *testing.T) {
	window, ok := parseWindow("23:00-07:00")
	if !ok {
		t.Fatal("window not parsed")
	}
	poll := Every{Window: window, Interval: 2 * time.Hour}
	cases := []struct{ now, want time.Time }{
		{at(15, 22, 0, 0), at(15, 23, 0, 0)},
		{at(15, 23, 0, 0), at(16, 1, 0, 0)},
		{at(16, 4, 30, 0), at(16, 5, 0, 0)},
		{at(16, 5, 0, 0), at(16, 23, 0, 0)},
	}
	for _, tc := range cases {
		if got := poll.Next(tc.now); !got.Equal(tc.want) {
			t.Fatalf("now=%s next=%s want=%s", tc.now, got, tc.want)
		}
	}
	if !poll.Covers(at(16, 3, 0, 0)) || poll.Covers(at(16, 6, 0, 0)) || poll.Covers(at(16, 12, 0, 0)) {
		t.Fatal("cross-midnight span is 23:00 through 05:00")
	}
}

func TestWholeDayWindowRestartsAtMidnight(t *testing.T) {
	window, ok := parseWindow("00:00-24:00")
	if !ok {
		t.Fatal("whole day window not parsed")
	}
	poll := Every{Window: window, Interval: 7 * time.Hour}
	if got := poll.Next(at(15, 21, 0, 0)); !got.Equal(at(16, 0, 0, 0)) {
		t.Fatalf("got %s", got)
	}
}

func TestUnreadableValuesFallBackToDefaults(t *testing.T) {
	t.Setenv(collectionTimesVar, "8点")
	t.Setenv(layoutPollWindowVar, "18:30-18:30")
	t.Setenv(layoutPollSecondsVar, "59")
	t.Setenv(baseSyncFallbackVar, "6h")
	if got := FromEnv(); got.String() != Default.String() {
		t.Fatalf("got %s want %s", got, Default)
	}
	for _, bad := range []string{"24:00-08:00", "8:30-25:00", "08:30", "+8:30-18:00"} {
		if _, ok := parseWindow(bad); ok {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

func TestHubValuesAreRead(t *testing.T) {
	t.Setenv(collectionTimesVar, "09:00,17:30")
	t.Setenv(layoutPollWindowVar, "08:30-18:30")
	t.Setenv(layoutPollSecondsVar, "1800")
	t.Setenv(baseSyncFallbackVar, "43200")
	want := "ANALYTICS_TIMES=09:00,17:30 AUTO_LAYOUT_POLL_WINDOW=08:30-18:30 AUTO_LAYOUT_POLL_SECONDS=1800 OFFICIAL_BASE_SYNC_FALLBACK_SECONDS=43200"
	if got := FromEnv().String(); got != want {
		t.Fatalf("got %s", got)
	}
}
