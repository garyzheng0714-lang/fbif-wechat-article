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
)

const officialAPIBase = "https://api.weixin.qq.com"

type ContentEndpoint struct {
	Name           string   `json:"name"`
	Method         string   `json:"method"`
	Path           string   `json:"path"`
	Category       string   `json:"category"`
	CollectionMode string   `json:"collectionMode"`
	RequiredFields []string `json:"requiredFields,omitempty"`
	Documentation  string   `json:"documentation"`
}

var contentEndpoints = []ContentEndpoint{
	{Name: "draft_count", Method: http.MethodGet, Path: "/cgi-bin/draft/count", Category: "draft", CollectionMode: "automatic", Documentation: "https://developers.weixin.qq.com/doc/subscription/api/draftbox/draftmanage/api_draft_count.html"},
	{Name: "draft_batchget", Method: http.MethodPost, Path: "/cgi-bin/draft/batchget", Category: "draft", CollectionMode: "automatic", Documentation: "https://developers.weixin.qq.com/doc/subscription/api/draftbox/draftmanage/api_draft_batchget.html"},
	{Name: "draft_get", Method: http.MethodPost, Path: "/cgi-bin/draft/get", Category: "draft", CollectionMode: "automatic", RequiredFields: []string{"media_id"}, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/draftbox/draftmanage/api_getdraft.html"},
	{Name: "material_get_materialcount", Method: http.MethodGet, Path: "/cgi-bin/material/get_materialcount", Category: "material", CollectionMode: "automatic", Documentation: "https://developers.weixin.qq.com/doc/subscription/api/material/permanent/api_getmaterialcount.html"},
	{Name: "material_batchget_material", Method: http.MethodPost, Path: "/cgi-bin/material/batchget_material", Category: "material", CollectionMode: "automatic", Documentation: "https://developers.weixin.qq.com/doc/subscription/api/material/permanent/api_batchgetmaterial.html"},
	{Name: "material_get_material", Method: http.MethodPost, Path: "/cgi-bin/material/get_material", Category: "material", CollectionMode: "automatic", RequiredFields: []string{"media_id"}, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/material/permanent/api_getmaterial.html"},
	{Name: "freepublish_batchget", Method: http.MethodPost, Path: "/cgi-bin/freepublish/batchget", Category: "publish", CollectionMode: "automatic", Documentation: "https://developers.weixin.qq.com/doc/subscription/api/public/api_freepublish_batchget.html"},
	{Name: "freepublish_get", Method: http.MethodPost, Path: "/cgi-bin/freepublish/get", Category: "publish", CollectionMode: "identifier_required", RequiredFields: []string{"publish_id"}, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/public/api_freepublish_get.html"},
	{Name: "freepublish_getarticle", Method: http.MethodPost, Path: "/cgi-bin/freepublish/getarticle", Category: "publish", CollectionMode: "automatic", RequiredFields: []string{"article_id"}, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/public/api_freepublishgetarticle.html"},
	{Name: "message_mass_get", Method: http.MethodPost, Path: "/cgi-bin/message/mass/get", Category: "publish", CollectionMode: "identifier_required", RequiredFields: []string{"msg_id"}, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/notify/message/api_massmsgget.html"},
	{Name: "comment_list", Method: http.MethodPost, Path: "/cgi-bin/comment/list", Category: "comment", CollectionMode: "automatic", RequiredFields: []string{"msg_data_id", "index"}, Documentation: "https://developers.weixin.qq.com/doc/subscription/api/leaving/api_listcomment.html"},
}

func AllContentEndpoints() []ContentEndpoint {
	result := make([]ContentEndpoint, len(contentEndpoints))
	copy(result, contentEndpoints)
	return result
}

func ContentEndpointByName(name string) (ContentEndpoint, bool) {
	for _, endpoint := range contentEndpoints {
		if endpoint.Name == name {
			return endpoint, true
		}
	}
	return ContentEndpoint{}, false
}

type ContentClient struct {
	BaseURL      string
	HTTPClient   *http.Client
	GetToken     func() (string, error)
	RefreshToken func() (string, error)
	BeforeCall   func(endpoint string) error
}

func NewContentClient() *ContentClient {
	return &ContentClient{
		BaseURL:      officialAPIBase,
		HTTPClient:   httpClient,
		GetToken:     GetToken,
		RefreshToken: RefreshTokenNow,
		BeforeCall:   checkAndIncrementQuota,
	}
}

// Call invokes one whitelisted official content endpoint. It deliberately
// returns raw bytes because material/get_material may return binary media.
func (c *ContentClient) Call(ctx context.Context, endpoint ContentEndpoint, payload interface{}) (*RawAPIResponse, error) {
	requestBody := []byte(nil)
	var err error
	if endpoint.Method == http.MethodPost {
		if payload == nil {
			payload = map[string]interface{}{}
		}
		requestBody, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode %s request: %w", endpoint.Name, err)
		}
	}
	token, err := c.GetToken()
	if err != nil {
		return &RawAPIResponse{Endpoint: endpoint.Name, RequestBody: requestBody}, err
	}
	response, callErr := c.callOnce(ctx, endpoint, token, requestBody)
	if callErr == nil || response == nil || !isTokenError(response.ErrCode) {
		return response, callErr
	}
	token, err = c.RefreshToken()
	if err != nil {
		return response, err
	}
	return c.callOnce(ctx, endpoint, token, requestBody)
}

func (c *ContentClient) callOnce(ctx context.Context, endpoint ContentEndpoint, token string, requestBody []byte) (*RawAPIResponse, error) {
	if c.BeforeCall != nil {
		if err := c.BeforeCall(endpoint.Name); err != nil {
			return &RawAPIResponse{Endpoint: endpoint.Name, RequestBody: requestBody}, err
		}
	}
	fullURL := strings.TrimRight(c.BaseURL, "/") + endpoint.Path + "?access_token=" + url.QueryEscape(token)
	var bodyReader io.Reader
	if endpoint.Method == http.MethodPost {
		bodyReader = bytes.NewReader(requestBody)
	}
	req, err := http.NewRequestWithContext(ctx, endpoint.Method, fullURL, bodyReader)
	if err != nil {
		return &RawAPIResponse{Endpoint: endpoint.Name, RequestBody: requestBody}, err
	}
	if endpoint.Method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTPClient
	if client == nil {
		client = httpClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return &RawAPIResponse{Endpoint: endpoint.Name, RequestBody: requestBody}, fmt.Errorf("call %s: %w", endpoint.Name, err)
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(resp.Body)
	result := &RawAPIResponse{
		Endpoint:    endpoint.Name,
		RequestBody: requestBody,
		Body:        responseBody,
		HTTPStatus:  resp.StatusCode,
	}
	if readErr != nil {
		return result, fmt.Errorf("read %s response: %w", endpoint.Name, readErr)
	}

	if json.Valid(responseBody) {
		var envelope struct {
			ErrCode int    `json:"errcode"`
			ErrMsg  string `json:"errmsg"`
		}
		_ = json.Unmarshal(responseBody, &envelope)
		result.ErrCode = envelope.ErrCode
		result.ErrMsg = envelope.ErrMsg
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(responseBody))
		if len(message) > 300 {
			message = message[:300]
		}
		return result, &APIError{Endpoint: endpoint.Name, HTTPStatus: resp.StatusCode, ErrMsg: message}
	}
	if result.ErrCode != 0 {
		return result, &APIError{Endpoint: endpoint.Name, HTTPStatus: resp.StatusCode, ErrCode: result.ErrCode, ErrMsg: result.ErrMsg}
	}
	return result, nil
}
