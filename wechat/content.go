package wechat

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var contentClient = &http.Client{Timeout: 60 * time.Second}

// ==================== Text Cleaning ====================

var (
	reMarkdownImg    = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	reMarkdownLink   = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reMarkdownBold   = regexp.MustCompile(`\*{2}([^*]+)\*{2}`)
	reMarkdownItalic = regexp.MustCompile(`(?s)\*([^*\n]+)\*`)
	reMarkdownHead   = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reMarkdownCode   = regexp.MustCompile("`([^`]*)`")
	reMarkdownHR     = regexp.MustCompile(`(?m)^---+\s*$`)
	reHTMLTag        = regexp.MustCompile(`<[^>]+>`)
	reExtraBlankLine = regexp.MustCompile(`\n{3,}`)
)

// cleanToPlainText strips markdown syntax and HTML entities,
// returning clean readable text.
func CleanHTMLToPlainText(md string) string {
	// Remove markdown images (keep alt text if present, else discard)
	md = reMarkdownImg.ReplaceAllStringFunc(md, func(s string) string {
		alt := reMarkdownImg.FindStringSubmatch(s)
		if len(alt) > 1 && strings.TrimSpace(alt[1]) != "" {
			return "[图片: " + alt[1] + "]"
		}
		return "[图片]"
	})

	// Remove markdown links (keep link text)
	md = reMarkdownLink.ReplaceAllString(md, "$1")

	// Remove bold/italic markers
	md = reMarkdownBold.ReplaceAllString(md, "$1")
	md = reMarkdownItalic.ReplaceAllString(md, "$1")

	// Remove heading markers
	md = reMarkdownHead.ReplaceAllString(md, "")

	// Remove inline code backticks (keep content)
	md = reMarkdownCode.ReplaceAllString(md, "$1")

	// Remove horizontal rules
	md = reMarkdownHR.ReplaceAllString(md, "")

	// Remove any remaining HTML tags
	md = reHTMLTag.ReplaceAllString(md, "")

	// Decode HTML entities (e.g. &amp; → &, &lt; → <, &#39; → ')
	md = html.UnescapeString(md)

	// Collapse excessive blank lines
	md = reExtraBlankLine.ReplaceAllString(md, "\n\n")

	return strings.TrimSpace(md)
}
