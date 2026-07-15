package archive

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("analytics database path is empty")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("create analytics database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open analytics database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA busy_timeout = 10000`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = NORMAL`,
		`CREATE TABLE IF NOT EXISTS official_api_fetches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			endpoint TEXT NOT NULL,
			category TEXT NOT NULL,
			begin_date TEXT NOT NULL DEFAULT '',
			end_date TEXT NOT NULL DEFAULT '',
			request_json TEXT NOT NULL DEFAULT '',
			response_json BLOB,
			response_ref_id INTEGER,
			response_sha256 TEXT NOT NULL DEFAULT '',
			http_status INTEGER NOT NULL DEFAULT 0,
			wechat_errcode INTEGER NOT NULL DEFAULT 0,
			wechat_errmsg TEXT NOT NULL DEFAULT '',
			success INTEGER NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			fetched_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_official_api_fetches_endpoint_date
			ON official_api_fetches(endpoint, begin_date, end_date, fetched_at)`,
		`CREATE INDEX IF NOT EXISTS idx_official_api_fetches_failures
			ON official_api_fetches(success, fetched_at)`,
		`CREATE TABLE IF NOT EXISTS official_api_state (
			endpoint TEXT PRIMARY KEY,
			category TEXT NOT NULL,
			next_backfill_date TEXT NOT NULL DEFAULT '',
			last_success_begin TEXT NOT NULL DEFAULT '',
			last_success_end TEXT NOT NULL DEFAULT '',
			last_success_at INTEGER NOT NULL DEFAULT 0,
			last_attempt_at INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS official_api_rows (
			endpoint TEXT NOT NULL,
			row_key TEXT NOT NULL,
			row_scope TEXT NOT NULL DEFAULT 'item',
			ref_date TEXT NOT NULL DEFAULT '',
			stat_date TEXT NOT NULL DEFAULT '',
			msgid TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			dimensions_json TEXT NOT NULL DEFAULT '{}',
			raw_json TEXT NOT NULL,
			first_seen_at INTEGER NOT NULL,
			last_seen_at INTEGER NOT NULL,
			PRIMARY KEY(endpoint, row_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_official_api_rows_article
			ON official_api_rows(msgid, ref_date, stat_date)`,
		`CREATE INDEX IF NOT EXISTS idx_official_api_rows_endpoint_date
			ON official_api_rows(endpoint, ref_date, stat_date)`,
		`CREATE INDEX IF NOT EXISTS idx_official_api_rows_metric_join
			ON official_api_rows(endpoint, row_scope, msgid, stat_date)`,
		`CREATE TABLE IF NOT EXISTS official_content_objects (
			source TEXT NOT NULL,
			object_id TEXT NOT NULL,
			update_time INTEGER NOT NULL DEFAULT 0,
			raw_json TEXT NOT NULL,
			detail_response BLOB,
			detail_fetched_at INTEGER NOT NULL DEFAULT 0,
			first_seen_at INTEGER NOT NULL,
			last_seen_at INTEGER NOT NULL,
			PRIMARY KEY(source, object_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_official_content_objects_detail
			ON official_content_objects(source, detail_fetched_at, update_time)`,
		`CREATE TABLE IF NOT EXISTS official_content_articles (
			source TEXT NOT NULL,
			object_id TEXT NOT NULL,
			article_index INTEGER NOT NULL,
			article_type TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			author TEXT NOT NULL DEFAULT '',
			digest TEXT NOT NULL DEFAULT '',
			content_html TEXT NOT NULL DEFAULT '',
			content_source_url TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL DEFAULT '',
			thumb_media_id TEXT NOT NULL DEFAULT '',
			thumb_url TEXT NOT NULL DEFAULT '',
			is_deleted INTEGER NOT NULL DEFAULT 0,
			raw_json TEXT NOT NULL,
			first_seen_at INTEGER NOT NULL,
			last_seen_at INTEGER NOT NULL,
			PRIMARY KEY(source, object_id, article_index)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_official_content_articles_url
			ON official_content_articles(url)`,
		`CREATE INDEX IF NOT EXISTS idx_official_content_articles_type_match
			ON official_content_articles(source, article_index, title)`,
		`CREATE TABLE IF NOT EXISTS official_content_state (
			stream TEXT PRIMARY KEY,
			next_offset INTEGER NOT NULL DEFAULT 0,
			total_count INTEGER NOT NULL DEFAULT 0,
			complete INTEGER NOT NULL DEFAULT 0,
			last_success_at INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS official_layout_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			initialized_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS official_layout_outbox (
			source_key TEXT PRIMARY KEY,
			source_url TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			source_name TEXT NOT NULL DEFAULT '',
			author TEXT NOT NULL DEFAULT '',
			content_html TEXT NOT NULL,
			cover_url TEXT NOT NULL DEFAULT '',
			published_at INTEGER NOT NULL DEFAULT 0,
			discovered_via TEXT NOT NULL,
			source_ref TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			next_attempt_at INTEGER NOT NULL DEFAULT 0,
			layout_job_id INTEGER NOT NULL DEFAULT 0,
			layout_stage TEXT NOT NULL DEFAULT '',
			existing_job INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			delivered_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_official_layout_outbox_due
			ON official_layout_outbox(status, next_attempt_at, created_at)`,
		`CREATE VIEW IF NOT EXISTS official_article_metrics AS
			SELECT
				endpoint,
				row_scope,
				ref_date,
				stat_date,
				msgid,
				title,
				dimensions_json,
				COALESCE(json_extract(raw_json, '$.read_user'), json_extract(raw_json, '$.detail.read_user'), json_extract(raw_json, '$.int_page_read_user')) AS read_user,
				COALESCE(json_extract(raw_json, '$.read_count'), json_extract(raw_json, '$.detail.read_count'), json_extract(raw_json, '$.int_page_read_count')) AS read_count,
				json_extract(raw_json, '$.ori_page_read_user') AS original_read_user,
				json_extract(raw_json, '$.ori_page_read_count') AS original_read_count,
				COALESCE(json_extract(raw_json, '$.share_user'), json_extract(raw_json, '$.detail.share_user')) AS share_user,
				json_extract(raw_json, '$.share_count') AS share_count,
				json_extract(raw_json, '$.add_to_fav_user') AS favorite_user,
				json_extract(raw_json, '$.add_to_fav_count') AS favorite_count,
				json_extract(raw_json, '$.zaikan_user') AS zaikan_user,
				json_extract(raw_json, '$.like_user') AS like_user,
				json_extract(raw_json, '$.comment_count') AS comment_count,
				json_extract(raw_json, '$.collection_user') AS collection_user,
				json_extract(raw_json, '$.praise_money') AS praise_money,
				json_extract(raw_json, '$.read_subscribe_user') AS read_subscribe_user,
				json_extract(raw_json, '$.read_delivery_rate') AS read_delivery_rate,
				json_extract(raw_json, '$.read_finish_rate') AS read_finish_rate,
				json_extract(raw_json, '$.read_avg_activetime') AS read_avg_activetime,
				raw_json,
				last_seen_at
			FROM official_api_rows
			WHERE endpoint IN ('getarticleread', 'getarticleshare', 'getarticlesummary', 'getarticletotal', 'getarticletotaldetail')`,
		`CREATE VIEW IF NOT EXISTS official_article_publications AS
			WITH ranked AS (
				SELECT
					msgid,
					ref_date,
					title,
					dimensions_json,
					raw_json,
					last_seen_at,
					ROW_NUMBER() OVER (
						PARTITION BY msgid ORDER BY last_seen_at DESC
					) AS recency_rank
				FROM official_api_rows
				WHERE endpoint = 'getarticletotaldetail'
					AND row_scope = 'item'
					AND msgid <> ''
			)
			SELECT
				msgid,
				CASE
					WHEN instr(msgid, '_') > 0 THEN substr(msgid, 1, instr(msgid, '_') - 1)
					ELSE msgid
				END AS msg_data_id,
				CASE
					WHEN instr(msgid, '_') > 0 THEN CAST(substr(msgid, instr(msgid, '_') + 1) AS INTEGER)
					ELSE 0
				END AS article_index,
				ref_date AS publish_date,
				CAST(COALESCE(
					json_extract(dimensions_json, '$.publish_type'),
					json_extract(raw_json, '$.publish_type')
				) AS INTEGER) AS publish_type,
				title,
				COALESCE(json_extract(raw_json, '$.content_url'), '') AS content_url,
				last_seen_at
			FROM ranked
			WHERE recency_rank = 1`,
		`CREATE VIEW IF NOT EXISTS official_article_catalog AS
			WITH ranked_content AS (
				SELECT
					*,
					ROW_NUMBER() OVER (
						PARTITION BY url ORDER BY last_seen_at DESC, object_id
					) AS recency_rank
				FROM official_content_articles
				WHERE source = 'freepublish' AND url <> ''
			)
			SELECT
				p.msgid,
				p.msg_data_id,
				p.article_index,
				p.publish_date,
				p.publish_type,
				p.title,
				p.content_url,
				c.object_id AS article_id,
				c.article_index AS content_article_index,
				c.author,
				c.digest,
				c.content_html,
				c.content_source_url,
				c.thumb_media_id,
				c.thumb_url,
				c.article_type,
				c.is_deleted,
				p.last_seen_at
			FROM official_article_publications AS p
			LEFT JOIN ranked_content AS c
				ON c.url = p.content_url AND c.recency_rank = 1`,
		`CREATE VIEW IF NOT EXISTS official_article_metric_facts AS
			SELECT
				m.endpoint,
				m.row_scope,
				m.ref_date,
				m.stat_date,
				COALESCE(NULLIF(m.stat_date, ''), m.ref_date) AS metric_date,
				m.msgid,
				p.msg_data_id,
				p.article_index,
				p.publish_date,
				COALESCE(NULLIF(m.title, ''), p.title) AS title,
				p.content_url,
				p.article_id,
				p.content_article_index,
				m.dimensions_json,
				m.read_user,
				m.read_count,
				m.original_read_user,
				m.original_read_count,
				m.share_user,
				m.share_count,
				m.favorite_user,
				m.favorite_count,
				m.zaikan_user,
				m.like_user,
				m.comment_count,
				m.collection_user,
				m.praise_money,
				m.read_subscribe_user,
				m.read_delivery_rate,
				m.read_finish_rate,
				m.read_avg_activetime,
				m.raw_json,
				m.last_seen_at
			FROM official_article_metrics AS m
			LEFT JOIN official_article_catalog AS p ON p.msgid = m.msgid
			WHERE m.msgid <> ''
				AND NOT (
					m.endpoint IN ('getarticletotal', 'getarticletotaldetail')
					AND m.row_scope = 'item'
				)`,
		`CREATE VIEW IF NOT EXISTS official_follower_metric_facts AS
			SELECT
				endpoint,
				ref_date,
				CAST(json_extract(dimensions_json, '$.user_source') AS INTEGER) AS user_source,
				CAST(json_extract(raw_json, '$.new_user') AS INTEGER) AS new_user,
				CAST(json_extract(raw_json, '$.cancel_user') AS INTEGER) AS cancel_user,
				CAST(json_extract(raw_json, '$.new_user') AS INTEGER)
					- CAST(json_extract(raw_json, '$.cancel_user') AS INTEGER) AS net_new_user,
				CAST(json_extract(raw_json, '$.cumulate_user') AS INTEGER) AS cumulate_user,
				raw_json,
				last_seen_at
			FROM official_api_rows
			WHERE endpoint IN ('getusersummary', 'getusercumulate')`,
		`CREATE VIEW IF NOT EXISTS official_article_latest_performance AS
			WITH ranked AS (
				SELECT
					*,
					ROW_NUMBER() OVER (
						PARTITION BY msgid ORDER BY stat_date DESC, last_seen_at DESC
					) AS recency_rank
				FROM official_article_metric_facts
				WHERE endpoint = 'getarticletotaldetail'
			)
			SELECT * FROM ranked WHERE recency_rank = 1`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate analytics database: %w", err)
		}
	}
	if err := s.ensureColumn(ctx, "official_api_fetches", "response_ref_id", "INTEGER"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "official_layout_outbox", "published_at", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "official_content_articles", "article_type", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE official_content_articles
		SET article_type = json_extract(raw_json, '$.article_type')
		WHERE article_type = '' AND json_type(raw_json, '$.article_type') = 'text'`); err != nil {
		return fmt.Errorf("backfill official article type: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE VIEW IF NOT EXISTS official_api_fetch_payloads AS
		SELECT
			f.id,
			f.endpoint,
			f.request_json,
			COALESCE(f.response_json, original.response_json) AS response_json,
			f.response_sha256,
			f.fetched_at
		FROM official_api_fetches AS f
		LEFT JOIN official_api_fetches AS original ON original.id = f.response_ref_id`); err != nil {
		return fmt.Errorf("create API payload view: %w", err)
	}
	return nil
}

