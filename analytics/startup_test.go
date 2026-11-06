package analytics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

// 2026-11-06 服务一天重启 17 次，08:05 后每次启动都补跑一整轮，文章数据接口一天用到 460 次。
// 今天这一轮已经跑完的，启动不再补跑；没跑完、失败了的，照旧补跑。
func TestRanSinceOnlyWhenEveryActiveEndpointSucceededAfterSlot(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	slot := time.Date(2026, 11, 6, 8, 5, 0, 0, wechat.ShanghaiLoc())
	now := slot.Add(time.Minute)
	collector := &Collector{Client: &fakeClient{}, Store: store, Now: func() time.Time { return now }, MaxCalls: 15}

	if ran, err := collector.RanSince(context.Background(), slot); err != nil || ran {
		t.Fatalf("before any run: ran=%v err=%v", ran, err)
	}
	if _, err := collector.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ran, err := collector.RanSince(context.Background(), slot); err != nil || !ran {
		t.Fatalf("after today's run: ran=%v err=%v", ran, err)
	}
	if ran, _ := collector.RanSince(context.Background(), slot.AddDate(0, 0, 1)); ran {
		t.Fatal("yesterday's run must not count for tomorrow's slot")
	}
}

func TestRanSinceFalseWhenAnEndpointFailed(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	slot := time.Date(2026, 11, 6, 8, 5, 0, 0, wechat.ShanghaiLoc())
	now := slot.Add(time.Minute)
	client := &fakeClient{fail: map[string]error{"getbizsummary": errors.New("boom")}}
	collector := &Collector{Client: client, Store: store, Now: func() time.Time { return now }, MaxCalls: 15}
	_, _ = collector.Run(context.Background())
	if ran, err := collector.RanSince(context.Background(), slot); err != nil || ran {
		t.Fatalf("a failed endpoint must make the next start retry: ran=%v err=%v", ran, err)
	}
}
