package feishu

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
)

// Record 是读回来的一行：record_id 和按列名取的值（飞书原样的 JSON 形状）。
type Record struct {
	RecordID string                 `json:"record_id"`
	Fields   map[string]interface{} `json:"fields"`
}

// SearchRecords 读整张表，只带 fieldNames 这几列。用于第一次接管已有表格时
// 按链接把行和文章对上。
func (b Base) SearchRecords(tableID string, fieldNames []string) ([]Record, error) {
	records := make([]Record, 0)
	pageToken := ""
	for {
		params := url.Values{"page_size": {"500"}}
		if pageToken != "" {
			params.Set("page_token", pageToken)
		}
		data, err := b.tableRequest("POST", "/records/search?"+params.Encode(), map[string]interface{}{
			"field_names": fieldNames,
		}, tableID)
		if err != nil {
			return nil, err
		}
		var page struct {
			Items     []Record `json:"items"`
			HasMore   bool     `json:"has_more"`
			PageToken string   `json:"page_token"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("parse record search: %w", err)
		}
		records = append(records, page.Items...)
		if !page.HasMore {
			return records, nil
		}
		pageToken = page.PageToken
	}
}

// BatchGetRecords 按 record_id 读回当前值；absent 是表里已经没有的行（被人删了）。
func (b Base) BatchGetRecords(tableID string, recordIDs []string) (records []Record, absent []string, err error) {
	const batch = 100
	for start := 0; start < len(recordIDs); start += batch {
		end := min(start+batch, len(recordIDs))
		data, err := b.tableRequest("POST", "/records/batch_get", map[string]interface{}{
			"record_ids": recordIDs[start:end],
		}, tableID)
		if err != nil {
			return records, absent, err
		}
		var page struct {
			Records   []Record `json:"records"`
			Absent    []string `json:"absent_record_ids"`
			Forbidden []string `json:"forbidden_record_ids"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return records, absent, fmt.Errorf("parse batch get: %w", err)
		}
		if len(page.Forbidden) > 0 {
			return records, absent, fmt.Errorf("batch get: no permission for %d records", len(page.Forbidden))
		}
		records = append(records, page.Records...)
		absent = append(absent, page.Absent...)
	}
	return records, absent, nil
}

// CreateTable 新建一张表，第一列是索引列。返回 table_id。
func (b Base) CreateTable(name string, fields []FieldSpec) (string, error) {
	specs := make([]map[string]interface{}, 0, len(fields))
	for _, field := range fields {
		spec := map[string]interface{}{"field_name": field.Name, "type": field.Type}
		if field.Property != nil {
			spec["property"] = field.Property
		}
		specs = append(specs, spec)
	}
	recordWriteMu.Lock()
	data, err := b.appRequest("POST", "/tables", map[string]interface{}{
		"table": map[string]interface{}{"name": name, "default_view_name": "全部", "fields": specs},
	})
	recordWriteMu.Unlock()
	if err != nil {
		return "", err
	}
	var created struct {
		TableID string `json:"table_id"`
	}
	if err := json.Unmarshal(data, &created); err != nil || created.TableID == "" {
		return "", fmt.Errorf("parse create table response: %v", err)
	}
	log.Printf("[Feishu] Created table %q (%s)", name, created.TableID)
	return created.TableID, nil
}
