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

// Article 是投递给排版服务的一篇文章。**只有链接**：排版服务只接收链接（其侧红线，
// 2026-08-06），正文、标题、来源、作者与封面一律由它自己从原文现抓。
//
// 为什么不能把我们手里的官方正文传过去（我们明明有）：排版服务的正文解析有两条路，
// 自抓那条会按账号规则包取参数，收外部正文那条不会；而我们手里的是官方 API 的 HTML，
// 与网页版结构未必等价。用户报告的排版服务任务 #72 崩坏（长文零小标题、正文段落被
// 排成灰色图注）即出自后者。
//
// 保留的三个字段不是内容，是**类型证据**：官方 freepublish 已确认 article_type=news
// （普通图文），排版服务据此 fail closed，防止小绿书/图集被误投上站。
type Article struct {
	URL               string `json:"url"`
	ContentKind       string `json:"content_kind"`
	Classification    string `json:"classification"`
	ClassifierVersion string `json:"classifier_version"`
}

const (
	// 官方 freepublish article_type=news 通过 candidates() 过滤后的正向类型证据。
	layoutContentKind    = "article"
	layoutClassification = "ordinary_confirmed"
	// 分类器版本：语义变化时递增，便于排版服务侧追溯是哪一版判定放行的。
	layoutClassifierVersion = "official-freepublish-news-v1"
)

type Receipt struct {
	JobID    int64
	Stage    string
	Existing bool
}

type API interface {
	SubmitOfficial(ctx context.Context, article Article) (Receipt, error)
}

type URLAPI interface {
	SubmitURL(ctx context.Context, article Article) (Receipt, error)
}

// HTTPAPI 排版服务客户端。
//
// 鉴权走 X-Publish-Sync-Token（排版服务的机器提交通道）。历史上这里发的是
// X-Admin-Password，但排版服务 2026-08-04 把密码鉴权换成会话角色后就不再认它，
// 而客户端没跟着改——这条投递链路自那时起对端恒返回 401。
type HTTPAPI struct {
	Endpoint  string
	SyncToken string
	Client    *http.Client
}

const publishSyncTokenHeader = "X-Publish-Sync-Token"

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
	req.Header.Set(publishSyncTokenHeader, c.SyncToken)
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

// SubmitURL 把一篇已发布文章的链接投给排版服务的 site-sync 入口。
// 与 SubmitOfficial 的差别只在证据来源（群发回调侧的页面分类器 vs freepublish
// article_type），载荷同样只有链接加类型证据——两个入口都不接收正文。
func (c *HTTPAPI) SubmitURL(ctx context.Context, article Article) (Receipt, error) {
	if _, err := CanonicalSourceKey(article.URL); err != nil {
		return Receipt{}, err
	}
	endpoint, err := url.Parse(c.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return Receipt{}, fmt.Errorf("layout official-sync endpoint is invalid")
	}
	endpoint.Path = "/api/publish/site-sync"
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	body, err := json.Marshal(article)
	if err != nil {
		return Receipt{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return Receipt{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(publishSyncTokenHeader, c.SyncToken)
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
		return Receipt{}, fmt.Errorf("layout site-sync HTTP %d: %s", resp.StatusCode, message)
	}
	if response.Job.ID <= 0 {
		return Receipt{}, fmt.Errorf("layout site-sync returned no job id")
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
	Bootstrapped     bool              `json:"bootstrapped"`
	Baselined        int               `json:"baselined"`
	Discovered       int               `json:"discovered"`
	SkippedNewspic   int               `json:"skippedNewspic"`
	HeldUnclassified int               `json:"heldUnclassified"`
	Attempted        int               `json:"attempted"`
	Delivered        int               `json:"delivered"`
	Failed           int               `json:"failed"`
	Errors           map[string]string `json:"errors,omitempty"`
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
	// 排版服务 2026-08-04 起用 X-Publish-Sync-Token 认机器提交；旧的
	// LAYOUT_ADMIN_PASSWORD 已无对端支持，缺配置必须 fail loud 而不是继续 401 空转。
	syncToken := strings.TrimSpace(os.Getenv("LAYOUT_SYNC_TOKEN"))
	if endpoint == "" || syncToken == "" {
		return nil, fmt.Errorf("ENABLE_AUTO_LAYOUT=1 requires LAYOUT_OFFICIAL_SYNC_URL and LAYOUT_SYNC_TOKEN")
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
		Client:        &HTTPAPI{Endpoint: endpoint, SyncToken: syncToken},
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
	candidates, skippedNewspic, heldUnclassified, err := d.candidates(ctx, notBefore)
	if err != nil {
		return result, err
	}
	result.SkippedNewspic = skippedNewspic
	result.HeldUnclassified = heldUnclassified
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
		// 只投链接：正文/标题/作者/封面由排版服务从原文现抓（见 Article 头注释）。
		// candidates() 已把 article_type 过滤到 news，因此这里给出正向类型证据。
		receipt, callErr := d.Client.SubmitOfficial(ctx, Article{
			URL:               delivery.SourceURL,
			ContentKind:       layoutContentKind,
			Classification:    layoutClassification,
			ClassifierVersion: layoutClassifierVersion,
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
	status.Ready = stats.InitializedAt > 0 && stats.Failed == 0 && stats.HeldUnclassified == 0
	if stats.InitializedAt == 0 {
		status.Reason = "自动排版尚未完成首次历史基线初始化"
	} else if stats.Failed > 0 {
		status.Reason = "存在自动排版投递失败，服务会按持久化 outbox 重试"
	} else if stats.HeldUnclassified > 0 {
		status.Reason = "存在无法由官方草稿类型确认的已发布内容；为防止小绿书误投，已暂停自动排版"
	}
	return status, nil
}

func (d *Dispatcher) SubmitURL(ctx context.Context, article Article) (Receipt, error) {
	if d == nil || d.Client == nil {
		return Receipt{}, fmt.Errorf("auto-layout dispatcher is not configured")
	}
	client, ok := d.Client.(URLAPI)
	if !ok {
		return Receipt{}, fmt.Errorf("auto-layout client does not support URL import")
	}
	return client.SubmitURL(ctx, article)
}

func (d *Dispatcher) candidates(ctx context.Context, notBefore int64) ([]archive.LayoutCandidate, int, int, error) {
	candidates, err := d.Store.ListOfficialPublishedArticles(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	byKey := make(map[string]archive.LayoutCandidate, len(candidates))
	skippedNewspic := 0
	heldUnclassified := 0
	for _, candidate := range candidates {
		// freepublish 历史分页会在启用后继续回填；以官方 update_time 而不是
		// first_seen_at 判断新旧，杜绝把刚补入数据库的旧文章当成新文章排版。
		if notBefore > 0 && (candidate.PublishedAt == 0 || candidate.PublishedAt < notBefore) {
			continue
		}
		switch candidate.ArticleType {
		case "news":
			// Only the official ordinary-article type is eligible for website layout.
		case "newspic":
			skippedNewspic++
			continue
		default:
			heldUnclassified++
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
	return result, skippedNewspic, heldUnclassified, nil
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
