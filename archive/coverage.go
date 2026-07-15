package archive

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

const HistoricalCoverageContractVersion = "2026-07-15-v1"

type HistoricalCoverageRequirement struct {
	Endpoint     string `json:"endpoint"`
	EarliestDate string `json:"earliestDate"`
}

type HistoricalEndpointCoverage struct {
	Endpoint            string `json:"endpoint"`
	EarliestDate        string `json:"earliestDate"`
	NextBackfillDate    string `json:"nextBackfillDate"`
	BackfillComplete    bool   `json:"backfillComplete"`
	LastSuccessBegin    string `json:"lastSuccessBegin"`
	LastSuccessEnd      string `json:"lastSuccessEnd"`
	LastSuccessAt       int64  `json:"lastSuccessAt"`
	LastError           string `json:"lastError,omitempty"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
}

type HistoricalCoverageCounts struct {
	KnownArticleIdentities          int64 `json:"knownArticleIdentities"`
	MetricArticleIdentities         int64 `json:"metricArticleIdentities"`
	PublicationArticleIdentities    int64 `json:"publicationArticleIdentities"`
	FreePublishObjects              int64 `json:"freePublishObjects"`
	FreePublishArticleItems         int64 `json:"freePublishArticleItems"`
	FreePublishItemsWithMessageID   int64 `json:"freePublishItemsWithMessageId"`
	ContentAndMetricIdentities      int64 `json:"contentAndMetricIdentities"`
	ContentOnlyIdentities           int64 `json:"contentOnlyIdentities"`
	MetricOnlyIdentities            int64 `json:"metricOnlyIdentities"`
	CompleteContentArticles         int64 `json:"completeContentArticles"`
	PartialMetadataArticles         int64 `json:"partialMetadataArticles"`
	IdentityOnlyArticles            int64 `json:"identityOnlyArticles"`
	InvalidMessageIDs               int64 `json:"invalidMessageIds"`
	DuplicateContentMessageIDs      int64 `json:"duplicateContentMessageIds"`
	FreePublishObjectsMissingDetail int64 `json:"freePublishObjectsMissingDetail"`
	ArchivedFetches                 int64 `json:"archivedFetches"`
	ArchivedSuccessfulResponses     int64 `json:"archivedSuccessfulResponses"`
	ArchivedEndpointCount           int64 `json:"archivedEndpointCount"`
}

type HistoricalCoverageDates struct {
	KnownPublishDateMin      string `json:"knownPublishDateMin,omitempty"`
	KnownPublishDateMax      string `json:"knownPublishDateMax,omitempty"`
	MetricObservationDateMin string `json:"metricObservationDateMin,omitempty"`
	MetricObservationDateMax string `json:"metricObservationDateMax,omitempty"`
	FreePublishDateMin       string `json:"freePublishDateMin,omitempty"`
	FreePublishDateMax       string `json:"freePublishDateMax,omitempty"`
}

type HistoricalCoverageApproval struct {
	Present         bool   `json:"present"`
	ContractVersion string `json:"contractVersion,omitempty"`
	ApprovedBy      string `json:"approvedBy,omitempty"`
	ApprovedAt      int64  `json:"approvedAt,omitempty"`
	MatchesContract bool   `json:"matchesContract"`
}

type HistoricalCoverageIssue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type HistoricalCoverageReport struct {
	ContractVersion                    string                       `json:"contractVersion"`
	GeneratedAt                        int64                        `json:"generatedAt"`
	Status                             string                       `json:"status"`
	Semantics                          string                       `json:"semantics"`
	Verified                           bool                         `json:"verified"`
	EligibleForUserApproval            bool                         `json:"eligibleForUserApproval"`
	BaseSyncAllowed                    bool                         `json:"baseSyncAllowed"`
	RequiredEndpointCount              int                          `json:"requiredEndpointCount"`
	CompletedRequiredEndpointCount     int                          `json:"completedRequiredEndpointCount"`
	Endpoints                          []HistoricalEndpointCoverage `json:"endpoints"`
	FreePublishObjectNextOffset        int                          `json:"freePublishObjectNextOffset"`
	FreePublishObjectTotalCount        int                          `json:"freePublishObjectTotalCount"`
	FreePublishObjectInventoryComplete bool                         `json:"freePublishObjectInventoryComplete"`
	Counts                             HistoricalCoverageCounts     `json:"counts"`
	Dates                              HistoricalCoverageDates      `json:"dates"`
	Approval                           HistoricalCoverageApproval   `json:"approval"`
	Issues                             []HistoricalCoverageIssue    `json:"issues"`
}

func (s *Store) AuditHistoricalCoverage(ctx context.Context, requirements []HistoricalCoverageRequirement, now time.Time) (*HistoricalCoverageReport, error) {
	if now.IsZero() {
		now = time.Now()
	}
	report := &HistoricalCoverageReport{
		ContractVersion: HistoricalCoverageContractVersion,
		GeneratedAt:     now.UnixMilli(),
		Status:          "collecting",
		Semantics:       "knownArticleIdentities 是多个官方接口和发布正文表关联后的已知稳定文章身份数；在 verified=true 前绝不表示公众号历史已发布文章总量",
		Issues:          make([]HistoricalCoverageIssue, 0),
	}

	if err := s.readHistoricalCoverageCounts(ctx, &report.Counts); err != nil {
		return nil, err
	}
	if err := s.readHistoricalCoverageDates(ctx, &report.Dates); err != nil {
		return nil, err
	}
	if err := s.readHistoricalEndpointCoverage(ctx, requirements, report); err != nil {
		return nil, err
	}
	if err := s.readHistoricalCoverageApproval(ctx, &report.Approval); err != nil {
		return nil, err
	}

	blocking := false
	addIssue := func(code, severity, message string) {
		report.Issues = append(report.Issues, HistoricalCoverageIssue{Code: code, Severity: severity, Message: message})
		if severity == "block" {
			blocking = true
		}
	}
	if report.CompletedRequiredEndpointCount != report.RequiredEndpointCount {
		addIssue("required_backfills_incomplete", "block", fmt.Sprintf("仍有 %d/%d 个现役官方接口未完成从新到旧回填", report.RequiredEndpointCount-report.CompletedRequiredEndpointCount, report.RequiredEndpointCount))
	}
	if !report.FreePublishObjectInventoryComplete {
		addIssue("freepublish_object_inventory_incomplete", "block", "freepublish 发布对象采集流尚未分页到接口当前终点")
	}
	if report.Counts.FreePublishObjectsMissingDetail > 0 {
		addIssue("freepublish_details_incomplete", "block", fmt.Sprintf("仍有 %d 个 freepublish 发布对象尚未保存详情接口原始响应", report.Counts.FreePublishObjectsMissingDetail))
	}
	if report.Counts.FreePublishItemsWithMessageID != report.Counts.FreePublishArticleItems {
		addIssue("freepublish_message_ids_incomplete", "block", fmt.Sprintf("freepublish 文章条目中有 %d 条尚未解析出稳定 msgid", report.Counts.FreePublishArticleItems-report.Counts.FreePublishItemsWithMessageID))
	}
	if report.Counts.InvalidMessageIDs > 0 {
		addIssue("invalid_message_ids", "block", fmt.Sprintf("发现 %d 个不符合 msg_data_id_index 结构的文章身份", report.Counts.InvalidMessageIDs))
	}
	if report.Counts.DuplicateContentMessageIDs > 0 {
		addIssue("duplicate_content_message_ids", "block", fmt.Sprintf("发现 %d 个映射到多条发布正文的重复 msgid，需要先裁决去重", report.Counts.DuplicateContentMessageIDs))
	}
	if report.Counts.KnownArticleIdentities == 0 {
		addIssue("no_article_identities", "block", "尚未形成任何可关联的官方文章稳定身份")
	}
	if report.Counts.IdentityOnlyArticles > 0 {
		addIssue("identity_only_articles", "warning", fmt.Sprintf("有 %d 个文章身份仅由阅读/分享接口发现；官方新接口不返回这些旧文章的标题、链接和正文", report.Counts.IdentityOnlyArticles))
	}
	if report.Counts.ContentOnlyIdentities > 0 {
		addIssue("content_only_articles", "warning", fmt.Sprintf("有 %d 个 freepublish 正文身份尚未在文章指标接口中出现", report.Counts.ContentOnlyIdentities))
	}
	if report.Approval.Present && !report.Approval.MatchesContract {
		addIssue("approval_contract_mismatch", "block", "已有确认属于旧版覆盖合同，必须按当前合同重新确认")
	}

	report.EligibleForUserApproval = !blocking
	report.BaseSyncAllowed = report.EligibleForUserApproval && report.Approval.MatchesContract
	report.Verified = report.BaseSyncAllowed
	switch {
	case report.Verified:
		report.Status = "verified"
	case report.Approval.Present && !report.EligibleForUserApproval:
		report.Status = "regressed"
	case report.EligibleForUserApproval:
		report.Status = "awaiting_user_approval"
	default:
		report.Status = "collecting"
	}
	return report, nil
}

func (s *Store) readHistoricalCoverageCounts(ctx context.Context, counts *HistoricalCoverageCounts) error {
	return s.db.QueryRowContext(ctx, `
		WITH catalog AS MATERIALIZED (
			SELECT * FROM official_known_article_catalog
		)
		SELECT
			COUNT(*),
			COALESCE(SUM(has_read_metrics = 1 OR has_share_metrics = 1 OR has_publication_record = 1), 0),
			COALESCE(SUM(has_publication_record = 1), 0),
			(SELECT COUNT(*) FROM official_content_objects WHERE source = 'freepublish'),
			(SELECT COUNT(*) FROM official_content_articles WHERE source = 'freepublish'),
			(SELECT COUNT(*) FROM official_content_articles WHERE source = 'freepublish' AND message_id <> ''),
			COALESCE(SUM(has_content_record = 1 AND (has_read_metrics = 1 OR has_share_metrics = 1 OR has_publication_record = 1)), 0),
			COALESCE(SUM(has_content_record = 1 AND has_read_metrics = 0 AND has_share_metrics = 0 AND has_publication_record = 0), 0),
			COALESCE(SUM(has_content_record = 0), 0),
			COALESCE(SUM(metadata_status = 'content_complete'), 0),
			COALESCE(SUM(metadata_status = 'metadata_partial'), 0),
			COALESCE(SUM(metadata_status = 'identity_only'), 0),
			COALESCE(SUM(
				instr(msgid, '_') <= 1 OR
				CAST(substr(msgid, 1, instr(msgid, '_') - 1) AS INTEGER) <= 0 OR
				CAST(substr(msgid, instr(msgid, '_') + 1) AS INTEGER) <= 0
			), 0),
			(SELECT COUNT(*) FROM (
				SELECT message_id FROM official_content_articles
				WHERE source = 'freepublish' AND message_id <> ''
				GROUP BY message_id HAVING COUNT(*) > 1
			)),
			(SELECT COUNT(*) FROM official_content_objects WHERE source = 'freepublish' AND detail_fetched_at = 0),
			(SELECT COUNT(*) FROM official_api_fetches),
			(SELECT COUNT(*) FROM official_api_fetches WHERE success = 1 AND response_sha256 <> ''),
			(SELECT COUNT(DISTINCT endpoint) FROM official_api_fetches)
		FROM catalog
	`).Scan(
		&counts.KnownArticleIdentities,
		&counts.MetricArticleIdentities,
		&counts.PublicationArticleIdentities,
		&counts.FreePublishObjects,
		&counts.FreePublishArticleItems,
		&counts.FreePublishItemsWithMessageID,
		&counts.ContentAndMetricIdentities,
		&counts.ContentOnlyIdentities,
		&counts.MetricOnlyIdentities,
		&counts.CompleteContentArticles,
		&counts.PartialMetadataArticles,
		&counts.IdentityOnlyArticles,
		&counts.InvalidMessageIDs,
		&counts.DuplicateContentMessageIDs,
		&counts.FreePublishObjectsMissingDetail,
		&counts.ArchivedFetches,
		&counts.ArchivedSuccessfulResponses,
		&counts.ArchivedEndpointCount,
	)
}

func (s *Store) readHistoricalCoverageDates(ctx context.Context, dates *HistoricalCoverageDates) error {
	var knownMin, knownMax, metricMin, metricMax, freeMin, freeMax sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT MIN(NULLIF(publish_date, '')) FROM official_known_article_catalog),
			(SELECT MAX(NULLIF(publish_date, '')) FROM official_known_article_catalog),
			(SELECT MIN(NULLIF(first_metric_date, '')) FROM official_known_article_catalog),
			(SELECT MAX(NULLIF(last_metric_date, '')) FROM official_known_article_catalog),
			(SELECT MIN(date(update_time, 'unixepoch', 'localtime')) FROM official_content_objects WHERE source = 'freepublish'),
			(SELECT MAX(date(update_time, 'unixepoch', 'localtime')) FROM official_content_objects WHERE source = 'freepublish')
	`).Scan(&knownMin, &knownMax, &metricMin, &metricMax, &freeMin, &freeMax)
	if err != nil {
		return err
	}
	dates.KnownPublishDateMin = nullString(knownMin)
	dates.KnownPublishDateMax = nullString(knownMax)
	dates.MetricObservationDateMin = nullString(metricMin)
	dates.MetricObservationDateMax = nullString(metricMax)
	dates.FreePublishDateMin = nullString(freeMin)
	dates.FreePublishDateMax = nullString(freeMax)
	return nil
}

