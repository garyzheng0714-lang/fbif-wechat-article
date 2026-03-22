package wechat

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const materialAPIBase = "https://api.weixin.qq.com/cgi-bin/material"

func GetMaterialCount() (*MaterialCount, error) {
	if err := checkAndIncrementQuota("material_get_materialcount"); err != nil {
		return nil, err
	}

	token, err := GetToken()
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Get(fmt.Sprintf("%s/get_materialcount?access_token=%s", materialAPIBase, token))
	if err != nil {
		return nil, fmt.Errorf("material get_materialcount: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("material get_materialcount HTTP %d: %s", resp.StatusCode, string(body))
	}

	var count MaterialCount
	if err := json.NewDecoder(resp.Body).Decode(&count); err != nil {
		return nil, fmt.Errorf("decode material count: %w", err)
	}
	return &count, nil
}

func BatchGetMaterialNews(offset, count int) (*MaterialNewsBatch, error) {
	if err := checkAndIncrementQuota("material_batchget_news"); err != nil {
		return nil, err
	}

	token, err := GetToken()
	if err != nil {
		return nil, err
	}

	body, _ := json.Marshal(map[string]interface{}{
		"type":   "news",
		"offset": offset,
		"count":  count,
	})

	resp, err := httpClient.Post(
		fmt.Sprintf("%s/batchget_material?access_token=%s", materialAPIBase, token),
		"application/json",
		strings.NewReader(string(body)),
	)
	if err != nil {
		return nil, fmt.Errorf("material batchget_material: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("material batchget_material HTTP %d: %s", resp.StatusCode, string(body))
	}

	var batch MaterialNewsBatch
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		return nil, fmt.Errorf("decode material batch: %w", err)
	}
	if batch.ErrCode != 0 {
		return nil, fmt.Errorf("material batchget_material error %d: %s", batch.ErrCode, batch.ErrMsg)
	}
	return &batch, nil
}
