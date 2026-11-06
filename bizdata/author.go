package bizdata

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
)

// 「作者」列（用户 2026-11-06 定）：程序只填空着的格子（导入时占位的全角空格也算空），人填的不碰；
// 看过的行按 record_id 记在 official_biz_authors，之后不再碰，所以程序填过、人又清空的也不会填回去。
// 依据按顺序：
//  1. 标题认得出的栏目：创新周报、短视频、百强榜、FBIF 活动；
//  2. 转载文章（网页 copyright_stat=2）写原公众号名，照网页原样（用户定：统一用公众号原名）；
//     转载卡片上的名字是转载那天的，号改过名就会有两种写法，所以按网页里的公众号编号（source_username）认号，
//     同一个号沿用表里已经写过的名字，一个号只有一种写法；
//  3. 没声明原创的写正文里的「来源：」；
//  4. 正文开头或结尾的「作者：/撰文：/文：」，没有再看「编辑：」，署名按人工习惯归类（bylineLabels）；
//  5. 都没有就留空。
//
// 网页是公开文章页 mp.weixin.qq.com/s?…，不走微信接口、不占接口额度；每 6 秒最多读一篇。
const (
	authorColumn   = "作者"
	authorFetchGap = 6 * time.Second
	authorBatch    = 20
	// 同一行连着几次打不开就不再试（多半是文章没了），留空。
	authorMaxMisses = 3
	// 一轮里连着几篇打不开就先停下，等退避后再来（多半是被微信拦了或断网）。
	authorMaxFailuresInARow = 3
)

var fbifEventTitle = regexp.MustCompile(`(?i)确认参加|确认参展|参展|议程|开幕|倒计时|FBIF ?20\d\d|食品创新展|高层确认|等企业|wow ?food|wow ?食品|分享实录`)

// authorFromTitle 认标题里的固定栏目，和表里人工的填法一致。
func authorFromTitle(title string) string {
	switch {
	case strings.Contains(title, "创新周报"):
		return "创新周报"
	case strings.Contains(title, "短视频"):
		return "短视频"
	case strings.Contains(title, "百强榜"):
		return "百强榜"
	case fbifEventTitle.MatchString(title):
		return "FBIF活动"
	}
	return ""
}

// bylineLabels 是从表里人工已填的 525 篇群发文章学来的「署名 → 表里的写法」（用户 2026-11-06 定：照人工习惯归类）：
// 编辑统一大小写，笔名、兼职、投稿按人工的归类。键是去掉空格、「·」后的小写署名；没见过的署名照网页原样写。
// 百强榜、FBIF活动是人工按题材归的，不按署名学（不然一个人的别的文章也会被归进去）。
var bylineLabels = map[string]string{
	"kiki": "Kiki", "dudu": "Dudu", "riri": "Riri", "sage": "Sage", "pride": "Pride", "abby": "Abby",
	"bobo": "Bobo", "nite": "nite", "mote": "Mote莫特", "mote莫特": "Mote莫特",
	"dan": "Mote莫特", "pai": "Mote莫特", "ossewa": "Mote莫特", "panda": "橙子",
	"gloria": "兼职", "mamo": "兼职", "ez": "投稿", "王熠": "36氪未来消费",
}

func bylineKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(name, "·", "")), ""))
}

func bylineLabel(name string) string {
	if label, ok := bylineLabels[bylineKey(name)]; ok {
		return label
	}
	return strings.TrimSpace(name)
}

type articlePage struct {
	Copyright string // copyright_stat：1 原创，2 转载，0 或空是没声明
	Reprint   string // 转载来源的公众号名（转载那天的名字）
	Source    string // 转载来源的公众号编号 gh_…，改名也不变
	Body      string // 正文纯文本
}

var (
	copyrightPattern = regexp.MustCompile(`copyright_stat\s*[:=]\s*['"]?(\d)`)
	reprintPattern   = regexp.MustCompile(`class="original_primary_nickname">([^<]+)<`)
	sourcePattern    = regexp.MustCompile(`source_username\s*[:=]\s*['"](gh_\w+)['"]`)
	noencodePattern  = regexp.MustCompile(`content_noencode:\s*'((?:[^'\\]|\\.)*)'`)
	scriptPattern    = regexp.MustCompile(`(?s)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>`)
	breakPattern     = regexp.MustCompile(`<br\s*/?>|</p>|</section>|</div>|</h\d>`)
	tagPattern       = regexp.MustCompile(`<[^>]+>`)
	spacePattern     = regexp.MustCompile("[ \t 　​]+")
	blankLinePattern = regexp.MustCompile(`\n\s*\n+`)
	// bylinePattern 认「作者：Kiki」「文｜Dudu」「编辑：Riri」这类署名。
	bylinePattern = regexp.MustCompile(`(?m)(?:^|[（(\s])(编辑|作者|撰文|文|整理|责编|采写|文字|来源)\s*[：:|｜丨/]\s*([A-Za-z\p{Han}·'’ ]{1,24}?)\s*(?:$|[（(）)，,。；;|｜丨/])`)
)

