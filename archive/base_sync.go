package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	BaseDatasetArticles           = "articles_v1"
	BaseDatasetArticleDaily       = "article_daily_v1"
	BaseDatasetArticleCumulative  = "article_cumulative_v1"
	BaseDatasetAccountDaily       = "account_daily_v1"
	BaseDatasetFollowerSource     = "follower_source_v1"
	BaseDatasetFollowerCumulative = "follower_cumulative_v1"
	BaseDatasetMessageMetrics     = "message_metrics_v1"
	BaseDatasetInterfaceMetrics   = "interface_metrics_v1"
	BaseDatasetContentAssets      = "content_assets_v1"
	BaseDatasetContentArticles    = "content_articles_v1"
	BaseDatasetComments           = "comments_v1"
	BaseDatasetAPIFetches         = "api_fetches_v1"
	BaseDatasetSyncStatus         = "sync_status_v1"
)

type BaseSyncCandidate struct {
	Dataset       string
	RowKey        string
	SourceSeenAt  int64
	Fields        map[string]interface{}
	RecordID      string
	PayloadSHA256 string
}

var baseSyncSourceQueries = map[string]string{
	BaseDatasetArticles: `
		SELECT
			msgid AS row_key,
			last_seen_at AS source_seen_at,
			json_object(
				'唯一键', msgid,
				'消息ID', msgid,
				'消息数据ID', msg_data_id,
				'文章位置', article_index,
				'发布日期', CASE WHEN publish_date <> '' THEN CAST(strftime('%s', publish_date) AS INTEGER) * 1000 END,
				'发表类型', publish_type,
				'文章标题', title,
				'文章链接', content_url,
				'文章ID', article_id,
				'作者', author,
				'摘要', digest,
				'正文HTML', content_html,
				'原文链接', content_source_url,
				'封面素材ID', thumb_media_id,
				'封面图链接', thumb_url,
				'文章类型', article_type,
				'是否删除', is_deleted,
				'发布原始JSON', publication_raw_json,
				'正文原始JSON', content_raw_json,
				'来源更新时间', last_seen_at
			) AS payload_json
		FROM official_published_article_catalog`,
	BaseDatasetArticleDaily: `
		SELECT
			ref_date || '|' || msgid AS row_key,
			last_seen_at AS source_seen_at,
			json_object(
				'唯一键', ref_date || '|' || msgid,
				'统计日期', CAST(strftime('%s', ref_date) AS INTEGER) * 1000,
				'消息ID', msgid,
				'文章标题', title,
				'文章链接', content_url,
				'阅读人数', read_user,
				'阅读来源JSON', read_user_source_json,
				'分享人数', share_user,
				'阅读原始JSON', read_raw_json,
				'分享原始JSON', share_raw_json,
				'来源更新时间', last_seen_at
			) AS payload_json
		FROM official_article_daily_metrics`,
	BaseDatasetArticleCumulative: `
		SELECT
			msgid || '|' || stat_date AS row_key,
			last_seen_at AS source_seen_at,
			json_object(
				'唯一键', msgid || '|' || stat_date,
				'消息ID', msgid,
				'消息数据ID', msg_data_id,
				'文章位置', article_index,
				'发布日期', CASE WHEN publish_date <> '' THEN CAST(strftime('%s', publish_date) AS INTEGER) * 1000 END,
				'统计日期', CAST(strftime('%s', stat_date) AS INTEGER) * 1000,
				'文章标题', title,
				'文章链接', content_url,
				'文章ID', article_id,
				'阅读人数', read_user,
				'阅读次数', read_count,
				'原文阅读人数', original_read_user,
				'原文阅读次数', original_read_count,
				'阅读来源JSON', json_extract(raw_json, '$.read_user_source'),
				'分享人数', share_user,
				'分享次数', share_count,
				'收藏人数旧口径', favorite_user,
				'收藏次数旧口径', favorite_count,
				'爱心赞人数', zaikan_user,
				'拇指赞人数', like_user,
				'留言条数', comment_count,
				'微信收藏人数', collection_user,
				'赞赏金额分', praise_money,
				'阅读后关注人数', read_subscribe_user,
				'阅读送达率', read_delivery_rate,
				'阅读完成率', read_finish_rate,
				'平均阅读时长分钟', read_avg_activetime,
				'跳出位置JSON', json_extract(raw_json, '$.read_jump_position'),
				'原始JSON', raw_json,
				'来源更新时间', last_seen_at
			) AS payload_json
		FROM official_article_metric_facts
		WHERE endpoint = 'getarticletotaldetail' AND row_scope = 'detail_list'`,
	BaseDatasetAccountDaily: `
		SELECT
			ref_date AS row_key,
			last_seen_at AS source_seen_at,
			json_object(
				'唯一键', ref_date,
				'统计日期', CAST(strftime('%s', ref_date) AS INTEGER) * 1000,
				'阅读人数', read_user,
				'阅读来源JSON', read_user_source_json,
				'分享人数', share_user,
				'爱心赞人数', zaikan_user,
				'拇指赞人数', like_user,
				'留言条数', comment_count,
				'微信收藏人数', collection_user,
				'跳转原文人数', redirect_ori_page_user,
				'发布篇数', send_page_count,
				'原始JSON', raw_json,
				'来源更新时间', last_seen_at
			) AS payload_json
		FROM official_account_daily_metrics`,
	BaseDatasetFollowerSource: `
		SELECT
			ref_date || '|' || COALESCE(CAST(user_source AS TEXT), '') AS row_key,
			last_seen_at AS source_seen_at,
			json_object(
				'唯一键', ref_date || '|' || COALESCE(CAST(user_source AS TEXT), ''),
				'统计日期', CAST(strftime('%s', ref_date) AS INTEGER) * 1000,
				'来源代码', user_source,
				'来源名称', CASE user_source
					WHEN 0 THEN '其他合计'
					WHEN 1 THEN '公众号搜索'
					WHEN 17 THEN '名片分享'
					WHEN 30 THEN '扫描二维码'
					WHEN 57 THEN '文章内账号名称'
					WHEN 100 THEN '微信广告'
					WHEN 149 THEN '小程序关注'
					WHEN 161 THEN '他人转载'
					WHEN 200 THEN '视频号'
					WHEN 201 THEN '直播'
					ELSE '未知来源'
				END,
				'新增粉丝', new_user,
				'取关粉丝', cancel_user,
				'净增粉丝', net_new_user,
				'原始JSON', raw_json,
				'来源更新时间', last_seen_at
			) AS payload_json
		FROM official_follower_metric_facts
		WHERE endpoint = 'getusersummary'`,
	BaseDatasetFollowerCumulative: `
		SELECT
			ref_date AS row_key,
			last_seen_at AS source_seen_at,
			json_object(
				'唯一键', ref_date,
				'统计日期', CAST(strftime('%s', ref_date) AS INTEGER) * 1000,
				'累计粉丝', cumulate_user,
				'原始JSON', raw_json,
				'来源更新时间', last_seen_at
			) AS payload_json
		FROM official_follower_metric_facts
		WHERE endpoint = 'getusercumulate'`,
	BaseDatasetMessageMetrics: `
		SELECT
			endpoint || '|' || row_key AS row_key,
			last_seen_at AS source_seen_at,
			json_object(
				'唯一键', endpoint || '|' || row_key,
				'接口', endpoint,
				'统计粒度', CASE endpoint
					WHEN 'getupstreammsg' THEN '日'
					WHEN 'getupstreammsghour' THEN '小时'
					WHEN 'getupstreammsgweek' THEN '周'
					WHEN 'getupstreammsgmonth' THEN '月'
					WHEN 'getupstreammsgdist' THEN '日分布'
					WHEN 'getupstreammsgdistweek' THEN '周分布'
					WHEN 'getupstreammsgdistmonth' THEN '月分布'
					ELSE '未知'
				END,
				'统计日期', CAST(strftime('%s', ref_date) AS INTEGER) * 1000,
				'统计小时', json_extract(raw_json, '$.ref_hour'),
				'用户来源代码', json_extract(raw_json, '$.user_source'),
				'消息类型代码', json_extract(raw_json, '$.msg_type'),
				'消息类型', CASE CAST(json_extract(raw_json, '$.msg_type') AS INTEGER)
					WHEN 1 THEN '文字'
					WHEN 2 THEN '图片'
					WHEN 3 THEN '语音'
					WHEN 4 THEN '视频'
					WHEN 6 THEN '第三方应用消息'
					ELSE '未知'
				END,
				'消息数量区间', json_extract(raw_json, '$.count_interval'),
				'上行消息人数', json_extract(raw_json, '$.msg_user'),
				'上行消息条数', json_extract(raw_json, '$.msg_count'),
				'维度JSON', dimensions_json,
				'原始JSON', raw_json,
				'首次获取时间', first_seen_at,
				'来源更新时间', last_seen_at
			) AS payload_json
		FROM official_api_rows
		WHERE endpoint IN (
			'getupstreammsg', 'getupstreammsghour', 'getupstreammsgweek',
			'getupstreammsgmonth', 'getupstreammsgdist',
			'getupstreammsgdistweek', 'getupstreammsgdistmonth'
		)`,
	BaseDatasetInterfaceMetrics: `
		SELECT
			endpoint || '|' || row_key AS row_key,
			last_seen_at AS source_seen_at,
			json_object(
				'唯一键', endpoint || '|' || row_key,
				'接口', endpoint,
				'统计粒度', CASE endpoint WHEN 'getinterfacesummaryhour' THEN '小时' ELSE '日' END,
				'统计日期', CAST(strftime('%s', ref_date) AS INTEGER) * 1000,
				'统计小时', json_extract(raw_json, '$.ref_hour'),
				'回调次数', json_extract(raw_json, '$.callback_count'),
				'失败次数', json_extract(raw_json, '$.fail_count'),
				'总耗时毫秒', json_extract(raw_json, '$.total_time_cost'),
				'最大耗时毫秒', json_extract(raw_json, '$.max_time_cost'),
				'维度JSON', dimensions_json,
				'原始JSON', raw_json,
				'首次获取时间', first_seen_at,
				'来源更新时间', last_seen_at
			) AS payload_json
		FROM official_api_rows
		WHERE endpoint IN ('getinterfacesummary', 'getinterfacesummaryhour')`,
	BaseDatasetContentAssets: `
		SELECT
			source || '|' || object_id AS row_key,
			last_seen_at AS source_seen_at,
			json_object(
				'唯一键', source || '|' || object_id,
				'内容来源', source,
				'对象ID', object_id,
				'内容更新时间', CASE WHEN update_time > 0 THEN update_time * 1000 END,
				'列表原始JSON', raw_json,
				'详情类型', CASE
					WHEN detail_response IS NULL THEN '未获取'
					WHEN json_valid(CAST(detail_response AS TEXT)) THEN 'JSON'
					ELSE '二进制'
				END,
				'详情字节数', length(detail_response),
				'详情原始JSON', CASE
					WHEN json_valid(CAST(detail_response AS TEXT)) THEN CAST(detail_response AS TEXT)
				END,
				'详情获取时间', detail_fetched_at,
				'首次获取时间', first_seen_at,
				'来源更新时间', last_seen_at
			) AS payload_json
		FROM official_content_objects`,
	BaseDatasetContentArticles: `
		SELECT
			a.source || '|' || a.object_id || '|' || CAST(a.article_index AS TEXT) AS row_key,
			a.last_seen_at AS source_seen_at,
			json_object(
				'唯一键', a.source || '|' || a.object_id || '|' || CAST(a.article_index AS TEXT),
				'内容来源', a.source,
				'对象ID', a.object_id,
				'条目位置', a.article_index + 1,
				'文章类型', a.article_type,
				'消息ID', a.message_id,
				'文章标题', a.title,
				'作者', a.author,
				'摘要', a.digest,
				'正文HTML', a.content_html,
				'原文链接', a.content_source_url,
				'文章链接', a.url,
				'封面素材ID', a.thumb_media_id,
				'封面图链接', a.thumb_url,
				'是否删除', a.is_deleted,
				'原始JSON', a.raw_json,
				'首次获取时间', a.first_seen_at,
				'来源更新时间', a.last_seen_at
			) AS payload_json
		FROM official_content_articles AS a`,
	BaseDatasetComments: `
		SELECT
			row_key,
			last_seen_at AS source_seen_at,
			json_object(
				'唯一键', row_key,
				'消息数据ID', msg_data_id,
				'文章位置', article_index + 1,
				'评论ID', user_comment_id,
				'评论时间', CASE WHEN create_time > 0 THEN create_time * 1000 END,
				'评论内容', content,
				'评论类型', comment_type,
				'评论者OpenID', openid,
				'回复内容', reply_content,
				'回复时间', CASE WHEN reply_create_time > 0 THEN reply_create_time * 1000 END,
				'原始JSON', raw_json,
				'首次获取时间', first_seen_at,
				'来源更新时间', last_seen_at
			) AS payload_json
		FROM official_comments`,
	BaseDatasetAPIFetches: `
		SELECT
			'fetch:' || CAST(f.id AS TEXT) AS row_key,
			f.fetched_at AS source_seen_at,
			json_object(
				'唯一键', 'fetch:' || CAST(f.id AS TEXT),
				'调用ID', f.id,
				'接口', f.endpoint,
				'分类', f.category,
				'开始日期', f.begin_date,
				'结束日期', f.end_date,
				'请求JSON', f.request_json,
				'响应SHA256', f.response_sha256,
				'响应引用ID', f.response_ref_id,
				'HTTP状态码', f.http_status,
				'微信错误码', f.wechat_errcode,
				'微信错误信息', f.wechat_errmsg,
				'是否成功', f.success,
				'内部错误', f.error,
				'响应类型', CASE
					WHEN COALESCE(f.response_json, original.response_json) IS NULL THEN '空'
					WHEN json_valid(CAST(COALESCE(f.response_json, original.response_json) AS TEXT)) THEN 'JSON'
					ELSE '二进制'
				END,
				'响应字节数', length(COALESCE(f.response_json, original.response_json)),
				'响应原始JSON', CASE
					WHEN json_valid(CAST(COALESCE(f.response_json, original.response_json) AS TEXT))
					THEN CAST(COALESCE(f.response_json, original.response_json) AS TEXT)
				END,
				'调用时间', f.fetched_at
			) AS payload_json
		FROM official_api_fetches AS f
		LEFT JOIN official_api_fetches AS original ON original.id = f.response_ref_id`,
	BaseDatasetSyncStatus: `
		SELECT
			'api:' || endpoint AS row_key,
			updated_at AS source_seen_at,
			json_object(
				'唯一键', 'api:' || endpoint,
				'数据集', endpoint,
				'类型', '数据接口',
				'分类', category,
				'回填方向', backfill_direction,
				'下次回填位置', next_backfill_date,
				'总数', NULL,
				'是否完成', backfill_complete,
				'最后成功时间', last_success_at,
				'最后错误', last_error,
				'连续失败次数', consecutive_failures,
				'状态更新时间', updated_at
			) AS payload_json
		FROM official_api_state
		WHERE category IN ('article', 'user')
		UNION ALL
		SELECT
			'content:' || stream AS row_key,
			updated_at AS source_seen_at,
			json_object(
				'唯一键', 'content:' || stream,
				'数据集', stream,
				'类型', '内容接口',
				'分类', 'content',
				'回填方向', 'newest_to_oldest',
				'下次回填位置', CAST(next_offset AS TEXT),
				'总数', total_count,
				'是否完成', complete,
				'最后成功时间', last_success_at,
				'最后错误', last_error,
				'连续失败次数', CASE WHEN last_error = '' THEN 0 ELSE 1 END,
				'状态更新时间', updated_at
			) AS payload_json
		FROM official_content_state
		WHERE stream = 'freepublish'
		UNION ALL
		SELECT
			'base:' || dataset AS row_key,
			CASE
				WHEN MAX(source_seen_at) > MAX(last_synced_at) THEN MAX(source_seen_at)
				ELSE MAX(last_synced_at)
			END AS source_seen_at,
			json_object(
				'唯一键', 'base:' || dataset,
				'数据集', dataset,
				'类型', 'Base写回',
				'分类', 'base',
				'回填方向', '',
				'下次回填位置', '',
				'总数', COUNT(*),
				'是否完成', CASE WHEN SUM(last_error <> '') = 0 THEN 1 ELSE 0 END,
				'最后成功时间', MAX(last_synced_at),
				'最后错误', COALESCE((
					SELECT latest.last_error
					FROM official_base_records AS latest
					WHERE latest.dataset = mapped.dataset AND latest.last_error <> ''
					ORDER BY latest.last_synced_at DESC LIMIT 1
				), ''),
				'连续失败次数', SUM(last_error <> ''),
				'状态更新时间', CASE
					WHEN MAX(source_seen_at) > MAX(last_synced_at) THEN MAX(source_seen_at)
					ELSE MAX(last_synced_at)
				END
			) AS payload_json
		FROM official_base_records AS mapped
		WHERE dataset <> 'sync_status_v1'
		GROUP BY dataset`,
}

