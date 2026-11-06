package feishu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
)

const bitableAPIBase = "https://open.feishu.cn/open-apis/bitable/v1/apps"

// ==================== Field Types ====================

const (
	FieldTypeText     = 1
	FieldTypeNumber   = 2
	FieldTypeDatetime = 5
	FieldTypeURL      = 15
	FieldTypeLink     = 18 // 单向关联，写入值是 record_id 数组
)

const (
	FieldTypeSingleSelect = 3
)

// FieldSpec 是要确保存在的一列。Property 只在新建这列时带上（数字格式、日期格式等），
// 已有的列不改。
type FieldSpec struct {
	Name     string
	Type     int
	Property map[string]interface{}
}

// Base 是一个多维表格。原始镜像用环境里的 FEISHU_BITABLE_APP_TOKEN，
// 业务数据表格用自己的 token，两者共用同一个飞书应用的 tenant token。
type Base struct {
	AppToken string
}

func defaultBase() Base { return Base{AppToken: config.Env.FeishuBitableAppToken} }

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

func (b Base) tableRequest(method, path string, body interface{}, tableID string) (json.RawMessage, error) {
	token, err := GetToken()
	if err != nil {
		return nil, err
	}
	fullURL := fmt.Sprintf("%s/%s/tables/%s%s", bitableAPIBase, b.AppToken, tableID, path)
	return doFeishuRequest(method, fullURL, body, token)
}

func (b Base) appRequest(method, path string, body interface{}) (json.RawMessage, error) {
	token, err := GetToken()
	if err != nil {
		return nil, err
	}
	fullURL := fmt.Sprintf("%s/%s%s", bitableAPIBase, b.AppToken, path)
	return doFeishuRequest(method, fullURL, body, token)
}

// ==================== Table Management ====================

var (
	tableIDCache   = make(map[string]string)
	fieldCache     = make(map[string]map[string]bool)
	tableIDCacheMu sync.Mutex
	recordWriteMu  sync.Mutex
)

// forgetTable drops what we cached about a table after a write fails, so the
// next run re-reads its ID and fields (a person may have renamed or deleted them).
func (b Base) forgetTable(tableID string) {
	tableIDCacheMu.Lock()
	defer tableIDCacheMu.Unlock()
	delete(fieldCache, b.key(tableID))
	for name, id := range tableIDCache {
		if id == tableID && strings.HasPrefix(name, b.AppToken+"|") {
			delete(tableIDCache, name)
		}
	}
}

func (b Base) key(name string) string { return b.AppToken + "|" + name }

func (b Base) fieldsKnown(tableID string, required []FieldSpec) bool {
	tableIDCacheMu.Lock()
	defer tableIDCacheMu.Unlock()
	known := fieldCache[b.key(tableID)]
	for _, field := range required {
		if !known[field.Name] {
			return false
		}
	}
	return known != nil
}

// GetOrCreateTable looks up or creates a Feishu Bitable table by name.
// An optional primaryFieldName can be provided; when the table is newly created
// that field becomes the first (primary/index) column. If omitted Feishu creates
// an unnamed default primary column.
func GetOrCreateTable(tableName string, primaryFieldName ...string) (string, error) {
	return defaultBase().GetOrCreateTable(tableName, primaryFieldName...)
}