func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

type ContentPageInfo struct {
	TotalCount int
	ItemCount  int
	ObjectIDs  []string
}

func (s *Store) SaveContentPage(ctx context.Context, source string, responseJSON []byte, fetchedAt time.Time) (ContentPageInfo, error) {
	var page struct {
		TotalCount int               `json:"total_count"`
		ItemCount  int               `json:"item_count"`
		Item       []json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(responseJSON, &page); err != nil {
		return ContentPageInfo{}, fmt.Errorf("decode %s content page: %w", source, err)
	}
	if fetchedAt.IsZero() {
		fetchedAt = time.Now()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ContentPageInfo{}, err
	}
	defer func() { _ = tx.Rollback() }()

	info := ContentPageInfo{TotalCount: page.TotalCount, ItemCount: page.ItemCount}
	for _, raw := range page.Item {
		objectID, err := upsertContentObject(ctx, tx, source, raw, fetchedAt)
		if err != nil {
			return ContentPageInfo{}, err
		}
		if objectID != "" {
			info.ObjectIDs = append(info.ObjectIDs, objectID)
		}
	}
	if err := tx.Commit(); err != nil {
		return ContentPageInfo{}, err
	}
	return info, nil
}

func upsertContentObject(ctx context.Context, tx *sql.Tx, source string, raw json.RawMessage, fetchedAt time.Time) (string, error) {
	var item map[string]json.RawMessage
	if err := json.Unmarshal(raw, &item); err != nil {
		return "", fmt.Errorf("decode %s content object: %w", source, err)
	}
	objectID := stringField(item, "article_id")
	if objectID == "" {
		objectID = stringField(item, "media_id")
	}
	if objectID == "" {
		return "", nil
	}
	updateTime := int64Field(item, "update_time")
	nowMs := fetchedAt.UnixMilli()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO official_content_objects (
			source, object_id, update_time, raw_json, first_seen_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(source, object_id) DO UPDATE SET
			detail_fetched_at = CASE
				WHEN excluded.update_time > official_content_objects.update_time THEN 0
				ELSE official_content_objects.detail_fetched_at
			END,
			update_time = excluded.update_time,
			raw_json = excluded.raw_json,
			last_seen_at = excluded.last_seen_at`,
		source, objectID, updateTime, string(raw), nowMs, nowMs)
	if err != nil {
		return "", fmt.Errorf("upsert %s content object: %w", source, err)
	}

	articles := newsItemsFromObject(item)
	for index, article := range articles {
		if err := upsertContentArticle(ctx, tx, source, objectID, index, article, fetchedAt); err != nil {
			return "", err
		}
	}
	return objectID, nil
}

func (s *Store) SaveContentDetail(ctx context.Context, source, objectID string, response []byte, fetchedAt time.Time) error {
	if fetchedAt.IsZero() {
		fetchedAt = time.Now()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	nowMs := fetchedAt.UnixMilli()
	if _, err := tx.ExecContext(ctx, `
		UPDATE official_content_objects
		SET detail_response = ?, detail_fetched_at = ?, last_seen_at = ?
		WHERE source = ? AND object_id = ?`,
		response, nowMs, nowMs, source, objectID); err != nil {
		return fmt.Errorf("save %s content detail: %w", source, err)
	}

	if json.Valid(response) {
		var detail map[string]json.RawMessage
		if json.Unmarshal(response, &detail) == nil {
			for index, article := range newsItemsFromObject(detail) {
				if err := upsertContentArticle(ctx, tx, source, objectID, index, article, fetchedAt); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

func newsItemsFromObject(item map[string]json.RawMessage) []json.RawMessage {
	var direct []json.RawMessage
	if json.Unmarshal(item["news_item"], &direct) == nil {
		return direct
	}
	var content struct {
		NewsItem []json.RawMessage `json:"news_item"`
	}
	if json.Unmarshal(item["content"], &content) == nil {
		return content.NewsItem
	}
	return nil
}

func upsertContentArticle(ctx context.Context, tx *sql.Tx, source, objectID string, index int, raw json.RawMessage, fetchedAt time.Time) error {
	var article map[string]json.RawMessage
	if err := json.Unmarshal(raw, &article); err != nil {
		return fmt.Errorf("decode %s article: %w", source, err)
	}
	nowMs := fetchedAt.UnixMilli()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO official_content_articles (
			source, object_id, article_index, article_type, title, author, digest, content_html,
			content_source_url, url, thumb_media_id, thumb_url, is_deleted,
			raw_json, first_seen_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source, object_id, article_index) DO UPDATE SET
			article_type = CASE
				WHEN excluded.article_type <> '' THEN excluded.article_type
				ELSE official_content_articles.article_type
			END,
			title = excluded.title,
			author = excluded.author,
			digest = excluded.digest,
			content_html = excluded.content_html,
			content_source_url = excluded.content_source_url,
			url = excluded.url,
			thumb_media_id = excluded.thumb_media_id,
			thumb_url = excluded.thumb_url,
			is_deleted = excluded.is_deleted,
			raw_json = excluded.raw_json,
			last_seen_at = excluded.last_seen_at`,
		source,
		objectID,
		index,
		stringField(article, "article_type"),
		stringField(article, "title"),
		stringField(article, "author"),
		stringField(article, "digest"),
		stringField(article, "content"),
		stringField(article, "content_source_url"),
		stringField(article, "url"),
		stringField(article, "thumb_media_id"),
		stringField(article, "thumb_url"),
		boolOrIntField(article, "is_deleted"),
		string(raw),
		nowMs,
		nowMs,
	)
	if err != nil {
		return fmt.Errorf("upsert %s article: %w", source, err)
	}
	return nil
}

func (s *Store) ListObjectsNeedingDetail(ctx context.Context, source string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT object_id FROM official_content_objects
		WHERE source = ? AND detail_fetched_at = 0
		ORDER BY update_time DESC, object_id
		LIMIT ?`, source, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var objectID string
		if err := rows.Scan(&objectID); err != nil {
			return nil, err
		}
		result = append(result, objectID)
	}
	return result, rows.Err()
}

func (s *Store) ArticleMessageIDs(ctx context.Context, refDate string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT msgid
		FROM official_api_rows
		WHERE ref_date = ? AND msgid <> ''
		ORDER BY msgid
		LIMIT ?`, refDate, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var msgID string
		if err := rows.Scan(&msgID); err != nil {
			return nil, err
		}
		result = append(result, msgID)
	}
	return result, rows.Err()
}

