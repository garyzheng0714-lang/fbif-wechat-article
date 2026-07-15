package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

type ContentAPI interface {
	Call(ctx context.Context, endpoint wechat.ContentEndpoint, payload interface{}) (*wechat.RawAPIResponse, error)
}

type ContentCollector struct {
	Client   ContentAPI
	Store    *archive.Store
	Now      func() time.Time
	MaxCalls int

	runMu sync.Mutex
}

type ContentRunResult struct {
	StartedAt  int64             `json:"startedAt"`
	FinishedAt int64             `json:"finishedAt"`
	Calls      int               `json:"calls"`
	Succeeded  int               `json:"succeeded"`
	Failed     int               `json:"failed"`
	Errors     map[string]string `json:"errors,omitempty"`
}

type contentStream struct {
	Name           string
	Source         string
	ListEndpoint   string
	DetailEndpoint string
	Payload        func(offset int) map[string]interface{}
}

var contentStreams = []contentStream{
	{Name: "draft", Source: "draft", ListEndpoint: "draft_batchget", DetailEndpoint: "draft_get", Payload: func(offset int) map[string]interface{} {
		return map[string]interface{}{"offset": offset, "count": 20, "no_content": 0}
	}},
	{Name: "freepublish", Source: "freepublish", ListEndpoint: "freepublish_batchget", DetailEndpoint: "freepublish_getarticle", Payload: func(offset int) map[string]interface{} {
		return map[string]interface{}{"offset": offset, "count": 20, "no_return_content": false}
	}},
	{Name: "material_news", Source: "material_news", ListEndpoint: "material_batchget_material", DetailEndpoint: "material_get_material", Payload: func(offset int) map[string]interface{} {
		return map[string]interface{}{"type": "news", "offset": offset, "count": 20}
	}},
	{Name: "material_image", Source: "material_image", ListEndpoint: "material_batchget_material", Payload: func(offset int) map[string]interface{} {
		return map[string]interface{}{"type": "image", "offset": offset, "count": 20}
	}},
	{Name: "material_voice", Source: "material_voice", ListEndpoint: "material_batchget_material", Payload: func(offset int) map[string]interface{} {
		return map[string]interface{}{"type": "voice", "offset": offset, "count": 20}
	}},
	{Name: "material_video", Source: "material_video", ListEndpoint: "material_batchget_material", Payload: func(offset int) map[string]interface{} {
		return map[string]interface{}{"type": "video", "offset": offset, "count": 20}
	}},
}

const publishedHistoryPagesPerRun = 20

func contentStreamByName(name string) (contentStream, bool) {
	for _, stream := range contentStreams {
		if stream.Name == name {
			return stream, true
		}
	}
	return contentStream{}, false
}

