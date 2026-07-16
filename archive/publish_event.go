package archive

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	PublishEventArticleIgnored   = "ignored"
	PublishEventArticlePending   = "pending"
	PublishEventArticleFailed    = "failed"
	PublishEventArticleDelivered = "delivered"
	PublishEventArticleHeld      = "held_unclassified"
	PublishEventArticleNewspic   = "skipped_newspic"
)

type PublishEvent struct {
	EventKey            string
	EventType           string
	MsgID               string
	ToUserName          string
	FromUserName        string
	CreateTime          int64
	MsgType             string
	Status              string
	TotalCount          int64
	FilterCount         int64
	SentCount           int64
	ErrorCount          int64
	CopyrightCount      int64
	CopyrightCheckState int64
	RawXML              []byte // exact HTTP request body (encrypted wrapper in safe mode)
	EventXML            []byte // decrypted MASSSENDJOBFINISH XML
	Articles            []PublishEventArticle
	CopyrightArticles   []PublishEventCopyrightArticle
}

type PublishEventArticle struct {
	ArticleIndex int
	SourceKey    string
	SourceURL    string
	Eligible     bool
}

type PublishEventDelivery struct {
	EventKey     string
	ArticleIndex int
	SourceKey    string
	SourceURL    string
	Attempts     int
}

type PublishEventCopyrightArticle struct {
	ArticleIndex          int
	UserDeclareState      int64
	AuditState            int64
	OriginalArticleURL    string
	OriginalArticleType   int64
	CanReprint            int64
	NeedReplaceContent    int64
	NeedShowReprintSource int64
}

type PublishEventPayload struct {
	SHA256       string
	RawXML       []byte
	EventXML     []byte
	FirstSeenAt  int64
	LastSeenAt   int64
	ReceiveCount int64
}

type PublishEventStats struct {
	Events    int64 `json:"events"`
	Ignored   int64 `json:"ignored"`
	Pending   int64 `json:"pending"`
	Failed    int64 `json:"failed"`
	Delivered int64 `json:"delivered"`
	Held      int64 `json:"heldUnclassified"`
	Newspic   int64 `json:"skippedNewspic"`
}

func (s *Store) PublishEventPayload(ctx context.Context, eventKey string) (rawXML, eventXML []byte, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT raw_xml, event_xml
		FROM official_publish_events
		WHERE event_key = ?`, eventKey).Scan(&rawXML, &eventXML)
	return rawXML, eventXML, err
}

func (s *Store) PublishEventPayloadHistory(ctx context.Context, eventKey string) ([]PublishEventPayload, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT payload_sha256, raw_xml, event_xml, first_seen_at, last_seen_at, receive_count
		FROM official_publish_event_payloads
		WHERE event_key = ?
		ORDER BY first_seen_at, payload_sha256`, eventKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PublishEventPayload, 0)
	for rows.Next() {
		var payload PublishEventPayload
		if err := rows.Scan(
			&payload.SHA256,
			&payload.RawXML,
			&payload.EventXML,
			&payload.FirstSeenAt,
			&payload.LastSeenAt,
			&payload.ReceiveCount,
		); err != nil {
			return nil, err
		}
		result = append(result, payload)
	}
	return result, rows.Err()
}