func parseArticlePage(page string) articlePage {
	var parsed articlePage
	if match := copyrightPattern.FindStringSubmatch(page); match != nil {
		parsed.Copyright = match[1]
	}
	if match := reprintPattern.FindStringSubmatch(page); match != nil {
		parsed.Reprint = strings.TrimSpace(html.UnescapeString(match[1]))
	}
	if match := sourcePattern.FindStringSubmatch(page); match != nil {
		parsed.Source = match[1]
	}
	parsed.Body = pageBody(page)
	return parsed
}

// pageBody 取正文：普通图文在 js_content 里；图片消息（小绿书）没有 js_content，正文在脚本的 content_noencode 里。
func pageBody(page string) string {
	if start := strings.Index(page, `id="js_content"`); start >= 0 {
		rest := page[start:]
		if open := strings.IndexByte(rest, '>'); open >= 0 {
			rest = rest[open+1:]
			if end := strings.Index(rest, "<script"); end >= 0 {
				rest = rest[:end]
			}
			if text := htmlText(rest); text != "" {
				return text
			}
		}
	}
	if match := noencodePattern.FindStringSubmatch(page); match != nil {
		return htmlText(jsUnescape(match[1]))
	}
	return ""
}

func htmlText(fragment string) string {
	text := scriptPattern.ReplaceAllString(fragment, " ")
	text = breakPattern.ReplaceAllString(text, "\n")
	text = html.UnescapeString(tagPattern.ReplaceAllString(text, ""))
	text = spacePattern.ReplaceAllString(text, " ")
	return strings.TrimSpace(blankLinePattern.ReplaceAllString(text, "\n"))
}

