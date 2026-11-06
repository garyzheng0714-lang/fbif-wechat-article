package analytics

import (
	"context"
	"log"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

// 微信 getarticletotaldetail 每篇只统计发表日起 30 天，最后一天是发表日+29；
// 每天的刷新只管最近 30 个发表日，哪天没拿到最后一天（那天采集失败、或问得太早微信还没出数），
// 出了刷新范围就再没人查，会永远少一截。这里把这种发表日补查一次。
const (
	finalDayEndpoint     = "getarticletotaldetail"
	maxFinalDayRefetches = 10
)

// refetchMissedFinalDays 在刷新最近 30 天之后、历史回填之前跑：
// 已出刷新范围、还有文章没到最后一天的发表日，从新到旧补查，一轮最多 10 个。
// 满 31 天（最后一天的数肯定出了）以后成功查过一次的不再查：微信给不了更多的（比如文章删了）不天天花额度。
func (c *Collector) refetchMissedFinalDays(ctx context.Context, endpoints []wechat.DataCubeEndpoint, yesterday time.Time, maxCalls int, failedEndpoint map[string]bool, result *RunResult, runErrors *[]error) {
	for _, endpoint := range endpoints {
		if endpoint.Name != finalDayEndpoint || endpoint.RefreshDays <= 1 || failedEndpoint[endpoint.Name] {
			continue
		}
		latest := yesterday.AddDate(0, 0, -endpoint.RefreshDays)
		if dateBefore(latest, endpoint.EarliestDate) {
			return
		}
		dates, err := c.Store.DatesShortOfFinalDay(ctx, endpoint.Name, endpoint.EarliestDate, latest.Format("2006-01-02"), endpoint.RefreshDays-1)
		if err != nil {
			recordEndpointIssue(result, failedEndpoint, endpoint.Name, err, runErrors)
			return
		}
		refetched := 0
		for _, date := range dates {
			if refetched >= maxFinalDayRefetches || result.Calls >= maxCalls {
				return
			}
			publish, err := time.ParseInLocation("2006-01-02", date, wechat.ShanghaiLoc())
			if err != nil {
				continue
			}
			tried, err := c.Store.FetchedSuccessfullySince(ctx, endpoint.Name, date, date, publish.AddDate(0, 0, endpoint.RefreshDays+1))
			if err != nil {
				recordEndpointIssue(result, failedEndpoint, endpoint.Name, err, runErrors)
				return
			}
			if tried {
				continue
			}
			refetched++
			log.Printf("[OfficialAnalytics] %s 发表日 %s 没采到第 %d 天，补查一次", endpoint.Name, date, endpoint.RefreshDays)
			err = c.collectWindow(ctx, endpoint, date, date, "")
			result.Calls++
			if err != nil {
				recordEndpointIssue(result, failedEndpoint, endpoint.Name, err, runErrors)
				return
			}
			result.Succeeded++
		}
	}
}