func (s *Store) readHistoricalEndpointCoverage(ctx context.Context, requirements []HistoricalCoverageRequirement, report *HistoricalCoverageReport) error {
	states, err := s.ListStates(ctx)
	if err != nil {
		return err
	}
	byEndpoint := make(map[string]EndpointState, len(states))
	for _, state := range states {
		byEndpoint[state.Endpoint] = state
	}
	unique := make(map[string]HistoricalCoverageRequirement, len(requirements))
	for _, requirement := range requirements {
		requirement.Endpoint = strings.TrimSpace(requirement.Endpoint)
		if requirement.Endpoint != "" {
			unique[requirement.Endpoint] = requirement
		}
	}
	names := make([]string, 0, len(unique))
	for endpoint := range unique {
		names = append(names, endpoint)
	}
	sort.Strings(names)
	report.RequiredEndpointCount = len(names)
	report.Endpoints = make([]HistoricalEndpointCoverage, 0, len(names))
	for _, endpoint := range names {
		requirement := unique[endpoint]
		state, found := byEndpoint[endpoint]
		item := HistoricalEndpointCoverage{Endpoint: endpoint, EarliestDate: requirement.EarliestDate}
		if found {
			item.NextBackfillDate = state.NextBackfillDate
			item.BackfillComplete = state.BackfillComplete && state.LastError == "" && state.LastSuccessAt > 0
			item.LastSuccessBegin = state.LastSuccessBegin
			item.LastSuccessEnd = state.LastSuccessEnd
			item.LastSuccessAt = state.LastSuccessAt
			item.LastError = state.LastError
			item.ConsecutiveFailures = state.ConsecutiveFailures
		}
		if item.BackfillComplete {
			report.CompletedRequiredEndpointCount++
		}
		report.Endpoints = append(report.Endpoints, item)
	}

	contentState, err := s.GetContentState(ctx, "freepublish")
	if err != nil {
		return err
	}
	if contentState != nil {
		report.FreePublishObjectNextOffset = contentState.NextObjectOffset
		report.FreePublishObjectTotalCount = contentState.ObjectTotalCount
		report.FreePublishObjectInventoryComplete = contentState.ObjectInventoryComplete && contentState.LastError == ""
	}
	return nil
}

