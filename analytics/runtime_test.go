package analytics

import (
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

func TestQuotaAwareCallBudgetReservesEveryRemainingLayoutPoll(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, wechat.ShanghaiLoc())
	budget, reserve := quotaAwareCallBudget(2000, 815, now, 15*time.Minute, true, 0)
	if reserve != 96 || budget != 719 {
		t.Fatalf("budget=%d reserve=%d, want 719/96", budget, reserve)
	}
}

func TestQuotaAwareCallBudgetHonorsConfiguredCapAndContentMinimum(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, wechat.ShanghaiLoc())
	budget, reserve := quotaAwareCallBudget(500, 815, now, 15*time.Minute, true, 0)
	if budget != 500 || reserve != 96 {
		t.Fatalf("configured cap not honored: budget=%d reserve=%d", budget, reserve)
	}
	budget, reserve = quotaAwareCallBudget(400, 1, now, 15*time.Minute, true, 2)
	if budget != 1 || reserve != 96 {
		t.Fatalf("content must use the one still-available call without crossing the hard cap: budget=%d reserve=%d", budget, reserve)
	}
}

func TestQuotaAwareCallBudgetWithoutLayoutUsesAllUsableQuota(t *testing.T) {
	budget, reserve := quotaAwareCallBudget(2000, 815, time.Now(), 15*time.Minute, false, 0)
	if budget != 815 || reserve != 0 {
		t.Fatalf("budget=%d reserve=%d, want 815/0", budget, reserve)
	}
}