// jsUnescape 解开网页脚本里单引号字符串的转义（\x3c、中、\n、\' 这些）。
func jsUnescape(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			out.WriteByte(s[i])
			continue
		}
		i++
		switch c := s[i]; c {
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'x', 'u':
			size := 2
			if c == 'u' {
				size = 4
			}
			if i+1+size <= len(s) {
				if code, err := strconv.ParseUint(s[i+1:i+1+size], 16, 32); err == nil {
					out.WriteRune(rune(code))
					i += size
					continue
				}
			}
			out.WriteByte(c)
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

type byline struct{ Role, Name string }

func bylines(text string) []byline {
	matches := bylinePattern.FindAllStringSubmatch(text, -1)
	found := make([]byline, 0, len(matches))
	for _, match := range matches {
		found = append(found, byline{Role: match[1], Name: strings.TrimSpace(match[2])})
	}
	return found
}

// bylineName 去掉署名后面跟的自家头衔：「Mote莫特 FBIF创始人」只取「Mote莫特」；
// 只剩自家号名（「FBIF食品饮料创新」）的不算作者。
func bylineName(name string) string {
	if at := strings.Index(strings.ToLower(name), "fbif"); at >= 0 {
		name = name[:at]
	}
	return strings.TrimSpace(name)
}

// firstByline 按顺序找第一个角色对得上的署名。
func firstByline(found []byline, roles ...string) string {
	for _, b := range found {
		name := bylineName(b.Name)
		if name == "" {
			continue
		}
		for _, role := range roles {
			if b.Role == role {
				return name
			}
		}
	}
	return ""
}

// signatures 是开头约 400 字里的前两个署名，和结尾约 700 字里的最后三个署名（从后往前）。
// 两段都按整行切，不把「作者：Milo」切成「作者：Mi」。
func (p articlePage) signatures() (head, tail []byline) {
	body := []rune(p.Body)
	headEnd := min(len(body), 400)
	for headEnd < len(body) && body[headEnd-1] != '\n' {
		headEnd++
	}
	head = bylines(string(body[:headEnd]))
	if len(head) > 2 {
		head = head[:2]
	}
	tailStart := max(0, len(body)-700)
	for tailStart > 0 && body[tailStart-1] != '\n' {
		tailStart--
	}
	tail = bylines(string(body[tailStart:]))
	if len(tail) > 3 {
		tail = tail[len(tail)-3:]
	}
	for i, j := 0, len(tail)-1; i < j; i, j = i+1, j-1 {
		tail[i], tail[j] = tail[j], tail[i]
	}
	return head, tail
}

// pickAuthor 按上面的顺序定作者；rule 是依据，记进状态表方便查。
func pickAuthor(title string, page articlePage) (value, rule string) {
	if value := authorFromTitle(title); value != "" {
		return value, "标题"
	}
	head, tail := page.signatures()
	source := func() string {
		if value := firstByline(tail, "来源"); value != "" {
			return value
		}
		return firstByline(head, "来源")
	}
	switch page.Copyright {
	case "2":
		if page.Reprint != "" {
			return page.Reprint, "转载"
		}
		if value := source(); value != "" {
			return value, "来源"
		}
		return "", "转载没写来源"
	case "1":
	default:
		if value := source(); value != "" {
			return value, "来源"
		}
	}
	writer := firstByline(head, "作者", "撰文", "文")
	if writer == "" {
		writer = firstByline(tail, "作者", "撰文", "文")
	}
	if writer != "" {
		return bylineLabel(writer), "作者署名"
	}
	editor := firstByline(tail, "编辑")
	if editor == "" {
		editor = firstByline(head, "编辑")
	}
	if editor != "" {
		return bylineLabel(editor), "编辑署名"
	}
	return "", "没有署名"
}

var errPageGone = errors.New("article page is gone")

const pageUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

var pageClient = &http.Client{Timeout: 30 * time.Second}

// fetchArticlePage 读公开文章页。文章已删、违规下架返回 errPageGone；被微信要求验证等认不出的页面返回错误，稍后再试。
func fetchArticlePage(ctx context.Context, link string) (string, error) {
	link = strings.Replace(strings.TrimSpace(link), "http://", "https://", 1)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", pageUserAgent)
	response, err := pageClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("article page HTTP %d", response.StatusCode)
	}
	page := string(body)
	if strings.Contains(page, `id="js_content"`) || strings.Contains(page, "content_noencode") {
		return page, nil
	}
	for _, gone := range []string{"已被发布者删除", "因违规无法查看", "涉嫌侵权", "该内容已被删除"} {
		if strings.Contains(page, gone) {
			return "", errPageGone
		}
	}
	if strings.Contains(page, "环境异常") {
		return "", errors.New("WeChat asked for verification (环境异常)")
	}
	return "", errors.New("article page has no content")
}

type AuthorResult struct {
	Filled   int `json:"filled"`
	Kept     int `json:"kept"`
	NoByline int `json:"noByline"`
	Failed   int `json:"failed"`
	Left     int `json:"left"`
}

type authorTask struct {
	recordID, msgID, title, link string
	mid                          int64
	idx                          int
}

type authorFill struct {
	task        authorTask
	value, rule string
	status      string
	source      string // 转载来源的公众号编号
}

