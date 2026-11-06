package archive

import (
	"context"
	"fmt"
	"time"
)

// DatesShortOfFinalDay 列出 [earliest, latest] 里还有文章没采到最后一天（发表日+finalOffsetDays）的发表日，新的在前。
// 指定走 (endpoint, row_scope, msgid, stat_date) 索引：只过一篇一行的 item 行（一千多行），每篇最新 stat_date 也在这条索引里取；
// 不指定的话 SQLite 会挑 (endpoint, ref_date)，把几万行分日数据一起扫一遍，冷缓存时服务器上要 9 秒。
func (s *Store) DatesShortOfFinalDay(ctx context.Context, endpoint, earliest, latest string, finalOffsetDays int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.ref_date
		FROM official_api_rows AS i INDEXED BY idx_official_api_rows_metric_join
		WHERE i.endpoint = ? AND i.row_scope = 'item' AND i.msgid != ''
			AND i.ref_date >= ? AND i.ref_date <= ?
			AND COALESCE((
				SELECT MAX(d.stat_date) FROM official_api_rows AS d
				WHERE d.endpoint = i.endpoint AND d.row_scope = 'detail_list' AND d.msgid = i.msgid
			), '') < date(i.ref_date, ?)
		GROUP BY i.ref_date
		ORDER BY i.ref_date DESC`,
		endpoint, earliest, latest, fmt.Sprintf("+%d day", finalOffsetDays))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dates []string
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			return nil, err
		}
		dates = append(dates, date)
	}
	return dates, rows.Err()
}

// FetchedSuccessfullySince 说这个窗口在 since 及以后有没有成功拉过（微信说数据延迟的不算）。
func (s *Store) FetchedSuccessfullySince(ctx context.Context, endpoint, beginDate, endDate string, since time.Time) (bool, error) {
	var found int
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM official_api_fetches
			WHERE endpoint = ? AND begin_date = ? AND end_date = ? AND fetched_at >= ? AND success = 1
		)`, endpoint, beginDate, endDate, since.UnixMilli()).Scan(&found)
	return found == 1, err
}