type ContentState struct {
	Stream        string `json:"stream"`
	NextOffset    int    `json:"nextOffset"`
	TotalCount    int    `json:"totalCount"`
	Complete      bool   `json:"complete"`
	LastSuccessAt int64  `json:"lastSuccessAt"`
	LastError     string `json:"lastError"`
	UpdatedAt     int64  `json:"updatedAt"`
}

func (s *Store) GetContentState(ctx context.Context, stream string) (*ContentState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT stream, next_offset, total_count, complete, last_success_at,
			last_error, updated_at
		FROM official_content_state WHERE stream = ?`, stream)
	var state ContentState
	var complete int
	if err := row.Scan(&state.Stream, &state.NextOffset, &state.TotalCount, &complete, &state.LastSuccessAt, &state.LastError, &state.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	state.Complete = complete != 0
	return &state, nil
}

func (s *Store) ListContentStates(ctx context.Context) ([]ContentState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT stream, next_offset, total_count, complete, last_success_at,
			last_error, updated_at
		FROM official_content_state ORDER BY stream`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := make([]ContentState, 0)
	for rows.Next() {
		var state ContentState
		var complete int
		if err := rows.Scan(&state.Stream, &state.NextOffset, &state.TotalCount, &complete, &state.LastSuccessAt, &state.LastError, &state.UpdatedAt); err != nil {
			return nil, err
		}
		state.Complete = complete != 0
		states = append(states, state)
	}
	return states, rows.Err()
}

