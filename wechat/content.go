package wechat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var contentClient = &http.Client{Timeout: 30 * time.Second}

// FetchArticleContent fetches a WeChat article's full text via Jina Reader.
// WeChat CDN images are replaced with publicly accessible URLs:
//   - If SMMS_API_TOKEN env var is set: uploaded permanently to SM.MS
//   - Otherwise: proxied via wsrv.nl (immediate, zero config)
//
// Returns cleaned plain text (no HTML/markdown entities or syntax).
func FetchArticleContent(articleURL string) (string, error) {
	req, err := http.NewRequest("GET", "https://r.jina.ai/"+articleURL, nil)
	if err != nil {
		return "", fmt.Errorf("build jina request: %w", err)
	}
	req.Header.Set("Accept", "text/markdown")
	req.Header.Set("User-Agent", "fbif-wechat-sync/1.0")

	// Add Jina API key if configured
	if apiKey := os.Getenv("JINA_API_KEY"); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := contentClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("jina fetch: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return "", fmt.Errorf("jina HTTP %d: %s", resp.StatusCode, snippet)
	}

	markdown := string(body)

	// Strip Jina's prepended title heading (first non-empty line)
	if idx := strings.Index(markdown, "\n"); idx >= 0 {
		markdown = strings.TrimLeft(markdown[idx+1:], "\n")
	}

	// Replace WeChat CDN image URLs with publicly accessible ones
	markdown = replaceWechatImages(markdown)

	// Convert to clean plain text
	text := cleanToPlainText(markdown)

	// Feishu text fields cap at 50000 chars — keep a safe margin
	if len([]rune(text)) > 48000 {
		runes := []rune(text)
		text = string(runes[:48000]) + "\n\n[内容已截断]"
	}

	return text, nil
}

// ==================== Image Handling ====================

var wechatImgRe = regexp.MustCompile(`https?://mmbiz\.qpic\.cn/[^\s)\]"'<>]+`)

func replaceWechatImages(content string) string {
	return wechatImgRe.ReplaceAllStringFunc(content, func(imgURL string) string {
		if hosted := smmsUpload(imgURL); hosted != "" {
			return hosted
		}
		// Fallback: wsrv.nl image proxy (no storage, but bypasses anti-hotlinking)
		return "https://wsrv.nl/?url=" + imgURL
	})
}

// smmsUpload downloads imgURL and uploads it to SM.MS.
// Returns the permanent URL, or "" if SMMS_API_TOKEN is not set or upload fails.
func smmsUpload(imgURL string) string {
	token := os.Getenv("SMMS_API_TOKEN")
	if token == "" {
		return ""
	}

	resp, err := contentClient.Get(imgURL)
	if err != nil || resp.StatusCode >= 300 {
		return ""
	}
	defer resp.Body.Close()

	imgData, err := io.ReadAll(resp.Body)
	if err != nil || len(imgData) == 0 {
		return ""
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("smfile", "image.jpg")
	if err != nil {
		return ""
	}
	if _, err := fw.Write(imgData); err != nil {
		return ""
	}
	w.Close()

	req, err := http.NewRequest("POST", "https://smms.app/api/v2/upload", &buf)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", w.FormDataContentType())

	upResp, err := contentClient.Do(req)
	if err != nil || upResp.StatusCode >= 300 {
		return ""
	}
	defer upResp.Body.Close()

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(upResp.Body).Decode(&result); err != nil || !result.Success {
		return ""
	}
	return result.Data.URL
}

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
func cleanToPlainText(md string) string {
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
