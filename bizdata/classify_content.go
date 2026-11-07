package bizdata

import (
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// 递给资讯订阅分类接口的正文只要开头一段。资讯订阅的摘要是正文去掉标签后的前 180 个字
// （ingest.summarize），属性、注释不出字；资讯订阅前面有 nginx，一次请求超过 1MB 回 413
// （2026-11-07 生产上：整页正文 HTML 一篇最大 2.6MB，连着 9 轮 413）。
// 所以去掉属性和注释，留到文字够 classifyContentText 个字的那里，最多 classifyContentBytes。
const (
	classifyContentText  = 1000
	classifyContentBytes = 64 << 10
)

// classifyContent 把正文 HTML 压成递过去的那一段：标签只留名字，文字原样（重新转义），注释和 DOCTYPE 去掉；
// 资讯订阅去标签后得到的文字和整页的开头一样。
func classifyContent(raw string) string {
	tokens := html.NewTokenizer(strings.NewReader(raw))
	var out strings.Builder
	text := 0 // 已经留下的非空白字数
	for out.Len() < classifyContentBytes && text < classifyContentText {
		switch tokens.Next() {
		case html.ErrorToken:
			return out.String()
		case html.TextToken:
			chunk := string(tokens.Text())
			for i, r := range chunk {
				if text >= classifyContentText || out.Len()+i >= classifyContentBytes {
					chunk = chunk[:i]
					break
				}
				if !unicode.IsSpace(r) {
					text++
				}
			}
			out.WriteString(html.EscapeString(chunk))
		case html.StartTagToken:
			name, _ := tokens.TagName()
			out.WriteString("<" + string(name) + ">")
		case html.SelfClosingTagToken:
			name, _ := tokens.TagName()
			out.WriteString("<" + string(name) + "/>")
		case html.EndTagToken:
			name, _ := tokens.TagName()
			out.WriteString("</" + string(name) + ">")
		}
	}
	return out.String()
}