// cellText 读回一个格子的文字：单选回的是字符串，文本回的是分段数组。
func cellText(value interface{}) string {
	if list, ok := value.([]interface{}); ok {
		var parts []string
		for _, item := range list {
			if text, ok := item.(string); ok {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.TrimSpace(strings.Join(parts, ""))
		}
	}
	return textValue(value) // 全角空格占位也会被去掉，算空
}

func linkCell(value interface{}) (title, link string) {
	if typed, ok := value.(map[string]interface{}); ok {
		title, _ = typed["text"].(string)
		link, _ = typed["link"].(string)
		return strings.TrimSpace(title), link
	}
	text := textValue(value)
	return text, text
}

func (w *Worker) fetchPage(ctx context.Context, link string) (string, error) {
	if w.FetchPage != nil {
		return w.FetchPage(ctx, link)
	}
	return fetchArticlePage(ctx, link)
}

func (w *Worker) pause(ctx context.Context) error {
	gap := authorFetchGap
	if w.FetchGap != nil {
		gap = *w.FetchGap
	}
	if gap <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(gap)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fillAuthors 跑一轮：读一遍表里的链接和作者，没看过的行里作者空着的，按顺序找作者填上。
// 读网页慢（每 6 秒一篇），每找到 20 篇就写一次；写之前再读一遍这些格子，期间有人填了就不动。
func (w *Worker) fillAuthors(ctx context.Context) (AuthorResult, error) {
	var result AuthorResult
	done, err := w.Store.BizAuthorRecords(ctx)
	if err != nil {
		return result, err
	}
	accountNames, err := w.Store.BizAuthorSourceNames(ctx)
	if err != nil {
		return result, err
	}
	records, err := w.Base.SearchRecords(w.ArticleTableID, []string{"内容标题", authorColumn})
	if err != nil {
		return result, fmt.Errorf("read article table: %w", err)
	}
	var tasks []authorTask
	for _, record := range records {
		if done[record.RecordID] != "" {
			continue
		}
		title, link := linkCell(record.Fields["内容标题"])
		match := articleLinkPattern.FindStringSubmatch(link)
		if match == nil {
			continue // 没有文章链接的行读不了网页
		}
		if cellText(record.Fields[authorColumn]) != "" {
			if err := w.Store.SaveBizAuthor(ctx, record.RecordID, match[1]+"_"+match[2], archive.AuthorKept, "", "", "", w.now()); err != nil {
				return result, err
			}
			result.Kept++
			continue
		}
		mid, _ := strconv.ParseInt(match[1], 10, 64)
		idx, _ := strconv.Atoi(match[2])
		tasks = append(tasks, authorTask{recordID: record.RecordID, msgID: match[1] + "_" + match[2], title: title, link: link, mid: mid, idx: idx})
	}
	// 先补最近的。
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].mid != tasks[j].mid {
			return tasks[i].mid > tasks[j].mid
		}
		return tasks[i].idx < tasks[j].idx
	})

	pages := make(map[string]authorFill) // 同一篇文章在表里有两行时只读一次网页
	var batch []authorFill
	failuresInARow, pagesRead := 0, 0
	var stopErr, lastErr error
	for i, task := range tasks {
		if ctx.Err() != nil {
			result.Left = len(tasks) - i
			break
		}
		fill, ok := pages[task.msgID]
		if !ok {
			fill, err = w.findAuthor(ctx, task)
			if err != nil {
				if ctx.Err() != nil {
					result.Left = len(tasks) - i
					break
				}
				result.Failed++
				failuresInARow++
				lastErr = err
				if w.authorMisses == nil {
					w.authorMisses = make(map[string]int)
				}
				w.authorMisses[task.recordID]++
				log.Printf("[BizData] author: page %s failed (%d for this row): %v", task.msgID, w.authorMisses[task.recordID], err)
				if w.authorMisses[task.recordID] >= authorMaxMisses {
					batch = append(batch, authorFill{task: task, status: archive.AuthorNone, rule: "网页打不开"})
				}
				if failuresInARow >= authorMaxFailuresInARow {
					stopErr = fmt.Errorf("%d article pages in a row failed, last: %w", failuresInARow, err)
					result.Left = len(tasks) - i - 1
					break
				}
				continue
			}
			failuresInARow = 0
			pagesRead++
			if fill.source != "" && fill.value != "" {
				if name, ok := accountNames[fill.source]; ok {
					fill.value = name // 同一个号沿用已经写过的名字
				} else {
					accountNames[fill.source] = fill.value
				}
			}
			pages[task.msgID] = fill
		}
		fill.task = task
		batch = append(batch, fill)
		if len(batch) >= authorBatch {
			if err := w.writeAuthors(ctx, batch, &result); err != nil {
				return result, err
			}
			batch = batch[:0]
		}
	}
	if err := w.writeAuthors(ctx, batch, &result); err != nil {
		return result, err
	}
	if stopErr == nil && result.Failed > 0 && pagesRead == 0 {
		stopErr = fmt.Errorf("all %d article pages failed, last: %w", result.Failed, lastErr) // 一篇都没读成也算这轮失败
	}
	return result, stopErr
}

// findAuthor 先看标题，认不出才读网页（读之前按间隔歇一下）。
func (w *Worker) findAuthor(ctx context.Context, task authorTask) (authorFill, error) {
	if value := authorFromTitle(task.title); value != "" {
		return authorFill{value: value, rule: "标题", status: archive.AuthorWritten}, nil
	}
	if err := w.pause(ctx); err != nil {
		return authorFill{}, err
	}
	page, err := w.fetchPage(ctx, task.link)
	if errors.Is(err, errPageGone) {
		return authorFill{rule: "文章已删", status: archive.AuthorNone}, nil
	}
	if err != nil {
		return authorFill{}, err
	}
	parsed := parseArticlePage(page)
	value, rule := pickAuthor(task.title, parsed)
	if value == "" {
		return authorFill{rule: rule, status: archive.AuthorNone}, nil
	}
	fill := authorFill{value: value, rule: rule, status: archive.AuthorWritten}
	if rule == "转载" {
		fill.source = parsed.Source
	}
	return fill, nil
}

