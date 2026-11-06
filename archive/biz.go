package archive

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 业务数据表格和数据汇报卡片读的数据。口径：
//   - 文章：getarticletotaldetail 里的文章，取发表后 30 天内最新一天的累计值；发表后
//     第 31 天起微信不再更新，这里的值也就定住了。卡片只看群发（publish_type=0）的，
//     表格连只发表、没通知粉丝的（publish_type=1）也写。
//   - 每天：getbizsummary 的账号当天阅读、互动和来源，加上 getusersummary 各来源
//     合计的新增/取关和 getusercumulate 的累计关注。
// 这些只是已经采到的数据，不代表历史全量。

type BizArticle struct {
	MsgID        string
	PublishDate  string // YYYY-MM-DD
	Position     int
	Title        string
	URL          string
	StatDate     string // 最新累计值是哪天的
	ReadUser     int64
	ShareUser    int64
	Subscribe    int64 // 阅读后关注
	Zaikan       int64 // 在看（爱心）
	Like         int64 // 点赞（拇指）
	Collection   int64
	Comment      int64
	DeliveryRate float64 // 送达阅读率：公众号消息阅读人数 / 送达人数
	FinishRate   float64
	MessageReads int64 // 从公众号消息进来的阅读人数
	Notified     bool  // 群发时通知了粉丝（publish_type=0）；false 是只发表
	PraiseMoney  int64 // 赞赏金额，分
	AvgMinutes   float64
	// JumpRates 是跳出位置分布：文章进度 0-20%、20-40%、40-60%、60-80%、80-100% 处跳出的人各占多少；
	// 微信没给时为 nil。
	JumpRates []float64
	Sources   map[string]int64 // 阅读来源 scene_desc → 人数
	SeenAt    int64
}

// Delivered 是送达人数：公众号消息阅读人数除以送达阅读率。微信没给送达率时为 0。
func (a BizArticle) Delivered() int64 {
	if a.DeliveryRate <= 0 || a.MessageReads <= 0 {
		return 0
	}
	return int64(float64(a.MessageReads)/a.DeliveryRate + 0.5)
}

