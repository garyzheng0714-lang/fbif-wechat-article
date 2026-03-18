package sync

import (
	"testing"

	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

func TestExtractArticleIndex(t *testing.T) {
	tests := []struct {
		name  string
		msgid string
		want  *int
		isNil bool
	}{
		{"normal msgid", "abc123_1", intPtr(1), false},
		{"second position", "abc123_2", intPtr(2), false},
		{"multi underscore", "a_b_c_3", intPtr(3), false},
		{"no underscore", "abc123", nil, true},
		{"trailing non-number", "abc_xyz", nil, true},
		{"empty string", "", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractArticleIndex(tt.msgid)
			if tt.isNil {
				if got != nil {
					t.Errorf("extractArticleIndex(%q) = %d, want nil", tt.msgid, *got)
				}
				return
			}
			if got == nil {
				t.Errorf("extractArticleIndex(%q) = nil, want %d", tt.msgid, *tt.want)
				return
			}
			if *got != *tt.want {
				t.Errorf("extractArticleIndex(%q) = %d, want %d", tt.msgid, *got, *tt.want)
			}
		})
	}
}

func intPtr(v int) *int { return &v }

func TestToArticleMasterFields(t *testing.T) {
	item := wechat.ArticleSummaryItem{
		Title:   "Test Article",
		RefDate: "2026-03-18",
		MsgID:   "msg_1",
	}

	total := &wechat.ArticleTotalItem{
		URL: "https://example.com/article",
	}

	fields := toArticleMasterFields(item, total)

	if fields["文章标题"] != "Test Article" {
		t.Errorf("title = %v, want 'Test Article'", fields["文章标题"])
	}
	if fields["消息ID"] != "msg_1" {
		t.Errorf("msgid = %v, want 'msg_1'", fields["消息ID"])
	}
	if fields["发布月份"] != "2026-03" {
		t.Errorf("month = %v, want '2026-03'", fields["发布月份"])
	}
	if idx, ok := fields["文章位置"]; !ok || idx != 1 {
		t.Errorf("index = %v, want 1", idx)
	}
	link, ok := fields["文章链接"].(map[string]string)
	if !ok || link["link"] != "https://example.com/article" {
		t.Errorf("link = %v, want url map", fields["文章链接"])
	}

	// Without totalItem
	fieldsNoTotal := toArticleMasterFields(item, nil)
	if _, ok := fieldsNoTotal["文章链接"]; ok {
		t.Error("Expected no 文章链接 when totalItem is nil")
	}
}

func TestToDailyArticleDataFields(t *testing.T) {
	item := wechat.ArticleSummaryItem{
		MsgID:            "msg_1",
		RefDate:          "2026-03-18",
		IntPageReadUser:  100,
		IntPageReadCount: 200,
		ShareUser:        10,
		ShareCount:       15,
	}

	// Without details
	fields := toDailyArticleDataFields(item, nil)
	if fields["唯一键"] != "msg_1_2026-03-18" {
		t.Errorf("uniqueKey = %v, want 'msg_1_2026-03-18'", fields["唯一键"])
	}
	if fields["图文页阅读人数"] != 100 {
		t.Errorf("intPageReadUser = %v, want 100", fields["图文页阅读人数"])
	}
	if _, ok := fields["送达人数"]; ok {
		t.Error("Expected no 送达人数 when no details")
	}

	// With details
	total := &wechat.ArticleTotalItem{
		Details: []wechat.ArticleTotalDetail{
			{TargetUser: 500, IntPageFromSessionReadUser: 50},
		},
	}
	fieldsWithDetail := toDailyArticleDataFields(item, total)
	if fieldsWithDetail["送达人数"] != 500 {
		t.Errorf("targetUser = %v, want 500", fieldsWithDetail["送达人数"])
	}
	if fieldsWithDetail["会话阅读人数"] != 50 {
		t.Errorf("sessionReadUser = %v, want 50", fieldsWithDetail["会话阅读人数"])
	}
}

func TestToUserGrowthFields(t *testing.T) {
	item := wechat.UserSummaryItem{
		RefDate:    "2026-03-18",
		UserSource: 0,
		NewUser:    10,
		CancelUser: 3,
	}

	cu := 1000
	fields := toUserGrowthFields(item, &cu)

	if fields["用户渠道"] != "其他" {
		t.Errorf("channel = %v, want '其他'", fields["用户渠道"])
	}
	if fields["净增人数"] != 7 {
		t.Errorf("net = %v, want 7", fields["净增人数"])
	}
	if fields["累计关注人数"] != 1000 {
		t.Errorf("cumulate = %v, want 1000", fields["累计关注人数"])
	}
	if fields["唯一键"] != "2026-03-18_0" {
		t.Errorf("uniqueKey = %v, want '2026-03-18_0'", fields["唯一键"])
	}

	// Without cumulate
	fieldsNoCu := toUserGrowthFields(item, nil)
	if _, ok := fieldsNoCu["累计关注人数"]; ok {
		t.Error("Expected no 累计关注人数 when cumulate is nil")
	}
}

func TestToUserReadFields(t *testing.T) {
	item := wechat.UserReadItem{
		RefDate:          "2026-03-18",
		UserSource:       0,
		IntPageReadUser:  200,
		IntPageReadCount: 500,
	}

	fields := toUserReadFields(item)
	if fields["流量来源"] != "会话" {
		t.Errorf("source = %v, want '会话'", fields["流量来源"])
	}
	if fields["唯一键"] != "2026-03-18_0" {
		t.Errorf("uniqueKey = %v, want '2026-03-18_0'", fields["唯一键"])
	}
}

func TestToUserShareFields(t *testing.T) {
	item := wechat.UserShareItem{
		RefDate:    "2026-03-18",
		ShareScene: 1,
		ShareUser:  30,
		ShareCount: 45,
	}

	fields := toUserShareFields(item)
	if fields["分享场景"] != "好友转发" {
		t.Errorf("scene = %v, want '好友转发'", fields["分享场景"])
	}
	if fields["唯一键"] != "2026-03-18_1" {
		t.Errorf("uniqueKey = %v, want '2026-03-18_1'", fields["唯一键"])
	}
}

func TestIsAllEmpty(t *testing.T) {
	// All empty
	result := &FullSyncResult{
		Articles: &ArticleSyncResult{
			Master: &feishu.SyncResult{Created: 0, Skipped: 5},
			Daily:  &feishu.SyncResult{Created: 0, Skipped: 5},
		},
		Users:  &UpsertSyncResult{Total: 0, Created: 0, Updated: 0},
		Reads:  &UpsertSyncResult{Total: 0, Created: 0, Updated: 0},
		Shares: &UpsertSyncResult{Total: 0, Created: 0, Updated: 0},
	}
	if !IsAllEmpty(result) {
		t.Error("Expected all empty")
	}

	// Articles has new data
	result.Articles = &ArticleSyncResult{
		Master: &feishu.SyncResult{Created: 1, Skipped: 0},
		Daily:  &feishu.SyncResult{Created: 1, Skipped: 0},
	}
	if IsAllEmpty(result) {
		t.Error("Expected not empty when articles have created records")
	}

	// Users has data
	result.Articles = &ArticleSyncResult{
		Master: &feishu.SyncResult{Created: 0},
		Daily:  &feishu.SyncResult{Created: 0},
	}
	result.Users = &UpsertSyncResult{Total: 5, Created: 3, Updated: 2}
	if IsAllEmpty(result) {
		t.Error("Expected not empty when users have data")
	}
}
