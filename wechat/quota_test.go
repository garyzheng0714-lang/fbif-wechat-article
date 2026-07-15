package wechat

import "testing"

func TestDailyQuotaKeepsTwoPercentReserveByDefault(t *testing.T) {
	t.Setenv("WECHAT_DAILY_QUOTA_LIMIT", "1500")
	t.Setenv("WECHAT_DAILY_QUOTA_RESERVE", "")
	t.Setenv("WECHAT_DAILY_QUOTA_RESERVE_PERCENT", "")
	if got := dailyQuotaReserve(); got != 30 {
		t.Fatalf("reserve=%d, want 30", got)
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
