package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const dataCubeAPIBase = "https://api.weixin.qq.com/datacube"

// DataCubeEndpoint describes one official-account analytics endpoint and its
// server-side date-window constraints. The response is intentionally kept as
// raw JSON so newly added WeChat fields are archived without a code change.
type DataCubeEndpoint struct {
	Name          string `json:"name"`
	Category      string `json:"category"`
	Lifecycle     string `json:"lifecycle"`
	Replacement   string `json:"replacement,omitempty"`
	EarliestDate  string `json:"earliestDate"`
	MaxSpanDays   int    `json:"maxSpanDays"`
	RefreshDays   int    `json:"refreshDays"`
	Documentation string `json:"documentation"`
}

var dataCubeEndpoints = []DataCubeEndpoint{
	// Article and account-content analytics.
	{Name: "getarticleread", Category: "article", Lifecycle: "active", EarliestDate: "2025-11-01", MaxSpanDays: 1, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/news/api_getarticleread.html"},
	{Name: "getarticleshare", Category: "article", Lifecycle: "active", EarliestDate: "2025-11-01", MaxSpanDays: 1, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/news/api_getarticleshare.html"},
	{Name: "getarticlesummary", Category: "article", Lifecycle: "retired", Replacement: "getarticleread,getarticleshare", EarliestDate: "2014-12-01", MaxSpanDays: 1, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/news/api_getarticlesummary.html"},
	{Name: "getarticletotal", Category: "article", Lifecycle: "retired", Replacement: "getarticletotaldetail", EarliestDate: "2014-12-01", MaxSpanDays: 1, RefreshDays: 7, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/news/api_getarticletotal.html"},
	{Name: "getarticletotaldetail", Category: "article", Lifecycle: "active", EarliestDate: "2025-11-01", MaxSpanDays: 1, RefreshDays: 30, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/news/api_getarticletotaldetail.html"},
	{Name: "getbizsummary", Category: "article", Lifecycle: "active", EarliestDate: "2025-11-01", MaxSpanDays: 30, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/news/api_getbizsummary.html"},
	{Name: "getuserread", Category: "article", Lifecycle: "retired", Replacement: "getbizsummary", EarliestDate: "2014-12-01", MaxSpanDays: 3, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/news/api_getuserread.html"},
	{Name: "getuserreadhour", Category: "article", Lifecycle: "retired", Replacement: "getarticleread,getbizsummary", EarliestDate: "2014-12-01", MaxSpanDays: 1, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/news/api_getuserreadhour.html"},
	{Name: "getusershare", Category: "article", Lifecycle: "retired", Replacement: "getarticleshare,getbizsummary", EarliestDate: "2014-12-01", MaxSpanDays: 7, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/news/api_getusershare.html"},
	{Name: "getusersharehour", Category: "article", Lifecycle: "retired", Replacement: "getarticleshare,getbizsummary", EarliestDate: "2014-12-01", MaxSpanDays: 1, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/news/api_getusersharehour.html"},

	// Follower analytics.
	{Name: "getusercumulate", Category: "user", Lifecycle: "active", EarliestDate: "2014-12-01", MaxSpanDays: 7, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/user/api_getusercumulate.html"},
	{Name: "getusersummary", Category: "user", Lifecycle: "active", EarliestDate: "2014-12-01", MaxSpanDays: 7, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/user/api_getusersummary.html"},

	// Incoming-message analytics.
	{Name: "getupstreammsg", Category: "message", Lifecycle: "active", EarliestDate: "2014-12-01", MaxSpanDays: 7, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/mess/api_getupstreammsg.html"},
	{Name: "getupstreammsghour", Category: "message", Lifecycle: "active", EarliestDate: "2014-12-01", MaxSpanDays: 1, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/mess/api_getupstreammsghour.html"},
	{Name: "getupstreammsgweek", Category: "message", Lifecycle: "active", EarliestDate: "2014-12-01", MaxSpanDays: 30, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/mess/api_getupstreammsgweek.html"},
	{Name: "getupstreammsgmonth", Category: "message", Lifecycle: "active", EarliestDate: "2014-12-01", MaxSpanDays: 30, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/mess/api_getupstreammsgmonth.html"},
	{Name: "getupstreammsgdist", Category: "message", Lifecycle: "active", EarliestDate: "2014-12-01", MaxSpanDays: 15, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/mess/api_getupstreammsgdist.html"},
	{Name: "getupstreammsgdistweek", Category: "message", Lifecycle: "active", EarliestDate: "2014-12-01", MaxSpanDays: 15, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/mess/api_getupstreammsgdistweek.html"},
	{Name: "getupstreammsgdistmonth", Category: "message", Lifecycle: "active", EarliestDate: "2014-12-01", MaxSpanDays: 15, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/mess/api_getupstreammsgdistmonth.html"},

	// Passive API-response analytics.
	{Name: "getinterfacesummary", Category: "interface", Lifecycle: "active", EarliestDate: "2014-12-01", MaxSpanDays: 30, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/api/api_getinterfacesummary.html"},
	{Name: "getinterfacesummaryhour", Category: "interface", Lifecycle: "active", EarliestDate: "2014-12-01", MaxSpanDays: 1, RefreshDays: 1, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/wedata/api/api_getinterfacesummaryhour.html"},
}

func ActiveDataCubeEndpoints() []DataCubeEndpoint {
	result := make([]DataCubeEndpoint, 0, len(dataCubeEndpoints))
	for _, endpoint := range dataCubeEndpoints {
		if endpoint.Lifecycle == "active" {
			result = append(result, endpoint)
		}
	}
	return result
}

func RetiredDataCubeEndpoints() []DataCubeEndpoint {
	result := make([]DataCubeEndpoint, 0)
	for _, endpoint := range dataCubeEndpoints {
		if endpoint.Lifecycle == "retired" {
			result = append(result, endpoint)
		}
	}
	return result
}

func AllDataCubeEndpoints() []DataCubeEndpoint {
	result := make([]DataCubeEndpoint, len(dataCubeEndpoints))
	copy(result, dataCubeEndpoints)
	return result
}

func DataCubeEndpointByName(name string) (DataCubeEndpoint, bool) {
	for _, endpoint := range dataCubeEndpoints {
		if endpoint.Name == name {
			return endpoint, true
		}
	}
	return DataCubeEndpoint{}, false
}

type DateWindow struct {
	Begin string `json:"beginDate"`
	End   string `json:"endDate"`
}

// SplitDataCubeRange splits an inclusive date range into API-valid windows.
func SplitDataCubeRange(endpoint DataCubeEndpoint, beginDate, endDate string) ([]DateWindow, error) {
	begin, err := parseAPIDate(beginDate)
	if err != nil {
		return nil, fmt.Errorf("begin date: %w", err)
	}
	end, err := parseAPIDate(endDate)
	if err != nil {
		return nil, fmt.Errorf("end date: %w", err)
	}
	if end.Before(begin) {
		return nil, fmt.Errorf("end date %s is before begin date %s", endDate, beginDate)
	}
	maxDays := endpoint.MaxSpanDays
	if maxDays <= 0 {
		maxDays = 1
	}

	result := make([]DateWindow, 0)
	for cursor := begin; !cursor.After(end); {
		windowEnd := cursor.AddDate(0, 0, maxDays-1)
		if windowEnd.After(end) {
			windowEnd = end
		}
		result = append(result, DateWindow{
			Begin: cursor.Format("2006-01-02"),
			End:   windowEnd.Format("2006-01-02"),
		})
		cursor = windowEnd.AddDate(0, 0, 1)
	}
	return result, nil
}

func parseAPIDate(value string) (time.Time, error) {
	parsed, err := time.ParseInLocation("2006-01-02", value, ShanghaiLoc())
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid YYYY-MM-DD date %q", value)
	}
	return parsed, nil
}

type RawAPIResponse struct {
	Endpoint    string
	RequestBody []byte
	Body        []byte
	HTTPStatus  int
	ErrCode     int
	ErrMsg      string
}

type APIError struct {
	Endpoint   string
	HTTPStatus int
	ErrCode    int
	ErrMsg     string
}

func (e *APIError) Error() string {
	if e.ErrCode != 0 {
		return fmt.Sprintf("WeChat %s error %d: %s", e.Endpoint, e.ErrCode, e.ErrMsg)
	}
	return fmt.Sprintf("WeChat %s HTTP %d: %s", e.Endpoint, e.HTTPStatus, e.ErrMsg)
}

type DataCubeClient struct {
	BaseURL      string
	HTTPClient   *http.Client
	GetToken     func() (string, error)
	RefreshToken func() (string, error)
	BeforeCall   func(endpoint string) error
}

func NewDataCubeClient() *DataCubeClient {
	return &DataCubeClient{
		BaseURL:      dataCubeAPIBase,
		HTTPClient:   httpClient,
		GetToken:     GetToken,
		RefreshToken: RefreshTokenNow,
		BeforeCall:   checkAndIncrementQuota,
	}
}

// Call fetches one valid date window. It returns the exact response body even
// when WeChat reports an API error, allowing the collector to archive failures.
func (c *DataCubeClient) Call(ctx context.Context, endpoint DataCubeEndpoint, beginDate, endDate string) (*RawAPIResponse, error) {
	windows, err := SplitDataCubeRange(endpoint, beginDate, endDate)
	if err != nil {
		return nil, err
	}
	if len(windows) != 1 || windows[0].Begin != beginDate || windows[0].End != endDate {
		return nil, fmt.Errorf("%s date range %s..%s exceeds maximum %d days", endpoint.Name, beginDate, endDate, endpoint.MaxSpanDays)
	}

	requestBody, err := json.Marshal(map[string]string{
		"begin_date": beginDate,
		"end_date":   endDate,
	})
	if err != nil {
		return nil, err
	}

	token, err := c.GetToken()
	if err != nil {
		return &RawAPIResponse{Endpoint: endpoint.Name, RequestBody: requestBody}, err
	}
	response, callErr := c.callOnce(ctx, endpoint.Name, token, requestBody)
	if callErr == nil || response == nil || !isTokenError(response.ErrCode) {
		return response, callErr
	}

	token, err = c.RefreshToken()
	if err != nil {
		return response, err
	}
	return c.callOnce(ctx, endpoint.Name, token, requestBody)
}

func (c *DataCubeClient) callOnce(ctx context.Context, endpoint, token string, requestBody []byte) (*RawAPIResponse, error) {
	if c.BeforeCall != nil {
		if err := c.BeforeCall("datacube_" + endpoint); err != nil {
			return &RawAPIResponse{Endpoint: endpoint, RequestBody: requestBody}, err
		}
	}

	baseURL := strings.TrimRight(c.BaseURL, "/")
	fullURL := fmt.Sprintf("%s/%s?access_token=%s", baseURL, endpoint, url.QueryEscape(token))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(requestBody))
	if err != nil {
		return &RawAPIResponse{Endpoint: endpoint, RequestBody: requestBody}, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := c.HTTPClient
	if client == nil {
		client = httpClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return &RawAPIResponse{Endpoint: endpoint, RequestBody: requestBody}, fmt.Errorf("call %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	result := &RawAPIResponse{
		Endpoint:    endpoint,
		RequestBody: requestBody,
		Body:        body,
		HTTPStatus:  resp.StatusCode,
	}
	if readErr != nil {
		return result, fmt.Errorf("read %s response: %w", endpoint, readErr)
	}

	var envelope struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &envelope)
	}
	result.ErrCode = envelope.ErrCode
	result.ErrMsg = envelope.ErrMsg

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if len(message) > 300 {
			message = message[:300]
		}
		return result, &APIError{Endpoint: endpoint, HTTPStatus: resp.StatusCode, ErrMsg: message}
	}
	if !json.Valid(body) {
		return result, fmt.Errorf("%s returned invalid JSON", endpoint)
	}
	if envelope.ErrCode != 0 {
		return result, &APIError{Endpoint: endpoint, HTTPStatus: resp.StatusCode, ErrCode: envelope.ErrCode, ErrMsg: envelope.ErrMsg}
	}
	return result, nil
}

func isTokenError(code int) bool {
	return code == 40001 || code == 40014 || code == 42001
}
