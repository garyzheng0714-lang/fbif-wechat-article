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
	recordWriteMu  sync.Mutex
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
		recordWriteMu.Lock()
		log.Printf("[Feishu] Creating field: %s (type: %d)", field.Name, field.Type)
		_, err := feishuRequest("POST", "/fields", map[string]interface{}{
			"field_name": field.Name,
			"type":       field.Type,
		}, tableID)
		recordWriteMu.Unlock()
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

type Record struct {
	RecordID string
	Fields   map[string]interface{}
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

func BatchCreateByRecordFields(tableID string, records []map[string]interface{}) error {
	batchSize := recordWriteBatchSize()
	created := 0
	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}
		batch := records[i:end]
		recordWriteMu.Lock()
		_, err := feishuRequest("POST", "/records/batch_create", map[string]interface{}{
			"records": batch,
		}, tableID)
		recordWriteMu.Unlock()
		if err != nil {
			return fmt.Errorf("batch create (offset %d): %w", i, err)
		}
		created += len(batch)
		log.Printf("[Feishu] Batch created %d records (%d/%d)", len(batch), created, len(records))
	}
	return nil
}

func ListRecords(tableID string, fieldNames []string) ([]Record, error) {
	var records []Record
	pageToken := ""
	useFieldNames := len(fieldNames) > 0

	for {
		params := url.Values{"page_size": {"500"}}
		if useFieldNames {
			params.Set("field_names", strings.Join(fieldNames, ","))
		}
		if pageToken != "" {
			params.Set("page_token", pageToken)
		}

		data, err := feishuRequest("GET", "/records?"+params.Encode(), nil, tableID)
		if err != nil {
			if useFieldNames && strings.Contains(err.Error(), "InvalidFieldNames") {
				log.Printf("[Feishu] ListRecords: field_names not yet valid, falling back to full fields")
				useFieldNames = false
				pageToken = ""
				records = nil
				continue
			}
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
			records = append(records, Record{
				RecordID: item.RecordID,
				Fields:   item.Fields,
			})
		}

		if !result.HasMore {
			break
		}
		pageToken = result.PageToken
	}

	return records, nil
}

func BatchUpdateByRecordID(tableID string, records []map[string]interface{}) error {
	batchSize := recordWriteBatchSize()
	updated := 0
	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}
		batch := records[i:end]
		recordWriteMu.Lock()
		_, err := feishuRequest("POST", "/records/batch_update", map[string]interface{}{
			"records": batch,
		}, tableID)
		recordWriteMu.Unlock()
		if err != nil {
			return fmt.Errorf("batch update (offset %d): %w", i, err)
		}
		updated += len(batch)
		log.Printf("[Feishu] Batch updated %d records (%d/%d)", len(batch), updated, len(records))
	}
	return nil
}

func FieldString(fields map[string]interface{}, name string) string {
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

func FieldInt64(fields map[string]interface{}, name string) int64 {
	v, ok := fields[name]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	}
	return 0
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
	if len(createList) > 0 {
		if err := BatchCreateByRecordFields(tableID, createList); err != nil {
			return nil, fmt.Errorf("batch create: %w", err)
		}
		created = len(createList)
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
	if len(createList) > 0 {
		if err := BatchCreateByRecordFields(tableID, createList); err != nil {
			return nil, fmt.Errorf("batch create: %w", err)
		}
		created = len(createList)
	}

	updated := 0
	if len(updateList) > 0 {
		if err := BatchUpdateByRecordID(tableID, updateList); err != nil {
			return nil, fmt.Errorf("batch update: %w", err)
		}
		updated = len(updateList)
	}

	return &SyncResult{Created: created, Updated: updated}, nil
}
