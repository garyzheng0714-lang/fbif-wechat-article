package autolayout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
)

type Article struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	SourceName  string `json:"source_name"`
	Author      string `json:"author"`
	ContentHTML string `json:"content_html"`
	CoverURL    string `json:"cover_url"`
}

type Receipt struct {
	JobID    int64
	Stage    string
	Existing bool
}

type API interface {
	SubmitOfficial(ctx context.Context, article Article) (Receipt, error)
}

type HTTPAPI struct {
	Endpoint      string
	AdminPassword string
	Client        *http.Client
}

func (c *HTTPAPI) SubmitOfficial(ctx context.Context, article Article) (Receipt, error) {
	body, err := json.Marshal(article)
	if err != nil {
		return Receipt{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Receipt{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Password", c.AdminPassword)
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Receipt{}, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Receipt{}, err
	}
	var response struct {
		Job struct {
			ID    int64  `json:"id"`
			Stage string `json:"stage"`
		} `json:"job"`
		Existing bool   `json:"existing"`
		Error    string `json:"error"`
	}
	_ = json.Unmarshal(responseBody, &response)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		message := strings.TrimSpace(response.Error)
		if message == "" {
			message = strings.TrimSpace(string(responseBody))
		}
		return Receipt{}, fmt.Errorf("layout official-sync HTTP %d: %s", resp.StatusCode, message)
	}
	if response.Job.ID <= 0 {
		return Receipt{}, fmt.Errorf("layout official-sync returned no job id")
	}
	return Receipt{JobID: response.Job.ID, Stage: response.Job.Stage, Existing: response.Existing}, nil
}

type Dispatcher struct {
	Store         *archive.Store
	Client        API
	SourceName    string
	MaxDeliveries int
	Now           func() time.Time
}

type RunResult struct {
	Bootstrapped bool              `json:"bootstrapped"`
	Baselined    int               `json:"baselined"`
	Discovered   int               `json:"discovered"`
	Attempted    int               `json:"attempted"`
	Delivered    int               `json:"delivered"`
	Failed       int               `json:"failed"`
	Errors       map[string]string `json:"errors,omitempty"`
}

type Status struct {
	Enabled bool                      `json:"enabled"`
	Ready   bool                      `json:"ready"`
	Outbox  archive.LayoutOutboxStats `json:"outbox"`
	Reason  string                    `json:"reason,omitempty"`
}

func NewFromEnv(store *archive.Store) (*Dispatcher, error) {
	if !Enabled() {
		return nil, nil
	}
	endpoint := strings.TrimSpace(os.Getenv("LAYOUT_OFFICIAL_SYNC_URL"))
	password := strings.TrimSpace(os.Getenv("LAYOUT_ADMIN_PASSWORD"))
	if endpoint == "" || password == "" {
		return nil, fmt.Errorf("ENABLE_AUTO_LAYOUT=1 requires LAYOUT_OFFICIAL_SYNC_URL and LAYOUT_ADMIN_PASSWORD")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("LAYOUT_OFFICIAL_SYNC_URL is invalid")
	}
	if parsed.Path != "/api/publish/official-sync" {
		return nil, fmt.Errorf("LAYOUT_OFFICIAL_SYNC_URL must end with /api/publish/official-sync")
	}
	sourceName := strings.TrimSpace(os.Getenv("LAYOUT_SOURCE_NAME"))
	if sourceName == "" {
		sourceName = "FBIF食品饮料创新"
	}
	return &Dispatcher{
		Store:         store,
		Client:        &HTTPAPI{Endpoint: endpoint, AdminPassword: password},
		SourceName:    sourceName,
		MaxDeliveries: envPositiveInt("AUTO_LAYOUT_MAX_DELIVERIES_PER_RUN", 20),
	}, nil
}

func Enabled() bool {
	value := strings.TrimSpace(os.Getenv("ENABLE_AUTO_LAYOUT"))
	return value == "1" || strings.EqualFold(value, "true")
}

func (d *Dispatcher) Sync(ctx context.Context) (*RunResult, error) {
	if d == nil || d.Store == nil || d.Client == nil {
		return nil, fmt.Errorf("auto-layout dispatcher is not configured")
	}
	now := d.now()
	result := &RunResult{Errors: make(map[string]string)}
	stats, err := d.Store.LayoutStats(ctx)
	if err != nil {
		return result, err
	}
	var notBefore int64
	if stats.InitializedAt > 0 {
		notBefore = stats.InitializedAt / 1000
	}
	candidates, err := d.candidates(ctx, notBefore)
	if err != nil {
		return result, err
	}
	initialized, baselined, err := d.Store.InitializeLayoutOutbox(ctx, candidates, now)
	if err != nil {
		return result, err
	}
	if initialized {
		result.Bootstrapped = true
		result.Baselined = baselined
		result.Errors = nil
		return result, nil
	}
	result.Discovered, err = d.Store.EnqueueLayoutCandidates(ctx, candidates, now)
	if err != nil {
		return result, err
	}
	limit := d.MaxDeliveries
	if limit <= 0 {
		limit = 20
	}
	due, err := d.Store.ListDueLayoutDeliveries(ctx, now, limit)
	if err != nil {
		return result, err
	}
	var runErrors []error
	for _, delivery := range due {
		result.Attempted++
		receipt, callErr := d.Client.SubmitOfficial(ctx, Article{
			URL:         delivery.SourceURL,
			Title:       delivery.Title,
			SourceName:  delivery.SourceName,
			Author:      delivery.Author,
			ContentHTML: delivery.ContentHTML,
			CoverURL:    delivery.CoverURL,
		})
		attemptedAt := d.now()
		if callErr != nil {
			result.Failed++
			result.Errors[delivery.SourceKey] = callErr.Error()
			runErrors = append(runErrors, callErr)
			if persistErr := d.Store.MarkLayoutFailed(ctx, delivery.SourceKey, callErr.Error(), attemptedAt.Add(15*time.Minute), attemptedAt); persistErr != nil {
				runErrors = append(runErrors, persistErr)
			}
			continue
		}
		if err := d.Store.MarkLayoutDelivered(ctx, delivery.SourceKey, receipt.JobID, receipt.Stage, receipt.Existing, attemptedAt); err != nil {
			runErrors = append(runErrors, err)
			continue
		}
		result.Delivered++
	}
	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, errors.Join(runErrors...)
}

func (d *Dispatcher) Status(ctx context.Context) (*Status, error) {
	stats, err := d.Store.LayoutStats(ctx)
	if err != nil {
		return nil, err
	}
	status := &Status{Enabled: true, Outbox: stats}
	status.Ready = stats.InitializedAt > 0 && stats.Failed == 0
	if stats.InitializedAt == 0 {
		status.Reason = "自动排版尚未完成首次历史基线初始化"
	} else if stats.Failed > 0 {
		status.Reason = "存在自动排版投递失败，服务会按持久化 outbox 重试"
	}
	return status, nil
}

func (d *Dispatcher) candidates(ctx context.Context, notBefore int64) ([]archive.LayoutCandidate, error) {
	candidates, err := d.Store.ListOfficialPublishedArticles(ctx)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]archive.LayoutCandidate, len(candidates))
	for _, candidate := range candidates {
		// freepublish 历史分页会在启用后继续回填；以官方 update_time 而不是
		// first_seen_at 判断新旧，杜绝把刚补入数据库的旧文章当成新文章排版。
		if notBefore > 0 && (candidate.PublishedAt == 0 || candidate.PublishedAt < notBefore) {
			continue
		}
		key, err := CanonicalSourceKey(candidate.SourceURL)
		if err != nil {
			continue
		}
		candidate.SourceKey = key
		candidate.SourceName = d.SourceName
		byKey[key] = candidate
	}
	result := make([]archive.LayoutCandidate, 0, len(byKey))
	for _, candidate := range byKey {
		result = append(result, candidate)
	}
	return result, nil
}