func (s *Store) MarkContentPageSuccess(ctx context.Context, stream string, nextOffset, totalCount int, complete bool, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	nowMs := now.UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO official_content_state (
			stream, next_offset, total_count, complete, last_success_at,
			last_error, updated_at
		) VALUES (?, ?, ?, ?, ?, '', ?)
		ON CONFLICT(stream) DO UPDATE SET
			next_offset = excluded.next_offset,
			total_count = excluded.total_count,
			complete = excluded.complete,
			last_success_at = excluded.last_success_at,
			last_error = '',
			updated_at = excluded.updated_at`,
		stream, nextOffset, totalCount, boolInt(complete), nowMs, nowMs)
	return err
}

func (s *Store) MarkContentFailure(ctx context.Context, stream, message string, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	nowMs := now.UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO official_content_state (stream, last_error, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(stream) DO UPDATE SET
			last_error = excluded.last_error,
			updated_at = excluded.updated_at`, stream, message, nowMs)
	return err
}

func int64Field(item map[string]json.RawMessage, key string) int64 {
	raw, ok := item[key]
	if !ok {
		return 0
	}
	var value int64
	_ = json.Unmarshal(raw, &value)
	return value
}

func boolOrIntField(item map[string]json.RawMessage, key string) int {
	raw, ok := item[key]
	if !ok {
		return 0
	}
	var boolean bool
	if json.Unmarshal(raw, &boolean) == nil {
		return boolInt(boolean)
	}
	var number int
	_ = json.Unmarshal(raw, &number)
	return number
}