func (b Base) GetOrCreateTable(tableName string, primaryFieldName ...string) (string, error) {
	tableIDCacheMu.Lock()
	if id, ok := tableIDCache[b.key(tableName)]; ok {
		tableIDCacheMu.Unlock()
		return id, nil
	}
	tableIDCacheMu.Unlock()

	data, err := b.appRequest("GET", "/tables?page_size=100", nil)
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
		tableIDCache[b.key(item.Name)] = item.TableID
	}
	if id, ok := tableIDCache[b.key(tableName)]; ok {
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
	createData, err := b.appRequest("POST", "/tables", map[string]interface{}{
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
	tableIDCache[b.key(tableName)] = createResult.TableID
	tableIDCacheMu.Unlock()

	log.Printf("[Feishu] Created table %q (%s)", tableName, createResult.TableID)
	return createResult.TableID, nil
}

// ==================== Field Management ====================

func (b Base) getExistingFields(tableID string) (map[string]bool, error) {
	fields := make(map[string]bool)
	pageToken := ""

	for {
		params := url.Values{"page_size": {"100"}}
		if pageToken != "" {
			params.Set("page_token", pageToken)
		}

		data, err := b.tableRequest("GET", "/fields?"+params.Encode(), nil, tableID)
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
	return defaultBase().EnsureFieldsExist(requiredFields, tableID)
}

func (b Base) EnsureFieldsExist(requiredFields []FieldSpec, tableID string) error {
	if b.fieldsKnown(tableID, requiredFields) {
		return nil
	}
	existing, err := b.getExistingFields(tableID)
	if err != nil {
		return err
	}

	for _, field := range requiredFields {
		if existing[field.Name] {
			continue
		}
		recordWriteMu.Lock()
		log.Printf("[Feishu] Creating field: %s (type: %d)", field.Name, field.Type)
		payload := map[string]interface{}{"field_name": field.Name, "type": field.Type}
		if field.Property != nil {
			payload["property"] = field.Property
		}
		_, err := b.tableRequest("POST", "/fields", payload, tableID)
		recordWriteMu.Unlock()
		if err != nil {
			return fmt.Errorf("create field %s: %w", field.Name, err)
		}
		existing[field.Name] = true
	}

	tableIDCacheMu.Lock()
	fieldCache[b.key(tableID)] = existing
	tableIDCacheMu.Unlock()
	return nil
}

// ==================== Record Operations ====================

func GetExistingRecords(keyField, tableID string) (map[string]string, error) {
	return defaultBase().GetExistingRecords(keyField, tableID)
}

func (b Base) GetExistingRecords(keyField, tableID string) (map[string]string, error) {
	records := make(map[string]string)
	pageToken := ""

	for {
		params := url.Values{"page_size": {"500"}}
		if pageToken != "" {
			params.Set("page_token", pageToken)
		}

		data, err := b.tableRequest("GET", "/records?"+params.Encode(), nil, tableID)
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

func recordWriteBatchSize() int {
	raw := strings.TrimSpace(os.Getenv("FEISHU_RECORD_BATCH_SIZE"))
	if raw == "" {
		return 20
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 20
	}
	if n > 500 {
		return 500
	}
	return n
}

// BatchCreateByRecordFieldsWithIDs creates records and returns their Feishu
// record IDs in request order. Official-data Base sync uses these IDs for
// incremental updates instead of re-reading an entire fact table every run.
func BatchCreateByRecordFieldsWithIDs(tableID string, records []map[string]interface{}) ([]string, error) {
	return defaultBase().BatchCreateByRecordFieldsWithIDs(tableID, records)
}

func (b Base) BatchCreateByRecordFieldsWithIDs(tableID string, records []map[string]interface{}) ([]string, error) {
	batchSize := recordWriteBatchSize()
	recordIDs := make([]string, 0, len(records))
	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}
		batch := records[i:end]
		recordWriteMu.Lock()
		data, err := b.tableRequest("POST", "/records/batch_create", map[string]interface{}{
			"records": batch,
		}, tableID)
		recordWriteMu.Unlock()
		if err != nil {
			b.forgetTable(tableID)
			return recordIDs, fmt.Errorf("batch create with IDs (offset %d): %w", i, err)
		}
		var result struct {
			Records []struct {
				RecordID string `json:"record_id"`
			} `json:"records"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return recordIDs, fmt.Errorf("parse batch create response (offset %d): %w", i, err)
		}
		if len(result.Records) != len(batch) {
			return recordIDs, fmt.Errorf("batch create response (offset %d) returned %d record IDs for %d records", i, len(result.Records), len(batch))
		}
		for _, record := range result.Records {
			if record.RecordID == "" {
				return recordIDs, fmt.Errorf("batch create response (offset %d) contains an empty record ID", i)
			}
			recordIDs = append(recordIDs, record.RecordID)
		}
		log.Printf("[Feishu] Batch created %d records with IDs (%d/%d)", len(batch), len(recordIDs), len(records))
	}
	return recordIDs, nil
}

func BatchUpdateByRecordID(tableID string, records []map[string]interface{}) error {
	return defaultBase().BatchUpdateByRecordID(tableID, records)
}

func (b Base) BatchUpdateByRecordID(tableID string, records []map[string]interface{}) error {
	batchSize := recordWriteBatchSize()
	updated := 0
	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}
		batch := records[i:end]
		recordWriteMu.Lock()
		_, err := b.tableRequest("POST", "/records/batch_update", map[string]interface{}{
			"records": batch,
		}, tableID)
		recordWriteMu.Unlock()
		if err != nil {
			b.forgetTable(tableID)
			return fmt.Errorf("batch update (offset %d): %w", i, err)
		}
		updated += len(batch)
		log.Printf("[Feishu] Batch updated %d records (%d/%d)", len(batch), updated, len(records))
	}
	return nil
}