// CanonicalSourceKey 与排版服务 normalizeSiteSourceKey 保持同一身份算法：长链只保留
// __biz/mid/idx，短链去 query，避免 sn/chksm/分享参数变化造成重复建稿。
func CanonicalSourceKey(rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme")
	}
	host := strings.ToLower(u.Hostname())
	if host != "mp.weixin.qq.com" || u.User != nil || u.Port() != "" {
		return "", fmt.Errorf("not a WeChat article URL")
	}
	u.Scheme = "https"
	u.Host = host
	u.Fragment = ""
	path := strings.TrimRight(u.Path, "/")
	if strings.HasPrefix(path, "/s/") && len(path) > len("/s/") && !strings.Contains(strings.TrimPrefix(path, "/s/"), "/") {
		u.Path = path
		u.RawPath = ""
		u.RawQuery = ""
		return "site:" + u.String(), nil
	}
	u.Path = path
	u.RawPath = ""
	if u.Path != "/s" {
		return "", fmt.Errorf("not a WeChat article path")
	}
	query := u.Query()
	kept := url.Values{}
	for _, key := range []string{"__biz", "mid", "idx"} {
		value := strings.TrimSpace(query.Get(key))
		if value == "" {
			return "", fmt.Errorf("missing WeChat article identity %s", key)
		}
		kept.Set(key, value)
	}
	u.RawQuery = kept.Encode()
	return "site:" + u.String(), nil
}

func (d *Dispatcher) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func envPositiveInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
