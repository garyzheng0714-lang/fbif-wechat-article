// Package schedule 读 FBIF 中台下发的轮询和定时时间。人在总账本「定时任务和回调」改「多久一次」，
// 中台写进 /etc/fbif-hub/env/wechat-sync.env（FBIF-One-Api docs/hub/轮询和定时任务.md）。
// 时段、时刻都按北京时间算。变量没有或看不懂时用默认值，进程照常起来。
package schedule

import (
	"fmt"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

// 总账本里这个程序的三行，各自对应的变量。
const (
	collectionTimesVar   = "ANALYTICS_TIMES"                     // reczz28NbDlb780a 公众号数据抓取
	layoutPollWindowVar  = "AUTO_LAYOUT_POLL_WINDOW"             // reczz28NbDlb8bqz 公众号发文轮询
	layoutPollSecondsVar = "AUTO_LAYOUT_POLL_SECONDS"            // reczz28NbDlb8bqz 公众号发文轮询
	baseSyncFallbackVar  = "OFFICIAL_BASE_SYNC_FALLBACK_SECONDS" // reczz28UubKiJIss 文章数据写回兜底
)

type Plan struct {
	Collection       Daily
	LayoutPoll       Every
	BaseSyncFallback time.Duration
}

// Default 是变量还没下发时的时间，和接入中台前一模一样。
var Default = Plan{
	Collection: Daily{8*60 + 5},
	// 时段结尾那一刻不跑；结尾写 18:45，最后一次才是接入前的 18:30。
	LayoutPoll:       Every{Window: Window{From: 8*60 + 30, To: 18*60 + 45}, Interval: 15 * time.Minute},
	BaseSyncFallback: 6 * time.Hour,
}

func FromEnv() Plan {
	return Plan{
		Collection: read(collectionTimesVar, Default.Collection, parseDaily),
		LayoutPoll: Every{
			Window:   read(layoutPollWindowVar, Default.LayoutPoll.Window, parseWindow),
			Interval: read(layoutPollSecondsVar, Default.LayoutPoll.Interval, parseSeconds),
		},
		BaseSyncFallback: read(baseSyncFallbackVar, Default.BaseSyncFallback, parseSeconds),
	}
}

func (p Plan) String() string {
	return fmt.Sprintf("%s=%s %s=%s %s=%d %s=%d",
		collectionTimesVar, p.Collection,
		layoutPollWindowVar, p.LayoutPoll.Window,
		layoutPollSecondsVar, int(p.LayoutPoll.Interval/time.Second),
		baseSyncFallbackVar, int(p.BaseSyncFallback/time.Second))
}

func read[T any](name string, fallback T, parse func(string) (T, bool)) T {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, ok := parse(raw)
	if !ok {
		log.Printf("[Schedule] %s 看不懂，用默认值", name)
		return fallback
	}
	return value
}

// Clock 是一天里的第几分钟；1440 只用作时段结尾（24:00）。
type Clock int

func (c Clock) String() string { return fmt.Sprintf("%02d:%02d", c/60, c%60) }

func (c Clock) on(day time.Time) time.Time { return day.Add(time.Duration(c) * time.Minute) }

// Window 是每天 From 起、到 To 之前；To 比 From 早就跨零点，00:00-24:00 是全天。
type Window struct{ From, To Clock }

func (w Window) String() string { return w.From.String() + "-" + w.To.String() }

func (w Window) length() time.Duration {
	minutes := (w.To - w.From + 1440) % 1440
	if minutes == 0 {
		minutes = 1440
	}
	return time.Duration(minutes) * time.Minute
}

// Every 是时段里从 From 起每隔 Interval 一次、到 To 之前为止，和中台给 timer 排的一样。
type Every struct {
	Window   Window
	Interval time.Duration
}

// Next 是 now 之后的下一次。
func (e Every) Next(now time.Time) time.Time {
	today := midnight(now)
	for _, start := range e.starts(today) {
		if now.Before(start) {
			return start
		}
		next := start.Add((now.Sub(start)/e.Interval + 1) * e.Interval)
		if next.Before(start.Add(e.Window.length())) {
			return next
		}
	}
	return e.Window.From.on(today.AddDate(0, 0, 1))
}

// Covers 说 now 是否在某一段的第一次和最后一次之间，两头都算。
func (e Every) Covers(now time.Time) bool {
	for _, start := range e.starts(midnight(now)) {
		last := start.Add((e.Window.length() - 1) / e.Interval * e.Interval)
		if !now.Before(start) && !now.After(last) {
			return true
		}
	}
	return false
}

// starts 是可能还没结束的两段的开头：昨天开始、跨零点到今天的那段，和今天的那段。
func (e Every) starts(today time.Time) [2]time.Time {
	return [2]time.Time{e.Window.From.on(today.AddDate(0, 0, -1)), e.Window.From.on(today)}
}

// Daily 是每天这几个时刻，从早到晚，至少一个。
type Daily []Clock

func (d Daily) String() string {
	parts := make([]string, len(d))
	for i, c := range d {
		parts[i] = c.String()
	}
	return strings.Join(parts, ",")
}

// Next 是 now 之后的下一个时刻。
func (d Daily) Next(now time.Time) time.Time {
	today := midnight(now)
	for _, c := range d {
		if at := c.on(today); at.After(now) {
			return at
		}
	}
	return d[0].on(today.AddDate(0, 0, 1))
}

// Last 是 now 当时或之前最近的一个时刻；今天第一个时刻还没到就是昨天最后一个。
func (d Daily) Last(now time.Time) time.Time {
	today := midnight(now)
	for i := len(d) - 1; i >= 0; i-- {
		if at := d[i].on(today); !at.After(now) {
			return at
		}
	}
	return d[len(d)-1].on(today.AddDate(0, 0, -1))
}

// Begun 说今天第一个时刻到了没有。
func (d Daily) Begun(now time.Time) bool { return !now.Before(d[0].on(midnight(now))) }

func midnight(t time.Time) time.Time {
	local := t.In(wechat.ShanghaiLoc())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
}

func parseSeconds(raw string) (time.Duration, bool) {
	if !digits(raw) {
		return 0, false
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 60 || seconds > 30*86400 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

func parseWindow(raw string) (Window, bool) {
	from, to, ok := strings.Cut(raw, "-")
	if !ok {
		return Window{}, false
	}
	start, okFrom := parseClock(from, false)
	end, okTo := parseClock(to, true)
	if !okFrom || !okTo || start == end {
		return Window{}, false
	}
	return Window{From: start, To: end}, true
}

func parseDaily(raw string) (Daily, bool) {
	var times Daily
	for _, part := range strings.Split(raw, ",") {
		c, ok := parseClock(part, false)
		if !ok {
			return nil, false
		}
		times = append(times, c)
	}
	slices.Sort(times)
	return slices.Compact(times), true
}

func parseClock(raw string, end bool) (Clock, bool) {
	h, m, ok := strings.Cut(strings.TrimSpace(raw), ":")
	if !ok || len(h) < 1 || len(h) > 2 || len(m) != 2 || !digits(h) || !digits(m) {
		return 0, false
	}
	hour, _ := strconv.Atoi(h)
	minute, _ := strconv.Atoi(m)
	if minute > 59 || hour > 24 || (hour == 24 && (minute != 0 || !end)) {
		return 0, false
	}
	return Clock(hour*60 + minute), true
}

func digits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}