func (s *Store) PublishEventCopyrightArticles(ctx context.Context, eventKey string) ([]PublishEventCopyrightArticle, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT article_index, user_declare_state, audit_state, original_article_url,
			original_article_type, can_reprint, need_replace_content, need_show_reprint_source
		FROM official_publish_event_copyright_articles
		WHERE event_key = ?
		ORDER BY article_index`, eventKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PublishEventCopyrightArticle, 0)
	for rows.Next() {
		var article PublishEventCopyrightArticle
		if err := rows.Scan(
			&article.ArticleIndex,
			&article.UserDeclareState,
			&article.AuditState,
			&article.OriginalArticleURL,
			&article.OriginalArticleType,
			&article.CanReprint,
			&article.NeedReplaceContent,
			&article.NeedShowReprintSource,
		); err != nil {
			return nil, err
		}
		result = append(result, article)
	}
	return result, rows.Err()
}

// SavePublishEvent 原样保存微信回调 XML，并以 event_key/article_index 幂等写入
// URL outbox。只有明确 send success 的文章 URL 才由调用方标记 Eligible。
func (s *Store) SavePublishEvent(ctx context.Context, event PublishEvent, now time.Time) error {
	if event.EventKey == "" || event.EventType == "" || event.MsgID == "" || len(event.RawXML) == 0 || len(event.EventXML) == 0 {
		return fmt.Errorf("publish event requires event key, type, msg id, raw XML and event XML")
	}
	if now.IsZero() {
		now = time.Now()
	}
	nowMs := now.UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO official_publish_events (
			event_key, event_type, msg_id, to_user_name, from_user_name,
			create_time, msg_type, status, total_count, filter_count,
			sent_count, error_count, copyright_count, copyright_check_state,
			raw_xml, event_xml, first_seen_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(event_key) DO UPDATE SET
			event_type = excluded.event_type,
			msg_id = excluded.msg_id,
			to_user_name = excluded.to_user_name,
			from_user_name = excluded.from_user_name,
			create_time = excluded.create_time,
			msg_type = excluded.msg_type,
			status = excluded.status,
			total_count = excluded.total_count,
			filter_count = excluded.filter_count,
			sent_count = excluded.sent_count,
			error_count = excluded.error_count,
			copyright_count = excluded.copyright_count,
			copyright_check_state = excluded.copyright_check_state,
			raw_xml = excluded.raw_xml,
			event_xml = excluded.event_xml,
			last_seen_at = excluded.last_seen_at`,
		event.EventKey,
		event.EventType,
		event.MsgID,
		event.ToUserName,
		event.FromUserName,
		event.CreateTime,
		event.MsgType,
		event.Status,
		event.TotalCount,
		event.FilterCount,
		event.SentCount,
		event.ErrorCount,
		event.CopyrightCount,
		event.CopyrightCheckState,
		event.RawXML,
		event.EventXML,
		nowMs,
		nowMs,
	); err != nil {
		return err
	}
	payloadDigest := sha256.Sum256(event.RawXML)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO official_publish_event_payloads (
			event_key, payload_sha256, raw_xml, event_xml,
			first_seen_at, last_seen_at, receive_count
		) VALUES (?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT(event_key, payload_sha256) DO UPDATE SET
			last_seen_at = excluded.last_seen_at,
			receive_count = official_publish_event_payloads.receive_count + 1`,
		event.EventKey,
		hex.EncodeToString(payloadDigest[:]),
		event.RawXML,
		event.EventXML,
		nowMs,
		nowMs,
	); err != nil {
		return err
	}
	for _, article := range event.CopyrightArticles {
		if article.ArticleIndex <= 0 {
			return fmt.Errorf("publish event copyright article requires positive index")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO official_publish_event_copyright_articles (
				event_key, article_index, user_declare_state, audit_state,
				original_article_url, original_article_type, can_reprint,
				need_replace_content, need_show_reprint_source, first_seen_at, last_seen_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(event_key, article_index) DO UPDATE SET
				user_declare_state = excluded.user_declare_state,
				audit_state = excluded.audit_state,
				original_article_url = excluded.original_article_url,
				original_article_type = excluded.original_article_type,
				can_reprint = excluded.can_reprint,
				need_replace_content = excluded.need_replace_content,
				need_show_reprint_source = excluded.need_show_reprint_source,
				last_seen_at = excluded.last_seen_at`,
			event.EventKey,
			article.ArticleIndex,
			article.UserDeclareState,
			article.AuditState,
			article.OriginalArticleURL,
			article.OriginalArticleType,
			article.CanReprint,
			article.NeedReplaceContent,
			article.NeedShowReprintSource,
			nowMs,
			nowMs,
		); err != nil {
			return err
		}
	}
	for _, article := range event.Articles {
		if article.ArticleIndex <= 0 || article.SourceKey == "" || article.SourceURL == "" {
			return fmt.Errorf("publish event article requires positive index, source key and URL")
		}
		status := PublishEventArticleIgnored
		if article.Eligible {
			status = PublishEventArticlePending
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO official_publish_event_articles (
				event_key, article_index, source_key, source_url, status,
				created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			event.EventKey,
			article.ArticleIndex,
			article.SourceKey,
			article.SourceURL,
			status,
			nowMs,
			nowMs,
		); err != nil {
			return err
		}
		// A failure callback can precede a later successful callback with the same
		// MsgID. Only that ignored -> pending transition is allowed; a duplicate
		// callback never resets delivered/failed work or its attempt history.
		if status == PublishEventArticlePending {
			if _, err := tx.ExecContext(ctx, `
				UPDATE official_publish_event_articles
				SET status = ?, next_attempt_at = 0, updated_at = ?
				WHERE event_key = ? AND article_index = ? AND source_key = ? AND status = ?`,
				PublishEventArticlePending,
				nowMs,
				event.EventKey,
				article.ArticleIndex,
				article.SourceKey,
				PublishEventArticleIgnored,
			); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) ListDuePublishEventDeliveries(ctx context.Context, now time.Time, limit int) ([]PublishEventDelivery, error) {
	if now.IsZero() {
		now = time.Now()
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT event_key, article_index, source_key, source_url, attempts
		FROM official_publish_event_articles
		WHERE status IN (?, ?) AND next_attempt_at <= ?
		ORDER BY created_at, event_key, article_index
		LIMIT ?`,
		PublishEventArticlePending,
		PublishEventArticleFailed,
		now.UnixMilli(),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PublishEventDelivery, 0)
	for rows.Next() {
		var delivery PublishEventDelivery
		if err := rows.Scan(
			&delivery.EventKey,
			&delivery.ArticleIndex,
			&delivery.SourceKey,
			&delivery.SourceURL,
			&delivery.Attempts,
		); err != nil {
			return nil, err
		}
		result = append(result, delivery)
	}
	return result, rows.Err()
}

func (s *Store) MarkPublishEventDelivered(ctx context.Context, eventKey string, articleIndex int, jobID int64, stage string, existing bool, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	nowMs := now.UnixMilli()
	result, err := s.db.ExecContext(ctx, `
		UPDATE official_publish_event_articles
		SET status = ?, attempts = attempts + 1, next_attempt_at = 0,
			layout_job_id = ?, layout_stage = ?, existing_job = ?, last_error = '',
			updated_at = ?, delivered_at = ?
		WHERE event_key = ? AND article_index = ?`,
		PublishEventArticleDelivered,
		jobID,
		stage,
		boolInt(existing),
		nowMs,
		nowMs,
		eventKey,
		articleIndex,
	)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) MarkPublishEventFailed(ctx context.Context, eventKey string, articleIndex int, message string, nextAttempt, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	if nextAttempt.IsZero() {
		nextAttempt = now.Add(15 * time.Minute)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE official_publish_event_articles
		SET status = ?, attempts = attempts + 1, next_attempt_at = ?,
			last_error = ?, updated_at = ?
		WHERE event_key = ? AND article_index = ?`,
		PublishEventArticleFailed,
		nextAttempt.UnixMilli(),
		message,
		now.UnixMilli(),
		eventKey,
		articleIndex,
	)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) MarkPublishEventClassified(ctx context.Context, eventKey string, articleIndex int, status, reason string, now time.Time) error {
	if status != PublishEventArticleHeld && status != PublishEventArticleNewspic {
		return fmt.Errorf("unsupported publish event classification status %q", status)
	}
	if now.IsZero() {
		now = time.Now()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE official_publish_event_articles
		SET status = ?, next_attempt_at = 0, last_error = ?, updated_at = ?
		WHERE event_key = ? AND article_index = ?`,
		status,
		reason,
		now.UnixMilli(),
		eventKey,
		articleIndex,
	)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) PublishEventStats(ctx context.Context) (PublishEventStats, error) {
	var stats PublishEventStats
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM official_publish_events`).Scan(&stats.Events); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(status = ?), 0),
			COALESCE(SUM(status = ?), 0),
			COALESCE(SUM(status = ?), 0),
			COALESCE(SUM(status = ?), 0),
			COALESCE(SUM(status = ?), 0),
			COALESCE(SUM(status = ?), 0)
		FROM official_publish_event_articles`,
		PublishEventArticleIgnored,
		PublishEventArticlePending,
		PublishEventArticleFailed,
		PublishEventArticleDelivered,
		PublishEventArticleHeld,
		PublishEventArticleNewspic,
	).Scan(&stats.Ignored, &stats.Pending, &stats.Failed, &stats.Delivered, &stats.Held, &stats.Newspic); err != nil {
		return stats, err
	}
	return stats, nil
}