func (s *Store) ListBaseSyncCandidates(ctx context.Context, dataset string, limit int) ([]BaseSyncCandidate, error) {
	sourceQuery, ok := baseSyncSourceQueries[dataset]
	if !ok {
		return nil, fmt.Errorf("unknown Base dataset %q", dataset)
	}
	if limit <= 0 {
		limit = 200
	}
	query := `
		WITH source AS (` + sourceQuery + `)
		SELECT
			source.row_key,
			source.source_seen_at,
			source.payload_json,
			COALESCE(mapped.record_id, ''),
			COALESCE(mapped.payload_sha256, '')
		FROM source
		LEFT JOIN official_base_records AS mapped
			ON mapped.dataset = ? AND mapped.row_key = source.row_key
		WHERE mapped.row_key IS NULL
			OR source.source_seen_at > mapped.source_seen_at
			OR mapped.last_error <> ''
		ORDER BY source.source_seen_at DESC, source.row_key
		LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, dataset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]BaseSyncCandidate, 0, limit)
	for rows.Next() {
		var candidate BaseSyncCandidate
		var payload string
		candidate.Dataset = dataset
		if err := rows.Scan(
			&candidate.RowKey,
			&candidate.SourceSeenAt,
			&payload,
			&candidate.RecordID,
			&candidate.PayloadSHA256,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &candidate.Fields); err != nil {
			return nil, fmt.Errorf("decode Base payload %s/%s: %w", dataset, candidate.RowKey, err)
		}
		removeNilFields(candidate.Fields)
		result = append(result, candidate)
	}
	return result, rows.Err()
}

func removeNilFields(fields map[string]interface{}) {
	for key, value := range fields {
		if value == nil {
			delete(fields, key)
		}
	}
}

func (s *Store) BaseRecordCount(ctx context.Context, dataset string) (int64, error) {
	return s.QueryInt64(ctx, `SELECT COUNT(*) FROM official_base_records WHERE dataset = ?`, dataset)
}

func (s *Store) BaseUnresolvedRecordCount(ctx context.Context, dataset string) (int64, error) {
	return s.QueryInt64(ctx, `
		SELECT COUNT(*) FROM official_base_records
		WHERE dataset = ? AND record_id = ''`, dataset)
}

func (s *Store) SaveBaseRecord(ctx context.Context, dataset, rowKey, recordID, payloadSHA256 string, sourceSeenAt int64, syncedAt time.Time) error {
	if syncedAt.IsZero() {
		syncedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO official_base_records (
			dataset, row_key, record_id, payload_sha256, source_seen_at,
			last_synced_at, last_error
		) VALUES (?, ?, ?, ?, ?, ?, '')
		ON CONFLICT(dataset, row_key) DO UPDATE SET
			record_id = CASE WHEN excluded.record_id <> '' THEN excluded.record_id ELSE official_base_records.record_id END,
			payload_sha256 = excluded.payload_sha256,
			source_seen_at = excluded.source_seen_at,
			last_synced_at = excluded.last_synced_at,
			last_error = ''`,
		dataset, rowKey, recordID, payloadSHA256, sourceSeenAt, syncedAt.UnixMilli())
	return err
}

func (s *Store) SeedBaseRecord(ctx context.Context, dataset, rowKey, recordID string) error {
	if strings.TrimSpace(rowKey) == "" || strings.TrimSpace(recordID) == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO official_base_records (dataset, row_key, record_id)
		VALUES (?, ?, ?)
		ON CONFLICT(dataset, row_key) DO UPDATE SET record_id = excluded.record_id`,
		dataset, rowKey, recordID)
	return err
}

func (s *Store) MarkBaseRecordError(ctx context.Context, dataset, rowKey string, syncErr error) error {
	message := ""
	if syncErr != nil {
		message = syncErr.Error()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO official_base_records (dataset, row_key, last_error)
		VALUES (?, ?, ?)
		ON CONFLICT(dataset, row_key) DO UPDATE SET last_error = excluded.last_error`,
		dataset, rowKey, message)
	return err
}
