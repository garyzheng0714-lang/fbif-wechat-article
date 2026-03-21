package feishu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
)

const bitableAPIBase = "https://open.feishu.cn/open-apis/bitable/v1/apps"

// ==================== Label Maps ====================

var UserSourceLabels = map[int]string{
	0: "其他", 1: "搜索", 17: "名片分享", 30: "扫码",
	57: "文章内账号名", 100: "微信广告", 161: "他人转载",
	149: "小程序关注", 200: "视频号", 201: "直播",
}

var ReadSourceLabels = map[int]string{
	0: "会话", 1: "好友", 2: "朋友圈", 4: "历史消息",
	5: "其他", 6: "看一看", 7: "搜一搜", 99999999: "全部",
}

var ShareSceneLabels = map[int]string{
	1: "好友转发", 2: "朋友圈", 255: "其他",
}

func LabelOrUnknown(m map[int]string, key int) string {
	if v, ok := m[key]; ok {
		return v
	}
	return fmt.Sprintf("未知(%d)", key)
}

// ==================== Field Types ====================

const (
	FieldTypeText     = 1
	FieldTypeNumber   = 2
	FieldTypeDatetime = 5
	FieldTypeURL      = 15
)

type FieldSpec struct {
	Name string
	Type int
}

// ==================== Feishu API ====================

type apiResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func doFeishuRequest(method, fullURL string, body interface{}, token string) (json.RawMessage, error) {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("feishu request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("feishu HTTP %d: %s", resp.StatusCode, snippet)
	}

	var result apiResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode feishu response: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("Feishu Bitable API error %d: %s", result.Code, result.Msg)
	}
	return result.Data, nil
}

func feishuRequest(method, path string, body interface{}, tableID string) (json.RawMessage, error) {
	token, err := GetToken()
	if err != nil {
		return nil, err
	}
	appToken := config.Env.FeishuBitableAppToken
	fullURL := fmt.Sprintf("%s/%s/tables/%s%s", bitableAPIBase, appToken, tableID, path)
	return doFeishuRequest(method, fullURL, body, token)
}

func feishuAppRequest(method, path string, body interface{}) (json.RawMessage, error) {
	token, err := GetToken()
	if err != nil {
		return nil, err
	}
	appToken := config.Env.FeishuBitableAppToken
	fullURL := fmt.Sprintf("%s/%s%s", bitableAPIBase, appToken, path)
	return doFeishuRequest(method, fullURL, body, token)
}

// ==================== Table Management ====================

var (
	tableIDCache   = make(map[string]string)
	tableIDCacheMu sync.Mutex
)

// GetOrCreateTable looks up or creates a Feishu Bitable table by name.
// An optional primaryFieldName can be provided; when the table is newly created
// that field becomes the first (primary/index) column. If omitted Feishu creates
// an unnamed default primary column.
func GetOrCreateTable(tableName string, primaryFieldName ...string) (string, error) {
	tableIDCacheMu.Lock()
	if id, ok := tableIDCache[tableName]; ok {
		tableIDCacheMu.Unlock()
		return id, nil
	}
	tableIDCacheMu.Unlock()

	data, err := feishuAppRequest("GET", "/tables", nil)
	if err != nil {
		return "", err
	}

	var result struct {
		Items []struct {
			Name    string `json:"name"`
			TableID string `json:"table_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse tables list: %w", err)
	}

	tableIDCacheMu.Lock()
	for _, item := range result.Items {
		tableIDCache[item.Name] = item.TableID
	}
	if id, ok := tableIDCache[tableName]; ok {
		tableIDCacheMu.Unlock()
		log.Printf("[Feishu] Found existing table %q (%s)", tableName, id)
		return id, nil
	}
	tableIDCacheMu.Unlock()

	log.Printf("[Feishu] Creating new table: %s", tableName)
	tablePayload := map[string]interface{}{"name": tableName}
	if len(primaryFieldName) > 0 && primaryFieldName[0] != "" {
		tablePayload["fields"] = []map[string]interface{}{
			{"field_name": primaryFieldName[0], "type": FieldTypeText},
		}
	}
	createData, err := feishuAppRequest("POST", "/tables", map[string]interface{}{
		"table": tablePayload,
	})
	if err != nil {
		return "", err
	}

	var createResult struct {
		TableID string `json:"table_id"`
	}
	if err := json.Unmarshal(createData, &createResult); err != nil {
		return "", fmt.Errorf("parse create table response: %w", err)
	}

	tableIDCacheMu.Lock()
	tableIDCache[tableName] = createResult.TableID
	tableIDCacheMu.Unlock()

	log.Printf("[Feishu] Created table %q (%s)", tableName, createResult.TableID)
	return createResult.TableID, nil
}

// ==================== Field Management ====================

func getExistingFields(tableID string) (map[string]bool, error) {
	fields := make(map[string]bool)
	pageToken := ""

	for {
		params := url.Values{"page_size": {"100"}}
		if pageToken != "" {
			params.Set("page_token", pageToken)
		}

		data, err := feishuRequest("GET", "/fields?"+params.Encode(), nil, tableID)
		if err != nil {
			return nil, err
		}

		var result struct {
			Items []struct {
				FieldName string `json:"field_name"`
			} `json:"items"`
			HasMore   bool   `json:"has_more"`
			PageToken string `json:"page_token"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("parse fields: %w", err)
		}

		for _, item := range result.Items {
			fields[item.FieldName] = true
		}

		if !result.HasMore {
			break
		}
		pageToken = result.PageToken
	}

	return fields, nil
}