func (c *ContentCollector) Run(ctx context.Context) (*ContentRunResult, error) {
	if c.Client == nil || c.Store == nil {
		return nil, fmt.Errorf("content collector is not configured")
	}
	if !c.runMu.TryLock() {
		return nil, fmt.Errorf("content collector is already running")
	}
	defer c.runMu.Unlock()

	result := &ContentRunResult{StartedAt: c.now().UnixMilli(), Errors: make(map[string]string)}
	maxCalls := c.MaxCalls
	if maxCalls <= 0 {
		maxCalls = 200
	}
	var runErrors []error
	failed := make(map[string]bool)

	// Counts are separate official interfaces and are always captured first.
	for _, endpointName := range []string{"draft_count", "material_get_materialcount"} {
		if result.Calls >= maxCalls {
			break
		}
		if err := c.callAndArchive(ctx, endpointName, nil, "", ""); err != nil {
			result.Failed++
			result.Errors[endpointName] = err.Error()
			runErrors = append(runErrors, err)
			if isQuotaError(err) {
				break
			}
		} else {
			result.Succeeded++
		}
		result.Calls++
	}

	// First page of every inventory is refreshed every run, newest content first.
	recentObjects := make(map[string][]string)
	for _, stream := range contentStreams {
		if result.Calls >= maxCalls {
			break
		}
		info, err := c.fetchPage(ctx, stream, 0, false)
		result.Calls++
		if err != nil {
			result.Failed++
			failed[stream.Name] = true
			result.Errors[stream.Name] = err.Error()
			runErrors = append(runErrors, err)
			if isQuotaError(err) {
				break
			}
			continue
		}
		result.Succeeded++
		recentObjects[stream.Name] = info.ObjectIDs
	}

	// freepublish 是已发布正文归档与自动排版的权威来源，必须在详情、评论等
	// 可耗尽预算的接口之前获得固定分页机会。接口每页最多 20 个对象，20 页
	// 足够覆盖当前数百篇历史；批量响应已含完整正文，无需为历史页逐篇调用详情接口。
	publishedStream, hasPublishedStream := contentStreamByName("freepublish")
	for pages := 0; hasPublishedStream && pages < publishedHistoryPagesPerRun && result.Calls < maxCalls && !failed["freepublish"]; pages++ {
		state, err := c.Store.GetContentState(ctx, "freepublish")
		if err != nil {
			failed["freepublish"] = true
			result.Errors["freepublish_history"] = err.Error()
			runErrors = append(runErrors, err)
			break
		}
		if state != nil && state.Complete {
			break
		}
		offset := 0
		if state != nil {
			offset = state.NextOffset
		}
		_, err = c.fetchPage(ctx, publishedStream, offset, true)
		result.Calls++
		if err != nil {
			result.Failed++
			failed["freepublish"] = true
			result.Errors["freepublish_history"] = err.Error()
			runErrors = append(runErrors, err)
			if isQuotaError(err) {
				break
			}
			continue
		}
		result.Succeeded++
	}

	// Fetch full detail for recent article-bearing objects. Batch responses are
	// already complete, but invoking detail endpoints protects against field
	// differences and archives those official responses too.
	for _, stream := range contentStreams {
		if stream.DetailEndpoint == "" || failed[stream.Name] {
			continue
		}
		for _, objectID := range recentObjects[stream.Name] {
			if result.Calls >= maxCalls {
				break
			}
			err := c.fetchDetail(ctx, stream, objectID)
			result.Calls++
			if err != nil {
				result.Failed++
				failed[stream.Name+"_detail"] = true
				result.Errors[stream.Name+"_detail"] = err.Error()
				runErrors = append(runErrors, err)
				if isQuotaError(err) {
					break
				}
				// Permission errors affect the whole endpoint; do not repeat them
				// for every object in the same run.
				break
			}
			result.Succeeded++
		}
	}
	messageErrors := c.enrichRecentMessages(ctx, result, maxCalls)
	runErrors = append(runErrors, messageErrors...)

	// Continue all six inventories round-robin from durable offsets.
	for result.Calls < maxCalls {
		progressed := false
		for _, stream := range contentStreams {
			if result.Calls >= maxCalls || failed[stream.Name] {
				continue
			}
			state, err := c.Store.GetContentState(ctx, stream.Name)
			if err != nil {
				runErrors = append(runErrors, err)
				failed[stream.Name] = true
				continue
			}
			if state != nil && state.Complete {
				continue
			}
			offset := 0
			if state != nil {
				offset = state.NextOffset
			}
			progressed = true
			info, err := c.fetchPage(ctx, stream, offset, true)
			result.Calls++
			if err != nil {
				result.Failed++
				failed[stream.Name] = true
				result.Errors[stream.Name] = err.Error()
				runErrors = append(runErrors, err)
				if isQuotaError(err) {
					break
				}
				continue
			}
			result.Succeeded++

			// One queued detail per historical page keeps both list and detail
			// backfills moving without allowing 13k material details to starve lists.
			if stream.DetailEndpoint != "" && len(info.ObjectIDs) > 0 && result.Calls < maxCalls && !failed[stream.Name+"_detail"] {
				if err := c.fetchDetail(ctx, stream, info.ObjectIDs[0]); err != nil {
					result.Failed++
					failed[stream.Name+"_detail"] = true
					result.Errors[stream.Name+"_detail"] = err.Error()
					runErrors = append(runErrors, err)
				} else {
					result.Succeeded++
				}
				result.Calls++
			}
		}
		if !progressed {
			break
		}
	}

	result.FinishedAt = c.now().UnixMilli()
	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, errors.Join(runErrors...)
}