func (s *Store) readHistoricalCoverageApproval(ctx context.Context, approval *HistoricalCoverageApproval) error {
	err := s.db.QueryRowContext(ctx, `
		SELECT contract_version, approved_by, approved_at
		FROM official_history_coverage_approval WHERE id = 1`).Scan(
		&approval.ContractVersion,
		&approval.ApprovedBy,
		&approval.ApprovedAt,
	)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	approval.Present = true
	approval.MatchesContract = approval.ContractVersion == HistoricalCoverageContractVersion
	return nil
}

func (s *Store) SaveHistoricalCoverageApproval(ctx context.Context, approvedBy string, now time.Time) error {
	approvedBy = strings.TrimSpace(approvedBy)
	if approvedBy == "" {
		return fmt.Errorf("coverage approval requires approved_by")
	}
	if now.IsZero() {
		now = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO official_history_coverage_approval (id, contract_version, approved_by, approved_at)
		VALUES (1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			contract_version = excluded.contract_version,
			approved_by = excluded.approved_by,
			approved_at = excluded.approved_at`,
		HistoricalCoverageContractVersion, approvedBy, now.UnixMilli())
	return err
}

func (s *Store) RevokeHistoricalCoverageApproval(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM official_history_coverage_approval WHERE id = 1`)
	return err
}

func nullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