type FetchRecord struct {
	Endpoint      string
	Category      string
	BeginDate     string
	EndDate       string
	RequestJSON   []byte
	ResponseJSON  []byte
	HTTPStatus    int
	WechatErrCode int
	WechatErrMsg  string
	Success       bool
	Error         string
	FetchedAt     time.Time
}

func (s *Store) SaveFetch(ctx context.Context, record FetchRecord) (int64, error) {
	if record.FetchedAt.IsZero() {
		record.FetchedAt = time.Now()
	}
	responseHash := ""
	if len(record.ResponseJSON) > 0 {
		hash := sha256.Sum256(record.ResponseJSON)
		responseHash = hex.EncodeToString(hash[:])
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	storedResponse := record.ResponseJSON
	var responseRefID interface{}
	if responseHash != "" {
		var existingID int64
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM official_api_fetches
			WHERE endpoint = ? AND request_json = ? AND response_sha256 = ?
				AND response_json IS NOT NULL
			ORDER BY id DESC LIMIT 1`, record.Endpoint, string(record.RequestJSON), responseHash).Scan(&existingID)
		if err == nil {
			storedResponse = nil
			responseRefID = existingID
		} else if err != sql.ErrNoRows {
			return 0, fmt.Errorf("find duplicate API response: %w", err)
		}
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO official_api_fetches (
			endpoint, category, begin_date, end_date, request_json, response_json,
			response_ref_id, response_sha256, http_status, wechat_errcode, wechat_errmsg,
			success, error, fetched_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Endpoint,
		record.Category,
		record.BeginDate,
		record.EndDate,
		string(record.RequestJSON),
		storedResponse,
		responseRefID,
		responseHash,
		record.HTTPStatus,
		record.WechatErrCode,
		record.WechatErrMsg,
		boolInt(record.Success),
		record.Error,
		record.FetchedAt.UnixMilli(),
	)
	if err != nil {
		return 0, fmt.Errorf("insert API fetch: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	if record.Success && len(record.ResponseJSON) > 0 {
		if err := upsertResponseRows(ctx, tx, record); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func upsertResponseRows(ctx context.Context, tx *sql.Tx, record FetchRecord) error {
	if !json.Valid(record.ResponseJSON) {
		// Some official material-detail calls return binary media. The exact
		// bytes are still archived in official_api_fetches; there are simply no
		// JSON rows to normalize.
		return nil
	}
	var response struct {
		List []json.RawMessage `json:"list"`
	}
	if err := json.Unmarshal(record.ResponseJSON, &response); err != nil {
		return fmt.Errorf("decode successful %s response: %w", record.Endpoint, err)
	}
	for index, raw := range response.List {
		if err := upsertRow(ctx, tx, record, "item", index, raw, nil); err != nil {
			return err
		}

		var item map[string]json.RawMessage
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		for _, nestedField := range []string{"details", "detail_list"} {
			var nested []json.RawMessage
			if json.Unmarshal(item[nestedField], &nested) != nil {
				continue
			}
			for nestedIndex, nestedRaw := range nested {
				if err := upsertRow(ctx, tx, record, nestedField, nestedIndex, nestedRaw, item); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func upsertRow(ctx context.Context, tx *sql.Tx, record FetchRecord, scope string, index int, raw json.RawMessage, parent map[string]json.RawMessage) error {
	var item map[string]json.RawMessage
	if err := json.Unmarshal(raw, &item); err != nil {
		return fmt.Errorf("decode %s response row: %w", record.Endpoint, err)
	}

	refDate := stringField(item, "ref_date")
	statDate := stringField(item, "stat_date")
	msgID := stringField(item, "msgid")
	title := stringField(item, "title")
	if parent != nil {
		if refDate == "" {
			refDate = stringField(parent, "ref_date")
		}
		if msgID == "" {
			msgID = stringField(parent, "msgid")
		}
		if title == "" {
			title = stringField(parent, "title")
		}
	}

	dimensions := make(map[string]interface{})
	for _, key := range []string{"user_source", "share_scene", "ref_hour", "msg_type", "count_interval", "publish_type"} {
		if value, ok := scalarField(item, key); ok {
			dimensions[key] = value
		} else if parent != nil {
			if value, ok := scalarField(parent, key); ok {
				dimensions[key] = value
			}
		}
	}
	dimensionsJSON, _ := json.Marshal(dimensions)

	identity := map[string]interface{}{
		"endpoint":   record.Endpoint,
		"scope":      scope,
		"begin_date": record.BeginDate,
		"end_date":   record.EndDate,
		"ref_date":   refDate,
		"stat_date":  statDate,
		"msgid":      msgID,
		"dimensions": dimensions,
	}
	if refDate == "" && statDate == "" && msgID == "" && len(dimensions) == 0 {
		identity["index"] = index
	}
	identityJSON, _ := json.Marshal(identity)
	hash := sha256.Sum256(identityJSON)
	rowKey := hex.EncodeToString(hash[:])
	fetchedAt := record.FetchedAt.UnixMilli()

	_, err := tx.ExecContext(ctx, `
		INSERT INTO official_api_rows (
			endpoint, row_key, row_scope, ref_date, stat_date, msgid, title,
			dimensions_json, raw_json, first_seen_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint, row_key) DO UPDATE SET
			row_scope = excluded.row_scope,
			ref_date = excluded.ref_date,
			stat_date = excluded.stat_date,
			msgid = excluded.msgid,
			title = excluded.title,
			dimensions_json = excluded.dimensions_json,
			raw_json = excluded.raw_json,
			last_seen_at = excluded.last_seen_at`,
		record.Endpoint,
		rowKey,
		scope,
		refDate,
		statDate,
		msgID,
		title,
		string(dimensionsJSON),
		string(raw),
		fetchedAt,
		fetchedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert %s response row: %w", record.Endpoint, err)
	}
	return nil
}

func stringField(item map[string]json.RawMessage, key string) string {
	raw, ok := item[key]
	if !ok {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return strings.Trim(string(raw), `"`)
}

func scalarField(item map[string]json.RawMessage, key string) (interface{}, bool) {
	raw, ok := item[key]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	var value interface{}
	if json.Unmarshal(raw, &value) != nil {
		return nil, false
	}
	switch value.(type) {
	case string, float64, bool:
		return value, true
	default:
		return nil, false
	}
}

type EndpointState struct {
	Endpoint            string `json:"endpoint"`
	Category            string `json:"category"`
	NextBackfillDate    string `json:"nextBackfillDate"`
	LastSuccessBegin    string `json:"lastSuccessBegin"`
	LastSuccessEnd      string `json:"lastSuccessEnd"`
	LastSuccessAt       int64  `json:"lastSuccessAt"`
	LastAttemptAt       int64  `json:"lastAttemptAt"`
	LastError           string `json:"lastError"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	UpdatedAt           int64  `json:"updatedAt"`
}

func (s *Store) GetState(ctx context.Context, endpoint string) (*EndpointState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT endpoint, category, next_backfill_date, last_success_begin,
			last_success_end, last_success_at, last_attempt_at, last_error,
			consecutive_failures, updated_at
		FROM official_api_state WHERE endpoint = ?`, endpoint)
	var state EndpointState
	if err := row.Scan(
		&state.Endpoint,
		&state.Category,
		&state.NextBackfillDate,
		&state.LastSuccessBegin,
		&state.LastSuccessEnd,
		&state.LastSuccessAt,
		&state.LastAttemptAt,
		&state.LastError,
		&state.ConsecutiveFailures,
		&state.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &state, nil
}

func (s *Store) MarkSuccess(ctx context.Context, endpoint, category, beginDate, endDate, nextBackfillDate string, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	nowMs := now.UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO official_api_state (
			endpoint, category, next_backfill_date, last_success_begin,
			last_success_end, last_success_at, last_attempt_at, last_error,
			consecutive_failures, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, '', 0, ?)
		ON CONFLICT(endpoint) DO UPDATE SET
			category = excluded.category,
			next_backfill_date = CASE
				WHEN excluded.next_backfill_date = '' THEN official_api_state.next_backfill_date
				ELSE excluded.next_backfill_date
			END,
			last_success_begin = excluded.last_success_begin,
			last_success_end = excluded.last_success_end,
			last_success_at = excluded.last_success_at,
			last_attempt_at = excluded.last_attempt_at,
			last_error = '',
			consecutive_failures = 0,
			updated_at = excluded.updated_at`,
		endpoint,
		category,
		nextBackfillDate,
		beginDate,
		endDate,
		nowMs,
		nowMs,
		nowMs,
	)
	return err
}

func (s *Store) MarkFailure(ctx context.Context, endpoint, category, errorMessage string, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	nowMs := now.UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO official_api_state (
			endpoint, category, last_attempt_at, last_error,
			consecutive_failures, updated_at
		) VALUES (?, ?, ?, ?, 1, ?)
		ON CONFLICT(endpoint) DO UPDATE SET
			category = excluded.category,
			last_attempt_at = excluded.last_attempt_at,
			last_error = excluded.last_error,
			consecutive_failures = official_api_state.consecutive_failures + 1,
			updated_at = excluded.updated_at`,
		endpoint,
		category,
		nowMs,
		errorMessage,
		nowMs,
	)
	return err
}

func (s *Store) ListStates(ctx context.Context) ([]EndpointState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT endpoint, category, next_backfill_date, last_success_begin,
			last_success_end, last_success_at, last_attempt_at, last_error,
			consecutive_failures, updated_at
		FROM official_api_state ORDER BY endpoint`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	states := make([]EndpointState, 0)
	for rows.Next() {
		var state EndpointState
		if err := rows.Scan(
			&state.Endpoint,
			&state.Category,
			&state.NextBackfillDate,
			&state.LastSuccessBegin,
			&state.LastSuccessEnd,
			&state.LastSuccessAt,
			&state.LastAttemptAt,
			&state.LastError,
			&state.ConsecutiveFailures,
			&state.UpdatedAt,
		); err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

type StoreStats struct {
	Fetches     int64 `json:"fetches"`
	Failed      int64 `json:"failed"`
	Rows        int64 `json:"rows"`
	ArticleRows int64 `json:"articleRows"`
}

func (s *Store) Stats(ctx context.Context) (StoreStats, error) {
	var stats StoreStats
	queries := []struct {
		query string
		value *int64
	}{
		{`SELECT COUNT(*) FROM official_api_fetches`, &stats.Fetches},
		{`SELECT COUNT(*) FROM official_api_fetches WHERE success = 0`, &stats.Failed},
		{`SELECT COUNT(*) FROM official_api_rows`, &stats.Rows},
		{`SELECT COUNT(*) FROM official_api_rows WHERE endpoint LIKE 'getarticle%'`, &stats.ArticleRows},
	}
	for _, query := range queries {
		if err := s.db.QueryRowContext(ctx, query.query).Scan(query.value); err != nil {
			return StoreStats{}, err
		}
	}
	return stats, nil
}

func (s *Store) HasTerminalFetch(ctx context.Context, endpoint string) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM official_api_fetches
		WHERE endpoint = ? AND (success = 1 OR wechat_errcode = 47009)`, endpoint).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) QueryInt64(ctx context.Context, query string, args ...interface{}) (int64, error) {
	var value int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func NumberString(value interface{}) string {
	switch typed := value.(type) {
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	default:
		return fmt.Sprint(value)
	}
}