// enrichRecentMessages calls the status and comment interfaces for articles
// returned by yesterday's analytics. Comments are paged until the official
// total is exhausted or the run budget is reached.
func (c *ContentCollector) enrichRecentMessages(ctx context.Context, result *ContentRunResult, maxCalls int) []error {
	if result.Calls >= maxCalls {
		return nil
	}
	yesterday := startOfDay(c.now().In(wechat.ShanghaiLoc())).AddDate(0, 0, -1).Format("2006-01-02")
	messageIDs, err := c.Store.ArticleMessageIDs(ctx, yesterday, 20)
	if err != nil {
		return []error{err}
	}
	commentFailed := false
	errorsFound := make([]error, 0)
	for _, combinedID := range messageIDs {
		if result.Calls >= maxCalls {
			break
		}
		msgDataID, articleIndex, ok := splitMessageID(combinedID)
		if !ok {
			continue
		}
		if commentFailed {
			continue
		}

		const commentPageSize = 49
		for begin := 0; result.Calls < maxCalls; begin += commentPageSize {
			response, err := c.callAndArchiveResponse(ctx, "comment_list", map[string]interface{}{
				"msg_data_id": msgDataID,
				"index":       articleIndex,
				"begin":       begin,
				"count":       commentPageSize,
				"type":        0,
			}, yesterday, yesterday)
			result.Calls++
			if err != nil {
				result.Failed++
				commentFailed = true
				result.Errors["comment_list"] = err.Error()
				errorsFound = append(errorsFound, err)
				break
			}
			result.Succeeded++
			total, returned := commentPageInfo(response.Body)
			if returned < commentPageSize || begin+returned >= total {
				break
			}
		}
	}
	return errorsFound
}

func commentPageInfo(body []byte) (total int, returned int) {
	var response struct {
		Total   int               `json:"total"`
		Comment []json.RawMessage `json:"comment"`
	}
	if json.Unmarshal(body, &response) != nil {
		return 0, 0
	}
	return response.Total, len(response.Comment)
}

func (c *ContentCollector) fetchPage(ctx context.Context, stream contentStream, offset int, advanceState bool) (archive.ContentPageInfo, error) {
	response, err := c.callAndArchiveResponse(ctx, stream.ListEndpoint, stream.Payload(offset), "", "")
	if err != nil {
		if advanceState {
			_ = c.Store.MarkContentFailure(ctx, stream.Name, err.Error(), c.now())
		}
		return archive.ContentPageInfo{}, err
	}
	info, err := c.Store.SaveContentPage(ctx, stream.Source, response.Body, c.now())
	if err != nil {
		return archive.ContentPageInfo{}, err
	}
	if advanceState {
		itemCount := info.ItemCount
		if itemCount == 0 {
			itemCount = len(info.ObjectIDs)
		}
		nextOffset := offset + itemCount
		complete := itemCount == 0 || nextOffset >= info.TotalCount
		if err := c.Store.MarkContentPageSuccess(ctx, stream.Name, nextOffset, info.TotalCount, complete, c.now()); err != nil {
			return archive.ContentPageInfo{}, err
		}
	}
	return info, nil
}

func (c *ContentCollector) fetchDetail(ctx context.Context, stream contentStream, objectID string) error {
	response, err := c.callAndArchiveResponse(ctx, stream.DetailEndpoint, map[string]string{detailIDKey(stream.DetailEndpoint): objectID}, "", "")
	if err != nil {
		return err
	}
	return c.Store.SaveContentDetail(ctx, stream.Source, objectID, response.Body, c.now())
}