// BizArticles 是 [since, until] 发表的全部文章，群发的和只发表、没通知粉丝的都算。
func (s *Store) BizArticles(ctx context.Context, since, until string) ([]BizArticle, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH items AS (
			SELECT msgid, ref_date, title, json_extract(raw_json, '$.content_url') AS content_url,
				COALESCE(json_extract(raw_json, '$.publish_type'), 0) AS publish_type
			FROM official_api_rows
			WHERE endpoint = 'getarticletotaldetail' AND row_scope = 'item'
				AND ref_date >= ? AND ref_date <= ?
		)
		SELECT i.msgid, i.ref_date, i.title, COALESCE(i.content_url, ''), i.publish_type = 0,
			COALESCE(d.stat_date, ''), COALESCE(d.raw_json, '{}'), COALESCE(d.last_seen_at, 0)
		FROM items AS i
		LEFT JOIN official_api_rows AS d ON d.rowid = (
			SELECT latest.rowid FROM official_api_rows AS latest
			WHERE latest.endpoint = 'getarticletotaldetail' AND latest.row_scope = 'detail_list'
				AND latest.msgid = i.msgid
			ORDER BY latest.stat_date DESC LIMIT 1
		)
		ORDER BY i.ref_date, i.msgid`, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	articles := make([]BizArticle, 0)
	for rows.Next() {
		var article BizArticle
		var raw string
		if err := rows.Scan(&article.MsgID, &article.PublishDate, &article.Title, &article.URL, &article.Notified, &article.StatDate, &raw, &article.SeenAt); err != nil {
			return nil, err
		}
		if _, index, ok := strings.Cut(article.MsgID, "_"); ok {
			article.Position, _ = strconv.Atoi(index)
		}
		var detail struct {
			ReadUser       int64   `json:"read_user"`
			ShareUser      int64   `json:"share_user"`
			Subscribe      int64   `json:"read_subscribe_user"`
			Zaikan         int64   `json:"zaikan_user"`
			Like           int64   `json:"like_user"`
			Collection     int64   `json:"collection_user"`
			Comment        int64   `json:"comment_count"`
			DeliveryRate   float64 `json:"read_delivery_rate"`
			FinishRate     float64 `json:"read_finish_rate"`
			PraiseMoney    int64   `json:"praise_money"`
			AvgMinutes     float64 `json:"read_avg_activetime"`
			ReadUserSource []struct {
				UserCount int64  `json:"user_count"`
				SceneDesc string `json:"scene_desc"`
			} `json:"read_user_source"`
			JumpPosition []struct {
				Position int     `json:"position"`
				Rate     float64 `json:"rate"`
			} `json:"read_jump_position"`
		}
		if err := json.Unmarshal([]byte(raw), &detail); err != nil {
			return nil, err
		}
		article.ReadUser, article.ShareUser, article.Subscribe = detail.ReadUser, detail.ShareUser, detail.Subscribe
		article.Zaikan, article.Like, article.Collection, article.Comment = detail.Zaikan, detail.Like, detail.Collection, detail.Comment
		article.DeliveryRate, article.FinishRate = detail.DeliveryRate, detail.FinishRate
		article.PraiseMoney, article.AvgMinutes = detail.PraiseMoney, detail.AvgMinutes
		article.Sources = make(map[string]int64, len(detail.ReadUserSource))
		for _, source := range detail.ReadUserSource {
			article.Sources[source.SceneDesc] = source.UserCount
		}
		article.MessageReads = article.Sources["公众号消息"]
		if len(detail.JumpPosition) > 0 {
			article.JumpRates = make([]float64, 5)
			for _, jump := range detail.JumpPosition {
				if jump.Position >= 1 && jump.Position <= 5 {
					article.JumpRates[jump.Position-1] = jump.Rate
				}
			}
		}
		articles = append(articles, article)
	}
	return articles, rows.Err()
}

type BizDay struct {
	Date             string // YYYY-MM-DD
	HasSummary       bool
	ReadUser         int64
	ShareUser        int64
	Zaikan           int64
	Like             int64
	Comment          int64
	Collection       int64
	SendPages        int64
	Sources          map[string]int64 // 阅读来源 scene_desc → 人数
	RedirectOriginal int64            // 点「阅读原文」的人数
	HasFollower      bool
	NewUser          int64
	CancelUser       int64
	NewBySource      map[int]int64 // getusersummary 的 user_source → 新增关注
	HasCumulate      bool
	Cumulate         int64
	SeenAt           int64
}

func (d BizDay) NetUser() int64 { return d.NewUser - d.CancelUser }

// BizDays 返回 [since, until] 每天一行，按日期升序；只有采到数据的日子才有。
func (s *Store) BizDays(ctx context.Context, since, until string) ([]BizDay, error) {
	days := make(map[string]*BizDay)
	day := func(date string) *BizDay {
		if days[date] == nil {
			days[date] = &BizDay{Date: date, Sources: map[string]int64{}, NewBySource: map[int]int64{}}
		}
		return days[date]
	}
	seen := func(d *BizDay, at int64) {
		if at > d.SeenAt {
			d.SeenAt = at
		}
	}

	summary, err := s.db.QueryContext(ctx, `
		SELECT ref_date, raw_json, last_seen_at FROM official_api_rows
		WHERE endpoint = 'getbizsummary' AND row_scope = 'item' AND ref_date >= ? AND ref_date <= ?`, since, until)
	if err != nil {
		return nil, err
	}
	for summary.Next() {
		var date, raw string
		var at int64
		if err := summary.Scan(&date, &raw, &at); err != nil {
			summary.Close()
			return nil, err
		}
		var payload struct {
			Detail struct {
				ReadUser       int64 `json:"read_user"`
				ShareUser      int64 `json:"share_user"`
				Zaikan         int64 `json:"zaikan_user"`
				Like           int64 `json:"like_user"`
				Comment        int64 `json:"comment_count"`
				Collection     int64 `json:"collection_user"`
				SendPages      int64 `json:"send_page_count"`
				Redirect       int64 `json:"redirect_ori_page_user"`
				ReadUserSource []struct {
					UserCount int64  `json:"user_count"`
					SceneDesc string `json:"scene_desc"`
				} `json:"read_user_source"`
			} `json:"detail"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			summary.Close()
			return nil, err
		}
		d := day(date)
		detail := payload.Detail
		d.HasSummary = true
		d.ReadUser, d.ShareUser, d.Zaikan, d.Like = detail.ReadUser, detail.ShareUser, detail.Zaikan, detail.Like
		d.Comment, d.Collection, d.SendPages, d.RedirectOriginal = detail.Comment, detail.Collection, detail.SendPages, detail.Redirect
		for _, source := range detail.ReadUserSource {
			d.Sources[source.SceneDesc] = source.UserCount
		}
		seen(d, at)
	}
	if err := summary.Close(); err != nil {
		return nil, err
	}

	followers, err := s.db.QueryContext(ctx, `
		SELECT ref_date, COALESCE(CAST(json_extract(raw_json, '$.user_source') AS INTEGER), 0),
			SUM(COALESCE(CAST(json_extract(raw_json, '$.new_user') AS INTEGER), 0)),
			SUM(COALESCE(CAST(json_extract(raw_json, '$.cancel_user') AS INTEGER), 0)),
			MAX(last_seen_at)
		FROM official_api_rows
		WHERE endpoint = 'getusersummary' AND ref_date >= ? AND ref_date <= ?
		GROUP BY 1, 2`, since, until)
	if err != nil {
		return nil, err
	}
	for followers.Next() {
		var date string
		var source int
		var newUser, cancelUser, at int64
		if err := followers.Scan(&date, &source, &newUser, &cancelUser, &at); err != nil {
			followers.Close()
			return nil, err
		}
		d := day(date)
		d.HasFollower = true
		d.NewUser += newUser
		d.CancelUser += cancelUser
		d.NewBySource[source] += newUser
		seen(d, at)
	}
	if err := followers.Close(); err != nil {
		return nil, err
	}

	cumulate, err := s.db.QueryContext(ctx, `
		SELECT ref_date, MAX(COALESCE(CAST(json_extract(raw_json, '$.cumulate_user') AS INTEGER), 0)), MAX(last_seen_at)
		FROM official_api_rows
		WHERE endpoint = 'getusercumulate' AND ref_date >= ? AND ref_date <= ?
		GROUP BY ref_date`, since, until)
	if err != nil {
		return nil, err
	}
	for cumulate.Next() {
		var date string
		var total, at int64
		if err := cumulate.Scan(&date, &total, &at); err != nil {
			cumulate.Close()
			return nil, err
		}
		d := day(date)
		d.HasCumulate, d.Cumulate = true, total
		seen(d, at)
	}
	if err := cumulate.Close(); err != nil {
		return nil, err
	}

	result := make([]BizDay, 0, len(days))
	for _, d := range days {
		result = append(result, *d)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result, nil
}

// BaseRecord 是一行在飞书里的 record_id 和上次写进去的内容摘要。
type BaseRecord struct {
	RecordID string
	Hash     string
}

func (s *Store) BaseRecords(ctx context.Context, dataset string) (map[string]BaseRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT row_key, record_id, payload_sha256 FROM official_base_records WHERE dataset = ?`, dataset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make(map[string]BaseRecord)
	for rows.Next() {
		var key string
		var record BaseRecord
		if err := rows.Scan(&key, &record.RecordID, &record.Hash); err != nil {
			return nil, err
		}
		records[key] = record
	}
	return records, rows.Err()
}

func (s *Store) BizState(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM official_biz_state WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *Store) SetBizState(ctx context.Context, key, value string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO official_biz_state (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, now.UnixMilli())
	return err
}
