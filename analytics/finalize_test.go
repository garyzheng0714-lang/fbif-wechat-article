package analytics

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

// totalDetailClient 像微信 getarticletotaldetail：查某个发表日，回这天发的一篇文章，
// 分日数据给到「发表日+29」和「昨天」里早的那天；stuck 里的发表日只给到固定那天（比如文章删了）。
type totalDetailClient struct {
	now   time.Time
	stuck map[string]string
	calls []string
}

func (f *totalDetailClient) Call(_ context.Context, endpoint wechat.DataCubeEndpoint, beginDate, endDate string) (*wechat.RawAPIResponse, error) {
	f.calls = append(f.calls, beginDate)
	yesterday := startOfDay(f.now).AddDate(0, 0, -1)
	publish, err := time.ParseInLocation("2006-01-02", beginDate, wechat.ShanghaiLoc())
	if err != nil {
		return nil, err
	}
	last := publish.AddDate(0, 0, 29)
	if last.After(yesterday) {
		last = yesterday
	}
	lastStat := last.Format("2006-01-02")
	if stuck, ok := f.stuck[beginDate]; ok {
		lastStat = stuck
	}
	return &wechat.RawAPIResponse{
		Endpoint:    endpoint.Name,
		RequestBody: []byte(fmt.Sprintf(`{"begin_date":%q,"end_date":%q}`, beginDate, endDate)),
		Body:        totalDetailBody(beginDate, lastStat),
		HTTPStatus:  200,
	}, nil
}

func totalDetailBody(publishDate, lastStat string) []byte {
	return []byte(fmt.Sprintf(`{"list":[{"ref_date":%q,"msgid":"%s_1","title":"t","detail_list":[{"stat_date":%q,"read_user":1}]}]}`,
		publishDate, publishDate, lastStat))
}

// seedTotalDetail 存一次过去的拉取：发表日 publishDate 在 fetchedAt 拉的，分日数据到 lastStat。
func seedTotalDetail(t *testing.T, store *archive.Store, publishDate, lastStat string, fetchedAt time.Time) {
	t.Helper()
	if _, err := store.SaveFetch(context.Background(), archive.FetchRecord{
		Endpoint:     "getarticletotaldetail",
		Category:     "article",
		BeginDate:    publishDate,
		EndDate:      publishDate,
		RequestJSON:  []byte(fmt.Sprintf(`{"begin_date":%q,"end_date":%q,"seed":%q}`, publishDate, publishDate, lastStat)),
		ResponseJSON: totalDetailBody(publishDate, lastStat),
		Success:      true,
		FetchedAt:    fetchedAt,
	}); err != nil {
		t.Fatal(err)
	}
}

func finalizeCollector(t *testing.T, store *archive.Store, client *totalDetailClient, maxCalls int) *Collector {
	t.Helper()
	endpoint, _ := wechat.DataCubeEndpointByName("getarticletotaldetail")
	// 历史回填已经走完，免得回填顺手把老日子又查一遍、掩盖补查这一步。
	if err := store.MarkBackfillComplete(context.Background(), endpoint.Name, endpoint.Category, client.now); err != nil {
		t.Fatal(err)
	}
	return &Collector{
		Client:    client,
		Store:     store,
		Now:       func() time.Time { return client.now },
		MaxCalls:  maxCalls,
		Endpoints: []wechat.DataCubeEndpoint{endpoint},
	}
}

func shanghai(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, wechat.ShanghaiLoc())
}

func countCalls(calls []string, date string) int {
	n := 0
	for _, call := range calls {
		if call == date {
			n++
		}
	}
	return n
}

// 复现 2026-06-16 那次：第 30 天（07-16）零点刚过就去问，微信还没出 07-15 的数，
// 只拿到 07-14；之后这个发表日出了 30 天刷新范围，再也没人查，永远少最后一天。
func TestRunRefetchesPublishDateThatMissedItsFinalDayOnce(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	seedTotalDetail(t, store, "2026-06-16", "2026-07-14", shanghai(2026, 7, 16, 0, 5)) // 少最后一天
	seedTotalDetail(t, store, "2026-06-15", "2026-07-14", shanghai(2026, 7, 15, 8, 5)) // 已到最终值
	seedTotalDetail(t, store, "2026-06-10", "2026-07-01", shanghai(2026, 7, 12, 8, 5)) // 满 31 天后查过一次，微信就给到这儿

	client := &totalDetailClient{now: shanghai(2026, 7, 20, 8, 5), stuck: map[string]string{}}
	collector := finalizeCollector(t, store, client, 100)
	if _, err := collector.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := countCalls(client.calls, "2026-06-16"); got != 1 {
		t.Fatalf("06-16 少最后一天，应补查一次，实际 %d 次；calls=%v", got, client.calls)
	}
	for _, date := range []string{"2026-06-15", "2026-06-10"} {
		if got := countCalls(client.calls, date); got != 0 {
			t.Fatalf("%s 不该再查，实际 %d 次", date, got)
		}
	}

	// 补查拿到了最后一天，第二天不再查。
	client.now = shanghai(2026, 7, 21, 8, 5)
	client.calls = nil
	if _, err := collector.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := countCalls(client.calls, "2026-06-16"); got != 0 {
		t.Fatalf("06-16 补齐后又查了 %d 次", got)
	}
}

// 微信本来就给不了第 30 天的（比如文章删了），补查一次后不天天重查。
func TestRunDoesNotRetryFinalDayEveryDayWhenWechatHasNoMore(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	seedTotalDetail(t, store, "2026-06-16", "2026-07-10", shanghai(2026, 7, 16, 8, 5))

	client := &totalDetailClient{now: shanghai(2026, 7, 20, 8, 5), stuck: map[string]string{"2026-06-16": "2026-07-10"}}
	collector := finalizeCollector(t, store, client, 100)
	for day := 20; day <= 23; day++ {
		client.now = shanghai(2026, 7, day, 8, 5)
		if _, err := collector.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := countCalls(client.calls, "2026-06-16"); got != 1 {
		t.Fatalf("06-16 微信给不了更多，四天里应只补查一次，实际 %d 次", got)
	}
}

// 一轮最多补 10 个发表日，从新到旧。
func TestRunCapsFinalDayRefetchesPerRunNewestFirst(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for day := 1; day <= 12; day++ {
		publish := shanghai(2026, 5, day, 0, 0)
		seedTotalDetail(t, store, publish.Format("2006-01-02"), publish.AddDate(0, 0, 28).Format("2006-01-02"), publish.AddDate(0, 0, 30))
	}
	client := &totalDetailClient{now: shanghai(2026, 7, 20, 8, 5), stuck: map[string]string{}}
	collector := finalizeCollector(t, store, client, 100)
	if _, err := collector.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var refetched []string
	for _, call := range client.calls {
		if call < "2026-06-01" {
			refetched = append(refetched, call)
		}
	}
	want := []string{"2026-05-12", "2026-05-11", "2026-05-10", "2026-05-09", "2026-05-08", "2026-05-07", "2026-05-06", "2026-05-05", "2026-05-04", "2026-05-03"}
	if fmt.Sprint(refetched) != fmt.Sprint(want) {
		t.Fatalf("refetched = %v, want %v", refetched, want)
	}
}
