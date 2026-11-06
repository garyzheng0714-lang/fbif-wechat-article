package bizdata

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
)

// Worker 不定时轮询：归档写完由 analytics.Runtime 叫醒，合并 5 秒后写一轮表；
// 没有要写的行时一次飞书接口都不调。
// 兜底间隔和 Base 写回共用总账本那一行（OFFICIAL_BASE_SYNC_FALLBACK_SECONDS）。
type Worker struct {
	Store          *archive.Store
	Base           BaseTarget
	BaseToken      string
	ArticleTableID string
	// Alert 只在连续失败到第 3 次那一刻调一次。
	Alert func(message string)
	Now   func() time.Time
	// FetchPage 读公开文章页，测试里换掉；FetchGap 是两次读网页的间隔，默认 6 秒。
	FetchPage func(ctx context.Context, link string) (string, error)
	FetchGap  *time.Duration

	runMu    sync.Mutex
	wake     chan struct{}
	failures int

	authorWake     chan struct{}
	authorFailures int
	authorMisses   map[string]int // record_id → 连着打不开几次
}

type RunResult struct {
	Base *BaseResult `json:"base,omitempty"`
}

// NewFromEnv 读中台下发的 BIZ_BASE_APP_TOKEN、BIZ_BASE_ARTICLE_TABLE_ID；缺一个就返回 nil，什么也不跑。
func NewFromEnv(store *archive.Store) *Worker {
	baseToken := strings.TrimSpace(os.Getenv("BIZ_BASE_APP_TOKEN"))
	articleTable := strings.TrimSpace(os.Getenv("BIZ_BASE_ARTICLE_TABLE_ID"))
	if baseToken == "" || articleTable == "" {
		if baseToken != "" || articleTable != "" {
			log.Printf("[BizData] BIZ_BASE_APP_TOKEN and BIZ_BASE_ARTICLE_TABLE_ID must both be set; table writes disabled")
		}
		return nil
	}
	return &Worker{
		Store:          store,
		Base:           feishu.Base{AppToken: baseToken},
		BaseToken:      baseToken,
		ArticleTableID: articleTable,
		wake:           make(chan struct{}, 1),
		authorWake:     make(chan struct{}, 1),
	}
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *Worker) Wake() {
	if w == nil || w.wake == nil {
		return
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run 写一轮表。
func (w *Worker) Run(ctx context.Context) (*RunResult, error) {
	if !w.runMu.TryLock() {
		return nil, fmt.Errorf("business data round is already running")
	}
	defer w.runMu.Unlock()
	base, err := w.syncBase(ctx)
	if base != nil && base.Articles.Created+base.Articles.Adopted > 0 {
		w.WakeAuthors() // 有新的文章行，去补作者
	}
	return &RunResult{Base: base}, err
}

const (
	wakeDebounce = 5 * time.Second
	firstRetry   = time.Minute
)

func Start(stopCh <-chan struct{}, worker *Worker, fallback time.Duration) {
	if worker == nil {
		return
	}
	go worker.loop(stopCh, 20*time.Second, fallback)
	go worker.authorLoop(stopCh, 2*time.Minute, fallback)
	log.Printf("[BizData] worker started: on archive change, fallback every %s; 作者 filled from article pages on new rows", fallback)
}

func (w *Worker) loop(stopCh <-chan struct{}, initialDelay, fallback time.Duration) {
	timer := time.NewTimer(initialDelay)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
		case <-w.wake:
			select {
			case <-time.After(wakeDebounce):
			case <-stopCh:
				return
			}
			select {
			case <-w.wake:
			default:
			}
		case <-stopCh:
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(w.round(fallback))
	}
}

// round 跑一轮并返回下一轮最晚什么时候跑：成功走兜底间隔；失败从 1 分钟起翻倍退避，
// 最长到兜底间隔；连续失败第 3 次找人一次。
func (w *Worker) round(fallback time.Duration) time.Duration {
	result, err := w.Run(context.Background())
	if result != nil {
		logResult(result)
	}
	if err == nil {
		w.failures = 0
		return fallback
	}
	w.failures++
	log.Printf("[BizData] round failed (%d in a row): %v", w.failures, err)
	if w.failures == 3 && w.Alert != nil {
		w.Alert(fmt.Sprintf("公众号业务数据表格连续 3 次失败：%v", err))
	}
	backoff := firstRetry << (w.failures - 1)
	if backoff > fallback || backoff <= 0 {
		backoff = fallback
	}
	return backoff
}

func logResult(result *RunResult) {
	if result.Base != nil {
		a, d := result.Base.Articles, result.Base.Days
		log.Printf("[BizData] articles created=%d updated=%d deleted_in_base=%d adopted=%d; days created=%d updated=%d", a.Created, a.Updated, a.Skipped, a.Adopted, d.Created, d.Updated)
	}
}
