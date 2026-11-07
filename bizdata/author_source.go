package bizdata

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
)

// 转载文章的作者写原公众号名，按网页里的公众号编号认号，号改过名也只用表里先写下的那种写法（用户 2026-11-06 定）。
// 2026-11-06 首轮补作者时还没记编号，那批转载行在这里补：读网页取编号（和别的读网页共用每 6 秒一篇的闸口），
// 再把同一个号的写法统一成最早写下的那种。只改程序写的、格子里还是程序写的那个名字的行；有人改过的不碰，记成人填的。

const reprintSourceUnknown = "转载（来源编号不明）"

// fillReprintSources 补编号、统一写法；links 是表里每行的文章链接。服务器读不了网页时停下，下一轮接着补。
func (w *Worker) fillReprintSources(ctx context.Context, links map[string]string, result *AuthorResult) error {
	rows, err := w.Store.BizAuthorReprints(ctx)
	if err != nil {
		return err
	}
	var missing []archive.BizAuthorReprint
	for _, row := range rows {
		if row.Source == "" {
			missing = append(missing, row)
		}
	}
	sort.SliceStable(missing, func(i, j int) bool { return msgOrder(missing[i].MsgID) > msgOrder(missing[j].MsgID) }) // 先补最近的
	waiting := 0
	for _, row := range missing {
		if err := ctx.Err(); err != nil {
			return err
		}
		link, inTable := links[row.RecordID]
		if !inTable {
			if err := w.Store.UpdateBizAuthor(ctx, row.RecordID, archive.AuthorGone, row.Value, "转载", ""); err != nil {
				return err
			}
			continue // 表里这行没了
		}
		var page string
		err := errPageGone // 这行的链接没了，读不了网页，和文章已删一样记成编号不明
		if link != "" {
			page, err = w.readPage(ctx, link)
		}
		source := ""
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case serverBlocked(err):
			return err
		case errors.Is(err, errPageGone):
		case err != nil:
			if w.authorMisses == nil {
				w.authorMisses = make(map[string]int)
			}
			w.authorMisses[row.RecordID]++
			log.Printf("[BizData] author: reprint page %s failed (%d for this row): %v", row.MsgID, w.authorMisses[row.RecordID], err)
			if w.authorMisses[row.RecordID] < authorMaxMisses {
				result.Left++
				waiting++
				continue
			}
		default:
			source = parseArticlePage(page).Source
		}
		rule := "转载"
		if source == "" {
			rule = reprintSourceUnknown
		}
		if err := w.Store.UpdateBizAuthor(ctx, row.RecordID, archive.AuthorWritten, row.Value, rule, source); err != nil {
			return err
		}
		if source != "" {
			result.Sourced++
		}
	}
	if waiting > 0 {
		return nil // 还有行的编号没补上：它可能才是最早写下的那种，补齐了再统一，免得改两回
	}
	return w.unifyReprintNames(ctx, result)
}

// unifyReprintNames 把同一个号的写法统一成最早写下的那种；写之前再读一遍格子，期间有人改过就不动。
func (w *Worker) unifyReprintNames(ctx context.Context, result *AuthorResult) error {
	names, err := w.Store.BizAuthorSourceNames(ctx)
	if err != nil {
		return err
	}
	rows, err := w.Store.BizAuthorReprints(ctx)
	if err != nil {
		return err
	}
	var change []archive.BizAuthorReprint
	for _, row := range rows {
		if name := names[row.Source]; row.Source != "" && name != "" && name != row.Value {
			change = append(change, row)
		}
	}
	for start := 0; start < len(change); start += 100 {
		chunk := change[start:min(start+100, len(change))]
		ids := make([]string, len(chunk))
		for i, row := range chunk {
			ids[i] = row.RecordID
		}
		records, absent, err := w.Base.BatchGetRecords(w.ArticleTableID, ids)
		if err != nil {
			return fmt.Errorf("read 作者 before unifying: %w", err)
		}
		current := make(map[string]string, len(records))
		for _, record := range records {
			current[record.RecordID] = cellText(record.Fields[authorColumn])
		}
		gone := make(map[string]bool, len(absent))
		for _, id := range absent {
			gone[id] = true
		}
		var payload []map[string]interface{}
		var unified []archive.BizAuthorReprint
		for _, row := range chunk {
			switch {
			case gone[row.RecordID]:
				err = w.Store.UpdateBizAuthor(ctx, row.RecordID, archive.AuthorGone, row.Value, "转载", row.Source)
			case current[row.RecordID] != row.Value:
				err = w.Store.UpdateBizAuthor(ctx, row.RecordID, archive.AuthorKept, "", "写之后有人改过", row.Source)
			default:
				payload = append(payload, map[string]interface{}{"record_id": row.RecordID, "fields": map[string]interface{}{authorColumn: names[row.Source]}})
				unified = append(unified, row)
			}
			if err != nil {
				return err
			}
		}
		if len(payload) > 0 {
			if err := w.Base.BatchUpdateByRecordID(w.ArticleTableID, payload); err != nil {
				return fmt.Errorf("unify 作者: %w", err)
			}
		}
		for _, row := range unified {
			if err := w.Store.UpdateBizAuthor(ctx, row.RecordID, archive.AuthorWritten, names[row.Source], "转载", row.Source); err != nil {
				return err
			}
			result.Unified++
		}
	}
	return nil
}

// msgOrder 是 mid_idx 的先后：mid 大的新，同一次群发 idx 小的在前。
func msgOrder(msgID string) int64 {
	mid, idx, _ := strings.Cut(msgID, "_")
	m, _ := strconv.ParseInt(mid, 10, 64)
	i, _ := strconv.ParseInt(idx, 10, 64)
	return m*100 - i
}
