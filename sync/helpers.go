package sync

import (
	"fmt"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

func nowMs() int64 {
	return time.Now().UnixMilli()
}

func toPublishMonth(dateStr string) string {
	t, err := wechat.ParseDate(dateStr)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d-%02d", t.Year(), t.Month())
}
