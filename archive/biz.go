package archive

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 业务数据表格读的数据。口径：
//   - 文章：getarticletotaldetail 里的文章，取发表后 30 天内最新一天的累计值；发表后
//     第 31 天起微信不再更新，这里的值也就定住了。群发的和只发表、没通知粉丝的
//     （publish_type=1）都算。
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

// 文章表「作者」列每一行处理到哪了，按 record_id 记：处理过的行不再碰，
// 所以程序填过、后来被人清空的格子也不会再被填回去。
const (
	AuthorKept    = "kept"    // 表里本来就有作者（人填的），不碰
	AuthorWritten = "written" // 程序填了 value
	AuthorNone    = "none"    // 网页里找不到署名、文章已删或一直打不开，留空
	AuthorGone    = "gone"    // 这一行在表格里被人删了
)

// BizAuthorRecords 是已经处理过的文章行（record_id → 状态）。
func (s *Store) BizAuthorRecords(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT record_id, status FROM official_biz_authors`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	done := make(map[string]string)
	for rows.Next() {
		var recordID, status string
		if err := rows.Scan(&recordID, &status); err != nil {
			return nil, err
		}
		done[recordID] = status
	}
	return done, rows.Err()
}

// SaveBizAuthor 记下一行作者的处理结果；rule 是依据（标题、转载、作者署名、编辑署名…），
// source 是转载来源的公众号编号（gh_…），别的依据为空。
func (s *Store) SaveBizAuthor(ctx context.Context, recordID, msgID, status, value, rule, source string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO official_biz_authors (record_id, msg_id, status, value, rule, source, checked_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(record_id) DO UPDATE SET msg_id = excluded.msg_id, status = excluded.status,
			value = excluded.value, rule = excluded.rule, source = excluded.source, checked_at = excluded.checked_at`,
		recordID, msgID, status, value, rule, source, now.UnixMilli())
	return err
}

// BizAuthorSourceNames 是每个转载来源公众号在表里用的名字（公众号编号 → 名字），一个号只认最早写下的那一种。
func (s *Store) BizAuthorSourceNames(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source, value FROM official_biz_authors
		WHERE source != '' AND value != '' AND status = ?
		ORDER BY checked_at, record_id`, AuthorWritten)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make(map[string]string)
	for rows.Next() {
		var source, value string
		if err := rows.Scan(&source, &value); err != nil {
			return nil, err
		}
		if _, ok := names[source]; !ok {
			names[source] = value
		}
	}
	return names, rows.Err()
}

// BizAuthorReprint 是程序按转载写的一行作者：写的名字和来源公众号编号（没记下编号时为空）。
type BizAuthorReprint struct {
	RecordID, MsgID, Value, Source string
}

// BizAuthorReprints 是程序按转载写的作者行，按写下的先后。规则是「转载」的才算，编号查不到的另记规则、不在里面。
func (s *Store) BizAuthorReprints(ctx context.Context) ([]BizAuthorReprint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT record_id, msg_id, value, source FROM official_biz_authors
		WHERE status = ? AND rule = '转载' AND value != ''
		ORDER BY checked_at, record_id`, AuthorWritten)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BizAuthorReprint
	for rows.Next() {
		var row BizAuthorReprint
		if err := rows.Scan(&row.RecordID, &row.MsgID, &row.Value, &row.Source); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateBizAuthor 改一行的来源编号、规则、名字或状态，不动写下的时间（「表里先写下的那种写法」按它认）。
func (s *Store) UpdateBizAuthor(ctx context.Context, recordID, status, value, rule, source string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE official_biz_authors SET status = ?, value = ?, rule = ?, source = ? WHERE record_id = ?`,
		status, value, rule, source, recordID)
	return err
}

// 品类、国家补填（用户 2026-11-06 定：直接用资讯订阅打的标签）：看过的行记在 official_biz_labels，
// 状态和作者同一套（kept 人填过、written 程序填了、none 资讯订阅没标、gone 行被删了），之后不再碰。

// BizLabelRecords 是已经处理过的文章行（record_id → 状态）。
// 资讯订阅没收、没分类的两种旧记录不算看过（2026-11-07 起这些改为把正文递给资讯订阅的分类接口补），重看一遍。
func (s *Store) BizLabelRecords(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT record_id, status FROM official_biz_labels
		WHERE NOT (status = 'none' AND rule IN ('资讯订阅没有这篇', '资讯订阅没分类'))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	done := make(map[string]string)
	for rows.Next() {
		var recordID, status string
		if err := rows.Scan(&recordID, &status); err != nil {
			return nil, err
		}
		done[recordID] = status
	}
	return done, rows.Err()
}

// SaveBizLabels 记下一行品类、国家的处理结果；category、country 是程序写进去的（逗号隔开），rule 是依据。
func (s *Store) SaveBizLabels(ctx context.Context, recordID, msgID, status, category, country, rule string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO official_biz_labels (record_id, msg_id, status, category, country, rule, checked_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(record_id) DO UPDATE SET msg_id = excluded.msg_id, status = excluded.status, category = excluded.category,
			country = excluded.country, rule = excluded.rule, checked_at = excluded.checked_at`,
		recordID, msgID, status, category, country, rule, now.UnixMilli())
	return err
}

// ArchivedArticleHTML 取归档里这篇文章（链接里的 mid、idx）的正文 HTML，优先已发布接口拿到的；没归档或没正文时返回空。
func (s *Store) ArchivedArticleHTML(ctx context.Context, mid, idx string) (string, error) {
	var content string
	err := s.db.QueryRowContext(ctx, `
		SELECT content_html FROM official_content_articles
		WHERE url LIKE ? AND content_html <> '' AND is_deleted = 0
		ORDER BY CASE source WHEN 'freepublish' THEN 0 ELSE 1 END, last_seen_at DESC
		LIMIT 1`, "%mid="+mid+"&idx="+idx+"&%").Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return content, err
}
