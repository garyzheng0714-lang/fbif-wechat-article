package wechat

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func resetQuotaForTest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "quota.json")
	t.Setenv("WECHAT_QUOTA_FILE", path)
	quotaMu.Lock()
	quotaCache = nil
	quotaMu.Unlock()
	t.Cleanup(func() {
		quotaMu.Lock()
		quotaCache = nil
		quotaMu.Unlock()
	})
	return path
}

func TestDailyQuotaKeepsTwoHundredCallsReserveByDefault(t *testing.T) {
	t.Setenv("WECHAT_DAILY_QUOTA_LIMIT", "")
	t.Setenv("WECHAT_ENDPOINT_DAILY_QUOTA_LIMIT", "")
	t.Setenv("WECHAT_DAILY_QUOTA_RESERVE", "")
	t.Setenv("WECHAT_DAILY_QUOTA_RESERVE_PERCENT", "")
	t.Setenv("WECHAT_ENDPOINT_DAILY_QUOTA_RESERVE", "")
	t.Setenv("WECHAT_ENDPOINT_DAILY_QUOTA_RESERVE_PERCENT", "")
	if got := dailyQuotaLimit(); got != 1000 {
		t.Fatalf("limit=%d, want official cap 1000", got)
	}
	if got := dailyQuotaReserve(); got != 200 {
		t.Fatalf("reserve=%d, want 200", got)
	}
}

func TestDailyQuotaPercentageOverridesLegacyAbsoluteReserve(t *testing.T) {
	t.Setenv("WECHAT_DAILY_QUOTA_LIMIT", "777")
	t.Setenv("WECHAT_DAILY_QUOTA_RESERVE", "100")
	t.Setenv("WECHAT_DAILY_QUOTA_RESERVE_PERCENT", "2")
	if got := dailyQuotaReserve(); got != 16 {
		t.Fatalf("reserve=%d, want ceil(777*2%%)=16", got)
	}
}

func TestDailyQuotaCapsLegacyLimitAtOfficialMaximum(t *testing.T) {
	t.Setenv("WECHAT_DAILY_QUOTA_LIMIT", "1500")
	if got := dailyQuotaLimit(); got != 1000 {
		t.Fatalf("limit=%d, want 1000", got)
	}
}

func TestDailyQuotaIsIndependentPerEndpoint(t *testing.T) {
	resetQuotaForTest(t)
	t.Setenv("WECHAT_ENDPOINT_DAILY_QUOTA_LIMIT", "3")
	t.Setenv("WECHAT_ENDPOINT_DAILY_QUOTA_RESERVE", "1")

	for i := 0; i < 2; i++ {
		if err := checkAndIncrementQuota("endpoint_a"); err != nil {
			t.Fatalf("endpoint_a call %d: %v", i+1, err)
		}
	}
	if err := checkAndIncrementQuota("endpoint_a"); err == nil {
		t.Fatal("endpoint_a third call should be blocked")
	} else {
		var quotaErr *QuotaLimitError
		if !errors.As(err, &quotaErr) {
			t.Fatalf("endpoint_a error=%T, want QuotaLimitError", err)
		}
	}
	if err := checkAndIncrementQuota("endpoint_b"); err != nil {
		t.Fatalf("endpoint_b must retain its independent budget: %v", err)
	}

	if got := CurrentEndpointQuotaStatus("endpoint_a").Used; got != 2 {
		t.Fatalf("endpoint_a used=%d, want 2", got)
	}
	if got := CurrentEndpointQuotaStatus("endpoint_b").Used; got != 1 {
		t.Fatalf("endpoint_b used=%d, want 1", got)
	}
}

func TestLegacyGlobalCountDoesNotResetSameDayQuota(t *testing.T) {
	path := resetQuotaForTest(t)
	t.Setenv("WECHAT_ENDPOINT_DAILY_QUOTA_LIMIT", "3")
	t.Setenv("WECHAT_ENDPOINT_DAILY_QUOTA_RESERVE", "1")
	legacy := map[string]interface{}{
		"date":  time.Now().In(ShanghaiLoc()).Format("2006-01-02"),
		"count": 2,
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := checkAndIncrementQuota("endpoint_a"); err == nil {
		t.Fatal("legacy same-day count must conservatively block endpoint_a")
	}
	if err := checkAndIncrementQuota("endpoint_b"); err == nil {
		t.Fatal("legacy same-day count must conservatively block endpoint_b")
	}
}

func TestDailyQuotaDoesNotCallThroughWhenReservationCannotPersist(t *testing.T) {
	resetQuotaForTest(t)
	brokenPath := filepath.Join(t.TempDir(), "quota-as-directory")
	if err := os.Mkdir(brokenPath, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WECHAT_QUOTA_FILE", brokenPath)
	t.Setenv("WECHAT_ENDPOINT_DAILY_QUOTA_LIMIT", "3")
	t.Setenv("WECHAT_ENDPOINT_DAILY_QUOTA_RESERVE", "1")

	if err := checkAndIncrementQuota("endpoint_a"); err == nil {
		t.Fatal("quota reservation persistence failure must fail closed")
	}
	if got := CurrentEndpointQuotaStatus("endpoint_a").Used; got != 0 {
		t.Fatalf("failed reservation must be rolled back in memory: used=%d", got)
	}
}