func detailIDKey(endpoint string) string {
	if endpoint == "freepublish_getarticle" {
		return "article_id"
	}
	return "media_id"
}

func (c *ContentCollector) callAndArchive(ctx context.Context, endpointName string, payload interface{}, beginDate, endDate string) error {
	_, err := c.callAndArchiveResponse(ctx, endpointName, payload, beginDate, endDate)
	return err
}

func (c *ContentCollector) callAndArchiveResponse(ctx context.Context, endpointName string, payload interface{}, beginDate, endDate string) (*wechat.RawAPIResponse, error) {
	endpoint, ok := wechat.ContentEndpointByName(endpointName)
	if !ok {
		return nil, fmt.Errorf("unknown content endpoint %q", endpointName)
	}
	fetchedAt := c.now()
	response, callErr := c.Client.Call(ctx, endpoint, payload)
	record := archive.FetchRecord{
		Endpoint:  endpoint.Name,
		Category:  endpoint.Category,
		BeginDate: beginDate,
		EndDate:   endDate,
		FetchedAt: fetchedAt,
		Success:   callErr == nil,
	}
	if response != nil {
		record.RequestJSON = response.RequestBody
		record.ResponseJSON = response.Body
		record.HTTPStatus = response.HTTPStatus
		record.WechatErrCode = response.ErrCode
		record.WechatErrMsg = response.ErrMsg
	}
	if callErr != nil {
		record.Error = callErr.Error()
	}
	if _, err := c.Store.SaveFetch(ctx, record); err != nil {
		return response, fmt.Errorf("persist %s response: %w", endpointName, err)
	}
	return response, callErr
}

func (c *ContentCollector) CallAndArchive(ctx context.Context, endpointName string, payload interface{}) (*wechat.RawAPIResponse, error) {
	if !c.runMu.TryLock() {
		return nil, fmt.Errorf("content collector is already running")
	}
	defer c.runMu.Unlock()
	return c.callAndArchiveResponse(ctx, endpointName, payload, "", "")
}

// RefreshPublished 供 15 分钟自动排版轮询使用：必须先刷新 draft
// 最新一页，保留 article_type=news|newspic 的官方分类快照，再刷新
// freepublish 最新一页。任一步仍经官方 API、原始响应归档和内容表落库，
// 不启动历史回填；draft 刷新失败时 fail closed，不拉取新的已发布候选。
func (c *ContentCollector) RefreshPublished(ctx context.Context) (archive.ContentPageInfo, error) {
	if !c.runMu.TryLock() {
		return archive.ContentPageInfo{}, fmt.Errorf("content collector is already running")
	}
	defer c.runMu.Unlock()
	draftStream, hasDraftStream := contentStreamByName("draft")
	publishedStream, hasPublishedStream := contentStreamByName("freepublish")
	if !hasDraftStream || !hasPublishedStream {
		return archive.ContentPageInfo{}, fmt.Errorf("draft/freepublish content streams are not configured")
	}
	if _, err := c.fetchPage(ctx, draftStream, 0, false); err != nil {
		return archive.ContentPageInfo{}, fmt.Errorf("refresh official draft types: %w", err)
	}
	return c.fetchPage(ctx, publishedStream, 0, false)
}

func (c *ContentCollector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func splitMessageID(combined string) (string, int, bool) {
	position := strings.LastIndex(combined, "_")
	if position <= 0 || position == len(combined)-1 {
		return "", 0, false
	}
	index, err := strconv.Atoi(combined[position+1:])
	if err != nil {
		return "", 0, false
	}
	// DataCube uses a 1-based article suffix (msg_data_id_1), while the
	// comment/list API uses a 0-based index.
	if index > 0 {
		index--
	}
	return combined[:position], index, true
}
