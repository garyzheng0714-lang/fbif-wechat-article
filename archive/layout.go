package archive

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	LayoutStatusBaseline  = "baseline"
	LayoutStatusPending   = "pending"
	LayoutStatusFailed    = "failed"
	LayoutStatusDelivered = "delivered"
)

type LayoutCandidate struct {
	SourceKey   string
	SourceURL   string
	Title       string
	ArticleType string
	SourceName  string
	Author      string
	ContentHTML string
	CoverURL    string
	PublishedAt int64
	Via         string
	SourceRef   string
}

type LayoutDelivery struct {
	LayoutCandidate
	Attempts int
}

type LayoutOutboxStats struct {
	InitializedAt    int64  `json:"initializedAt"`
	Baseline         int64  `json:"baseline"`
	Pending          int64  `json:"pending"`
	Failed           int64  `json:"failed"`
	Delivered        int64  `json:"delivered"`
	SkippedNewspic   int64  `json:"skippedNewspic"`
	HeldUnclassified int64  `json:"heldUnclassified"`
	OldestPendingAt  int64  `json:"oldestPendingAt"`
	OldestFailedAt   int64  `json:"oldestFailedAt"`
	LastError        string `json:"lastError,omitempty"`
}

// ListOfficialPublishedArticles 只返回微信官方 freepublish 接口已经给出完整正文的文章。
// 数据分析接口只有链接和指标，没有正文，不能冒充成可自动排版的官方正文来源。
func (s *Store) ListOfficialPublishedArticles(ctx context.Context) ([]LayoutCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.object_id, a.article_index, a.title,
			COALESCE((
				SELECT CASE
					WHEN COUNT(*) = 0 THEN ''
					WHEN COUNT(DISTINCT d.article_type) = 1 THEN MIN(d.article_type)
					ELSE ''
				END
				FROM official_content_articles AS d
				JOIN official_content_objects AS draft_object
					ON draft_object.source = d.source AND draft_object.object_id = d.object_id
				WHERE d.source = 'draft'
					AND d.article_type <> ''
					AND d.article_index = a.article_index
					AND d.title = a.title
					AND draft_object.update_time BETWEEN o.update_time - 2592000 AND o.update_time + 300
					AND (d.author = '' OR a.author = '' OR d.author = a.author)
					AND (d.content_source_url = '' OR a.content_source_url = '' OR d.content_source_url = a.content_source_url)
					AND (d.thumb_media_id = '' OR a.thumb_media_id = '' OR d.thumb_media_id = a.thumb_media_id)
					AND (d.thumb_url = '' OR a.thumb_url = '' OR d.thumb_url = a.thumb_url)
			), '') AS article_type,
			a.author, a.content_html, a.url, a.thumb_url, o.update_time
		FROM official_content_articles AS a
		JOIN official_content_objects AS o
			ON o.source = a.source AND o.object_id = a.object_id
		WHERE a.source = 'freepublish' AND a.url <> '' AND a.content_html <> '' AND a.is_deleted = 0
		ORDER BY a.first_seen_at, a.object_id, a.article_index`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]LayoutCandidate, 0)
	for rows.Next() {
		var candidate LayoutCandidate
		var objectID string
		var articleIndex int
		if err := rows.Scan(
			&objectID,
			&articleIndex,
			&candidate.Title,
			&candidate.ArticleType,
			&candidate.Author,
			&candidate.ContentHTML,
			&candidate.SourceURL,
			&candidate.CoverURL,
			&candidate.PublishedAt,
		); err != nil {
			return nil, err
		}
		candidate.Via = "freepublish"
		candidate.SourceRef = fmt.Sprintf("%s:%d", objectID, articleIndex)
		result = append(result, candidate)
	}
	return result, rows.Err()
}

// InitializeLayoutOutbox 首次启用时把库内既有文章原子标记为基线，避免把历史文章
// 一次性灌入排版待审列表。返回 initialized=false 表示早已初始化。
func (s *Store) InitializeLayoutOutbox(ctx context.Context, candidates []LayoutCandidate, now time.Time) (initialized bool, baselined int, err error) {
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var initializedAt int64
	if err := tx.QueryRowContext(ctx, `SELECT initialized_at FROM official_layout_state WHERE id = 1`).Scan(&initializedAt); err == nil {
		return false, 0, nil
	} else if err != sql.ErrNoRows {
		return false, 0, err
	}
	nowMs := now.UnixMilli()
	for _, candidate := range candidates {
		result, err := insertLayoutCandidate(ctx, tx, candidate, LayoutStatusBaseline, nowMs)
		if err != nil {
			return false, 0, err
		}
		if changed, _ := result.RowsAffected(); changed > 0 {
			baselined++
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO official_layout_state (id, initialized_at) VALUES (1, ?)`, nowMs); err != nil {
		return false, 0, err
	}
	if err := tx.Commit(); err != nil {
		return false, 0, err
	}
	return true, baselined, nil
}

