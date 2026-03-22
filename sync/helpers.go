package sync

import (
	"fmt"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
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

func dedupeSyncRecords(records []feishu.SyncRecord) []feishu.SyncRecord {
	if len(records) <= 1 {
		return records
	}

	byKey := make(map[string]feishu.SyncRecord, len(records))
	for _, record := range records {
		if record.UniqueKey == "" {
			continue
		}
		existing, ok := byKey[record.UniqueKey]
		if !ok {
			byKey[record.UniqueKey] = record
			continue
		}

		merged := existing
		if merged.Fields == nil {
			merged.Fields = map[string]interface{}{}
		}
		for k, v := range record.Fields {
			if !hasMeaningfulValue(merged.Fields[k]) && hasMeaningfulValue(v) {
				merged.Fields[k] = v
			}
		}
		byKey[record.UniqueKey] = merged
	}

	out := make([]feishu.SyncRecord, 0, len(byKey))
	for _, record := range byKey {
		out = append(out, record)
	}
	return out
}

func hasMeaningfulValue(v interface{}) bool {
	if v == nil {
		return false
	}
	switch t := v.(type) {
	case string:
		return t != ""
	case map[string]string:
		return len(t) > 0
	case map[string]interface{}:
		return len(t) > 0
	}
	return true
}