func EnsureFieldsExist(requiredFields []FieldSpec, tableID string) error {
	existing, err := getExistingFields(tableID)
	if err != nil {
		return err
	}

	for _, field := range requiredFields {
		if existing[field.Name] {
			continue
		}
		log.Printf("[Feishu] Creating field: %s (type: %d)", field.Name, field.Type)
		_, err := feishuRequest("POST", "/fields", map[string]interface{}{
			"field_name": field.Name,
			"type":       field.Type,
		}, tableID)
		if err != nil {
			return fmt.Errorf("create field %s: %w", field.Name, err)
		}
	}

	return nil
}

// ==================== Record Operations ====================

func GetExistingRecords(keyField, tableID string) (map[string]string, error) {
	records := make(map[string]string)
	pageToken := ""

	for {
		params := url.Values{"page_size": {"500"}}
		if pageToken != "" {
			params.Set("page_token", pageToken)
		}

		data, err := feishuRequest("GET", "/records?"+params.Encode(), nil, tableID)
		if err != nil {
			return nil, err
		}

		var result struct {
			Items []struct {
				RecordID string                 `json:"record_id"`
				Fields   map[string]interface{} `json:"fields"`
			} `json:"items"`
			HasMore   bool   `json:"has_more"`
			PageToken string `json:"page_token"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("parse records: %w", err)
		}

		for _, item := range result.Items {
			key := fmt.Sprintf("%v", item.Fields[keyField])
			if key != "" && key != "<nil>" {
				records[key] = item.RecordID
			}
		}

		if !result.HasMore {
			break
		}
		pageToken = result.PageToken
	}

	return records, nil
}

type SyncRecord struct {
	UniqueKey string
	Fields    map[string]interface{}
}

type SyncResult struct {
	Created int `json:"created"`
	Skipped int `json:"skipped,omitempty"`
	Updated int `json:"updated,omitempty"`
}

func SyncRecordsInsertOnly(records []SyncRecord, keyField, tableID string) (*SyncResult, error) {
	if len(records) == 0 {
		return &SyncResult{}, nil
	}

	existing, err := GetExistingRecords(keyField, tableID)
	if err != nil {
		return nil, err
	}

	var createList []map[string]interface{}
	skipped := 0

	for _, r := range records {
		if _, exists := existing[r.UniqueKey]; exists {
			skipped++
		} else {
			createList = append(createList, map[string]interface{}{"fields": r.Fields})
		}
	}

	created := 0
	for i := 0; i < len(createList); i += 500 {
		end := i + 500
		if end > len(createList) {
			end = len(createList)
		}
		batch := createList[i:end]
		_, err := feishuRequest("POST", "/records/batch_create", map[string]interface{}{
			"records": batch,
		}, tableID)
		if err != nil {
			return nil, fmt.Errorf("batch create: %w", err)
		}
		created += len(batch)
		log.Printf("[Feishu] Batch created %d records (%d/%d)", len(batch), created, len(createList))
	}

	return &SyncResult{Created: created, Skipped: skipped}, nil
}

func SyncRecordsUpsert(records []SyncRecord, keyField, tableID string) (*SyncResult, error) {
	if len(records) == 0 {
		return &SyncResult{}, nil
	}

	existing, err := GetExistingRecords(keyField, tableID)
	if err != nil {
		return nil, err
	}

	var createList []map[string]interface{}
	var updateList []map[string]interface{}

	for _, r := range records {
		if recordID, exists := existing[r.UniqueKey]; exists {
			updateList = append(updateList, map[string]interface{}{
				"record_id": recordID,
				"fields":    r.Fields,
			})
		} else {
			createList = append(createList, map[string]interface{}{"fields": r.Fields})
		}
	}

	created := 0
	for i := 0; i < len(createList); i += 500 {
		end := i + 500
		if end > len(createList) {
			end = len(createList)
		}
		batch := createList[i:end]
		_, err := feishuRequest("POST", "/records/batch_create", map[string]interface{}{
			"records": batch,
		}, tableID)
		if err != nil {
			return nil, fmt.Errorf("batch create: %w", err)
		}
		created += len(batch)
		log.Printf("[Feishu] Batch created %d records (%d/%d)", len(batch), created, len(createList))
	}

	updated := 0
	for i := 0; i < len(updateList); i += 500 {
		end := i + 500
		if end > len(updateList) {
			end = len(updateList)
		}
		batch := updateList[i:end]
		_, err := feishuRequest("POST", "/records/batch_update", map[string]interface{}{
			"records": batch,
		}, tableID)
		if err != nil {
			return nil, fmt.Errorf("batch update: %w", err)
		}
		updated += len(batch)
		log.Printf("[Feishu] Batch updated %d records (%d/%d)", len(batch), updated, len(updateList))
	}

	return &SyncResult{Created: created, Updated: updated}, nil
}

// ==================== Content Sync Helpers ====================

// ArticleForContent holds the minimal data needed for a content sync pass.
type ArticleForContent struct {
	RecordID   string
	UniqueKey  string
	ArticleURL string
}

// GetArticlesNeedingContent returns records in tableID that have 文章链接 set
// but 文章内容 empty. These are the candidates for content fetching.
func GetArticlesNeedingContent(tableID string) ([]ArticleForContent, error) {
	var result []ArticleForContent
	pageToken := ""

	for {
		params := url.Values{
			"page_size":   {"500"},
			"field_names": {"唯一键,文章链接,文章内容"},
		}
		if pageToken != "" {
			params.Set("page_token", pageToken)
		}

		data, err := feishuRequest("GET", "/records?"+params.Encode(), nil, tableID)
		if err != nil {
			return nil, err
		}

		var res struct {
			Items []struct {
				RecordID string                 `json:"record_id"`
				Fields   map[string]interface{} `json:"fields"`
			} `json:"items"`
			HasMore   bool   `json:"has_more"`
			PageToken string `json:"page_token"`
		}
		if err := json.Unmarshal(data, &res); err != nil {
			return nil, fmt.Errorf("parse records: %w", err)
		}

		for _, item := range res.Items {
			artURL := extractFieldString(item.Fields, "文章链接")
			content := extractFieldString(item.Fields, "文章内容")
			if artURL == "" || content != "" {
				continue
			}
			uniqueKey := extractFieldString(item.Fields, "唯一键")
			result = append(result, ArticleForContent{
				RecordID:   item.RecordID,
				UniqueKey:  uniqueKey,
				ArticleURL: artURL,
			})
		}

		if !res.HasMore {
			break
		}
		pageToken = res.PageToken
	}

	return result, nil
}

// extractFieldString pulls a string value from a Feishu field.
// Handles plain strings, URL objects {"link":"..."}, and text arrays.
func extractFieldString(fields map[string]interface{}, name string) string {
	v, ok := fields[name]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case map[string]interface{}:
		if link, ok := t["link"].(string); ok {
			return link
		}
	case []interface{}:
		// Feishu text fields return [{type:"text",text:"..."}]
		var parts []string
		for _, el := range t {
			if m, ok := el.(map[string]interface{}); ok {
				if s, ok := m["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "")
	}
	return fmt.Sprintf("%v", v)
}

// UpdateRecordFields updates a single record's fields.
func UpdateRecordFields(tableID, recordID string, fields map[string]interface{}) error {
	_, err := feishuRequest("PUT", "/records/"+recordID, map[string]interface{}{
		"fields": fields,
	}, tableID)
	return err
}

// BatchUpdateByRecordID performs batch updates given a list of
// {"record_id": "...", "fields": {...}} objects (Feishu batch_update format).
// Processes in chunks of 500.
func BatchUpdateByRecordID(tableID string, records []map[string]interface{}) error {
	for i := 0; i < len(records); i += 500 {
		end := i + 500
		if end > len(records) {
			end = len(records)
		}
		batch := records[i:end]
		_, err := feishuRequest("POST", "/records/batch_update", map[string]interface{}{
			"records": batch,
		}, tableID)
		if err != nil {
			return fmt.Errorf("batch update (offset %d): %w", i, err)
		}
		log.Printf("[Feishu] Batch updated %d records (%d/%d)", len(batch), i+len(batch), len(records))
	}
	return nil
}

// ==================== Clear Records ====================

func ClearTableRecords(tableID string) (int, error) {
	deleted := 0

	for {
		data, err := feishuRequest("GET", "/records?page_size=500", nil, tableID)
		if err != nil {
			return deleted, err
		}

		var result struct {
			Items []struct {
				RecordID string `json:"record_id"`
			} `json:"items"`
			HasMore bool `json:"has_more"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return deleted, fmt.Errorf("parse records for delete: %w", err)
		}

		if len(result.Items) == 0 {
			break
		}

		ids := make([]string, len(result.Items))
		for i, item := range result.Items {
			ids[i] = item.RecordID
		}

		_, err = feishuRequest("POST", "/records/batch_delete", map[string]interface{}{
			"records": ids,
		}, tableID)
		if err != nil {
			return deleted, fmt.Errorf("batch delete: %w", err)
		}
		deleted += len(ids)
		log.Printf("[Feishu] Deleted %d records (total: %d)", len(ids), deleted)

		if !result.HasMore {
			break
		}
	}

	return deleted, nil
}