func (s *Store) EnqueueLayoutCandidates(ctx context.Context, candidates []LayoutCandidate, now time.Time) (int, error) {
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	inserted := 0
	for _, candidate := range candidates {
		result, err := insertLayoutCandidate(ctx, tx, candidate, LayoutStatusPending, now.UnixMilli())
		if err != nil {
			return 0, err
		}
		if changed, _ := result.RowsAffected(); changed > 0 {
			inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

func insertLayoutCandidate(ctx context.Context, tx *sql.Tx, candidate LayoutCandidate, status string, nowMs int64) (sql.Result, error) {
	if candidate.SourceKey == "" || candidate.SourceURL == "" || candidate.ContentHTML == "" {
		return nil, fmt.Errorf("layout candidate requires source key, URL and official content")
	}
	return tx.ExecContext(ctx, `
		INSERT INTO official_layout_outbox (
			source_key, source_url, title, source_name, author, content_html,
			cover_url, published_at, discovered_via, source_ref, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_key) DO NOTHING`,
		candidate.SourceKey,
		candidate.SourceURL,
		candidate.Title,
		candidate.SourceName,
		candidate.Author,
		candidate.ContentHTML,
		candidate.CoverURL,
		candidate.PublishedAt,
		candidate.Via,
		candidate.SourceRef,
		status,
		nowMs,
		nowMs,
	)
}

func (s *Store) ListDueLayoutDeliveries(ctx context.Context, now time.Time, limit int) ([]LayoutDelivery, error) {
	if now.IsZero() {
		now = time.Now()
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT source_key, source_url, title, source_name, author, content_html,
			cover_url, published_at, discovered_via, source_ref, attempts
		FROM official_layout_outbox
		WHERE status IN (?, ?) AND next_attempt_at <= ?
		ORDER BY created_at, source_key
		LIMIT ?`, LayoutStatusPending, LayoutStatusFailed, now.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]LayoutDelivery, 0)
	for rows.Next() {
		var delivery LayoutDelivery
		if err := rows.Scan(
			&delivery.SourceKey,
			&delivery.SourceURL,
			&delivery.Title,
			&delivery.SourceName,
			&delivery.Author,
			&delivery.ContentHTML,
			&delivery.CoverURL,
			&delivery.PublishedAt,
			&delivery.Via,
			&delivery.SourceRef,
			&delivery.Attempts,
		); err != nil {
			return nil, err
		}
		result = append(result, delivery)
	}
	return result, rows.Err()
}

func (s *Store) MarkLayoutDelivered(ctx context.Context, sourceKey string, jobID int64, stage string, existing bool, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	nowMs := now.UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		UPDATE official_layout_outbox
		SET status = ?, attempts = attempts + 1, next_attempt_at = 0,
			layout_job_id = ?, layout_stage = ?, existing_job = ?, last_error = '',
			updated_at = ?, delivered_at = ?
		WHERE source_key = ?`,
		LayoutStatusDelivered, jobID, stage, boolInt(existing), nowMs, nowMs, sourceKey)
	return err
}

func (s *Store) MarkLayoutFailed(ctx context.Context, sourceKey, message string, nextAttempt, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	if nextAttempt.IsZero() {
		nextAttempt = now.Add(15 * time.Minute)
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE official_layout_outbox
		SET status = ?, attempts = attempts + 1, next_attempt_at = ?,
			last_error = ?, updated_at = ?
		WHERE source_key = ?`,
		LayoutStatusFailed, nextAttempt.UnixMilli(), message, now.UnixMilli(), sourceKey)
	return err
}

func (s *Store) LayoutStats(ctx context.Context) (LayoutOutboxStats, error) {
	var stats LayoutOutboxStats
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT initialized_at FROM official_layout_state WHERE id = 1), 0)`).Scan(&stats.InitializedAt); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(status = ?), 0),
			COALESCE(SUM(status = ?), 0),
			COALESCE(SUM(status = ?), 0),
			COALESCE(SUM(status = ?), 0)
		FROM official_layout_outbox`,
		LayoutStatusBaseline,
		LayoutStatusPending,
		LayoutStatusFailed,
		LayoutStatusDelivered,
	).Scan(&stats.Baseline, &stats.Pending, &stats.Failed, &stats.Delivered); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(MIN(CASE WHEN status = ? THEN created_at END), 0),
			COALESCE(MIN(CASE WHEN status = ? THEN updated_at END), 0)
		FROM official_layout_outbox`, LayoutStatusPending, LayoutStatusFailed).
		Scan(&stats.OldestPendingAt, &stats.OldestFailedAt); err != nil {
		return stats, err
	}
	_ = s.db.QueryRowContext(ctx, `
		SELECT last_error FROM official_layout_outbox
		WHERE status = ? AND last_error <> ''
		ORDER BY updated_at DESC LIMIT 1`, LayoutStatusFailed).Scan(&stats.LastError)
	if stats.InitializedAt > 0 {
		if err := s.db.QueryRowContext(ctx, `
			WITH classified AS (
				SELECT o.update_time,
					COALESCE((
						SELECT CASE
							WHEN COUNT(*) = 0 THEN ''
							WHEN COUNT(DISTINCT d.article_type) = 1 THEN MIN(d.article_type)
							ELSE ''
						END
						FROM official_content_articles AS d
						JOIN official_content_objects AS draft_object
							ON draft_object.source = d.source AND draft_object.object_id = d.object_id
						WHERE d.source = 'draft'
							AND d.article_type <> ''
							AND d.article_index = a.article_index
							AND d.title = a.title
							AND draft_object.update_time BETWEEN o.update_time - 2592000 AND o.update_time + 300
							AND (d.author = '' OR a.author = '' OR d.author = a.author)
							AND (d.content_source_url = '' OR a.content_source_url = '' OR d.content_source_url = a.content_source_url)
							AND (d.thumb_media_id = '' OR a.thumb_media_id = '' OR d.thumb_media_id = a.thumb_media_id)
							AND (d.thumb_url = '' OR a.thumb_url = '' OR d.thumb_url = a.thumb_url)
					), '') AS article_type
				FROM official_content_articles AS a
				JOIN official_content_objects AS o
					ON o.source = a.source AND o.object_id = a.object_id
				WHERE a.source = 'freepublish'
					AND a.url <> '' AND a.content_html <> '' AND a.is_deleted = 0
			)
			SELECT
				COALESCE(SUM(article_type = 'newspic'), 0),
				COALESCE(SUM(article_type NOT IN ('news', 'newspic')), 0)
			FROM classified
			WHERE update_time >= ?`, stats.InitializedAt/1000).Scan(
			&stats.SkippedNewspic,
			&stats.HeldUnclassified,
		); err != nil {
			return stats, err
		}
	}
	return stats, nil
}