// writeAuthors 写一批：先读回这些格子，还空着才写；表里被删了的行记成 gone。
func (w *Worker) writeAuthors(ctx context.Context, batch []authorFill, result *AuthorResult) error {
	if len(batch) == 0 {
		return nil
	}
	var ids []string
	for _, fill := range batch {
		if fill.status == archive.AuthorWritten {
			ids = append(ids, fill.task.recordID)
		}
	}
	current := make(map[string]map[string]interface{}, len(ids))
	gone := make(map[string]bool)
	if len(ids) > 0 {
		records, absent, err := w.Base.BatchGetRecords(w.ArticleTableID, ids)
		if err != nil {
			return fmt.Errorf("read 作者 before writing: %w", err)
		}
		for _, record := range records {
			current[record.RecordID] = record.Fields
		}
		for _, id := range absent {
			gone[id] = true
		}
	}
	var payload []map[string]interface{}
	for i, fill := range batch {
		if fill.status != archive.AuthorWritten {
			continue
		}
		id := fill.task.recordID
		switch {
		case gone[id]:
			batch[i].status, batch[i].value = archive.AuthorGone, ""
		case cellText(current[id][authorColumn]) != "":
			batch[i].status, batch[i].value, batch[i].rule = archive.AuthorKept, "", "写之前已有人填"
		default:
			payload = append(payload, map[string]interface{}{"record_id": id, "fields": map[string]interface{}{authorColumn: fill.value}})
		}
	}
	if len(payload) > 0 {
		if err := w.Base.BatchUpdateByRecordID(w.ArticleTableID, payload); err != nil {
			return fmt.Errorf("write 作者: %w", err)
		}
	}
	for _, fill := range batch {
		if err := w.Store.SaveBizAuthor(ctx, fill.task.recordID, fill.task.msgID, fill.status, fill.value, fill.rule, fill.source, w.now()); err != nil {
			return err
		}
		switch fill.status {
		case archive.AuthorWritten:
			result.Filled++
		case archive.AuthorKept:
			result.Kept++
		case archive.AuthorNone:
			result.NoByline++
		}
	}
	return nil
}

func (w *Worker) WakeAuthors() {
	if w == nil || w.authorWake == nil {
		return
	}
	select {
	case w.authorWake <- struct{}{}:
	default:
	}
}

// wakeLoop 不定时轮询：启动后跑一轮（补历史、接着上次没补完的），之后只在写表那边新建或认领了文章行时被叫醒；
// round 返回下次最晚多久再跑（失败退避、或有事要过一会儿再看），0 表示只等叫醒。作者、品类国家各用一个。
func wakeLoop(stopCh <-chan struct{}, wake <-chan struct{}, initialDelay time.Duration, round func(context.Context) time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-stopCh
		cancel()
	}()
	wait := initialDelay
	for {
		var timer *time.Timer
		var fire <-chan time.Time
		if wait > 0 {
			timer = time.NewTimer(wait)
			fire = timer.C
		}
		select {
		case <-fire:
		case <-wake:
		case <-ctx.Done():
			return
		}
		if timer != nil {
			timer.Stop()
		}
		wait = round(ctx)
	}
}

// authorRound 跑一轮，返回下次最晚多久再跑；0 表示等叫醒。
func (w *Worker) authorRound(ctx context.Context, fallback time.Duration) time.Duration {
	result, err := w.fillAuthors(ctx)
	if result.Filled+result.Kept+result.NoByline+result.Failed > 0 {
		log.Printf("[BizData] author: filled=%d kept=%d no_byline=%d failed=%d left=%d", result.Filled, result.Kept, result.NoByline, result.Failed, result.Left)
	}
	if ctx.Err() != nil {
		return 0
	}
	if err == nil {
		w.authorFailures = 0
		return 0
	}
	w.authorFailures++
	log.Printf("[BizData] author round failed (%d in a row): %v", w.authorFailures, err)
	if w.authorFailures == 3 && w.Alert != nil {
		w.Alert(fmt.Sprintf("公众号文章作者补全连续 3 次失败：%v", err))
	}
	backoff := firstRetry << (w.authorFailures - 1)
	if backoff > fallback || backoff <= 0 {
		backoff = fallback
	}
	return backoff
}
