package wechat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

const wechatAPIBase = "https://api.weixin.qq.com/datacube"

type QuotaLimitError struct {
	Endpoint string
}

func (e *QuotaLimitError) Error() string {
	return fmt.Sprintf("WeChat API daily quota limit reached for %s", e.Endpoint)
}

// maxSpan defines the maximum date span per API endpoint.
var maxSpan = map[string]int{
	"getarticlesummary": 1,
	"getarticletotal":   1,
	"getuserread":       3,
	"getuserreadhour":   1,
	"getusershare":      7,
	"getusersharehour":  1,
	"getusersummary":    7,
	"getusercumulate":   7,
}

// Cache entry
type cacheEntry struct {
	data      json.RawMessage
	expiresAt time.Time
}

var (
	apiCache   = make(map[string]*cacheEntry)
	apiCacheMu sync.Mutex
)

func getCacheTTL(endDate string) time.Duration {
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	if endDate >= yesterday {
		return 60 * time.Second
	}
	return time.Hour
}

func getCacheKey(endpoint, beginDate, endDate string) string {
	return endpoint + ":" + beginDate + ":" + endDate
}

// GetDateRange returns all dates in [begin, end] as YYYY-MM-DD strings.
func GetDateRange(beginDate, endDate string) ([]string, error) {
	loc := ShanghaiLoc()
	start, err := time.ParseInLocation("2006-01-02", beginDate, loc)
	if err != nil {
		return nil, fmt.Errorf("parse begin date: %w", err)
	}
	end, err := time.ParseInLocation("2006-01-02", endDate, loc)
	if err != nil {
		return nil, fmt.Errorf("parse end date: %w", err)
	}

	var dates []string
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		dates = append(dates, d.Format("2006-01-02"))
	}
	return dates, nil
}

type apiResponse struct {
	ErrCode int             `json:"errcode"`
	ErrMsg  string          `json:"errmsg"`
	List    json.RawMessage `json:"list"`
}

func callWechatAPISingle(endpoint, token, beginDate, endDate string) (*apiResponse, error) {
	if err := checkAndIncrementQuota(endpoint); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/%s?access_token=%s", wechatAPIBase, endpoint, token)

	body, _ := json.Marshal(map[string]string{
		"begin_date": beginDate,
		"end_date":   endDate,
	})

	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("wechat api %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		snippet := string(respBody)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("wechat HTTP %d for %s: %s", resp.StatusCode, endpoint, snippet)
	}

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode wechat response for %s: %w", endpoint, err)
	}
	return &result, nil
}

func handleAPIError(result *apiResponse, endpoint string) error {
	if result.ErrCode == 0 {
		return nil
	}
	if strings.Contains(result.ErrMsg, "quota") {
		return &QuotaLimitError{Endpoint: endpoint}
	}
	return fmt.Errorf("WeChat API error %d: %s", result.ErrCode, result.ErrMsg)
}

// CallWechatAPI calls a WeChat datacube API, handling date splitting, token refresh, and caching.
func CallWechatAPI(endpoint, beginDate, endDate string) (json.RawMessage, error) {
	cacheKey := getCacheKey(endpoint, beginDate, endDate)

	apiCacheMu.Lock()
	if entry, ok := apiCache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		data := entry.data
		apiCacheMu.Unlock()
		return data, nil
	}
	apiCacheMu.Unlock()

	token, err := GetToken()
	if err != nil {
		return nil, err
	}

	span := maxSpan[endpoint]
	if span == 0 {
		span = 1
	}

	dates, err := GetDateRange(beginDate, endDate)
	if err != nil {
		return nil, err
	}

	var allItems []json.RawMessage

	if len(dates) <= span {
		items, err := fetchWithRetry(endpoint, token, beginDate, endDate)
		if err != nil {
			return nil, err
		}
		allItems = items
	} else {
		type chunk struct{ begin, end string }
		var chunks []chunk
		for i := 0; i < len(dates); i += span {
			end := i + span
			if end > len(dates) {
				end = len(dates)
			}
			chunks = append(chunks, chunk{begin: dates[i], end: dates[end-1]})
		}

		type result struct {
			items []json.RawMessage
			err   error
		}

		sem := make(chan struct{}, 5)
		results := make([]result, len(chunks))
		var wg sync.WaitGroup

		for i, c := range chunks {
			wg.Add(1)
			go func(idx int, c chunk) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				t, err := GetToken()
				if err != nil {
					results[idx] = result{err: err}
					return
				}
				items, err := fetchWithRetry(endpoint, t, c.begin, c.end)
				results[idx] = result{items: items, err: err}
			}(i, c)
		}
		wg.Wait()

		for _, r := range results {
			if r.err != nil {
				if _, ok := r.err.(*QuotaLimitError); ok {
					return nil, r.err
				}
				log.Printf("[WeChat] API error for %s: %v", endpoint, r.err)
				continue
			}
			allItems = append(allItems, r.items...)
		}
	}

	listJSON, err := json.Marshal(allItems)
	if err != nil {
		return nil, fmt.Errorf("marshal list: %w", err)
	}

	apiCacheMu.Lock()
	apiCache[cacheKey] = &cacheEntry{
		data:      listJSON,
		expiresAt: time.Now().Add(getCacheTTL(endDate)),
	}
	apiCacheMu.Unlock()

	return listJSON, nil
}

func fetchWithRetry(endpoint, token, beginDate, endDate string) ([]json.RawMessage, error) {
	result, err := callWechatAPISingle(endpoint, token, beginDate, endDate)
	if err != nil {
		return nil, err
	}

	if result.ErrCode == 40001 {
		newToken, err := RefreshTokenNow()
		if err != nil {
			return nil, err
		}
		result, err = callWechatAPISingle(endpoint, newToken, beginDate, endDate)
		if err != nil {
			return nil, err
		}
		if err := handleAPIError(result, endpoint); err != nil {
			return nil, err
		}
	} else if err := handleAPIError(result, endpoint); err != nil {
		return nil, err
	}

	return parseList(result.List)
}

func parseList(raw json.RawMessage) ([]json.RawMessage, error) {
	if raw == nil || string(raw) == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parse list: %w", err)
	}
	return items, nil
}
