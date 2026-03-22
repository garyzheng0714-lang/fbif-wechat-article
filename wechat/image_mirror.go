package wechat

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	articleImgTagRe    = regexp.MustCompile(`(?is)<img\b[^>]*?(?:data-src|src)=["']([^"']+)["'][^>]*>`)
	imageMirrorCache   = make(map[string]string)
	imageMirrorCacheMu sync.Mutex
)

func ExtractArticleImageURLs(htmlContent string) []string {
	if strings.TrimSpace(htmlContent) == "" {
		return nil
	}

	seen := make(map[string]struct{})
	var result []string
	matches := articleImgTagRe.FindAllStringSubmatch(htmlContent, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		u := normalizeImageURL(match[1])
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		result = append(result, u)
	}
	return result
}

func MirrorArticleImages(htmlContent string) ([]string, error) {
	srcs := ExtractArticleImageURLs(htmlContent)
	if len(srcs) == 0 {
		return nil, nil
	}

	results := make([]string, 0, len(srcs))
	var firstErr error
	for _, src := range srcs {
		publicURL, err := MirrorRemoteImage(src)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if publicURL != "" {
			results = append(results, publicURL)
		}
	}
	return results, firstErr
}

func MirrorRemoteImage(imageURL string) (string, error) {
	imageURL = normalizeImageURL(imageURL)
	if imageURL == "" {
		return "", nil
	}

	imageMirrorCacheMu.Lock()
	if cached, ok := imageMirrorCache[imageURL]; ok {
		imageMirrorCacheMu.Unlock()
		return cached, nil
	}
	imageMirrorCacheMu.Unlock()

	if strings.HasPrefix(imageURL, publicBaseURL()+"/media/") {
		return imageURL, nil
	}

	req, err := http.NewRequest(http.MethodGet, imageURL, nil)
	if err != nil {
		return "", fmt.Errorf("build image request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; fbif-wechat-sync/1.0)")
	req.Header.Set("Referer", "https://mp.weixin.qq.com/")

	resp, err := contentClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return "", fmt.Errorf("download image HTTP %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}
	if len(data) == 0 {
		return "", nil
	}

	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:])
	ext := detectImageExt(imageURL, resp.Header.Get("Content-Type"))
	objectKey := filepath.ToSlash(filepath.Join("wechat-images", name+ext))
	publicURL, err := storeMirroredImage(objectKey, data, resp.Header.Get("Content-Type"))
	if err != nil {
		return "", err
	}
	imageMirrorCacheMu.Lock()
	imageMirrorCache[imageURL] = publicURL
	imageMirrorCacheMu.Unlock()
	return publicURL, nil
}

func storeMirroredImage(objectKey string, data []byte, contentType string) (string, error) {
	if ossEnabled() {
		return uploadToOSS(objectKey, data, contentType)
	}
	return writeLocalMirroredImage(objectKey, data)
}

func writeLocalMirroredImage(objectKey string, data []byte) (string, error) {
	fullPath := filepath.Join(publicMediaRoot(), filepath.FromSlash(objectKey))

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", fmt.Errorf("mkdir image dir: %w", err)
	}
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		if err := os.WriteFile(fullPath, data, 0644); err != nil {
			return "", fmt.Errorf("write mirrored image: %w", err)
		}
	}
	return publicBaseURL() + "/media/" + objectKey, nil
}

func ossEnabled() bool {
	return strings.TrimSpace(os.Getenv("OSS_ACCESS_KEY_ID")) != "" &&
		strings.TrimSpace(os.Getenv("OSS_ACCESS_KEY_SECRET")) != "" &&
		strings.TrimSpace(os.Getenv("OSS_BUCKET")) != ""
}

func uploadToOSS(objectKey string, data []byte, contentType string) (string, error) {
	contentType = normalizeContentType(contentType, data, objectKey)
	date := time.Now().UTC().Format(http.TimeFormat)

	headers := map[string]string{
		"Content-Type":     contentType,
		"Date":             date,
		"x-oss-object-acl": "public-read",
	}

	signature := signOSSRequest(http.MethodPut, headers, os.Getenv("OSS_BUCKET"), objectKey)
	fullURL := ossObjectURL(objectKey)

	req, err := http.NewRequest(http.MethodPut, fullURL, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("build oss upload request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", signature)

	resp, err := contentClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload to oss: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("oss upload HTTP %d: %s", resp.StatusCode, string(body))
	}

	return ossPublicURL(objectKey), nil
}

func signOSSRequest(method string, headers map[string]string, bucket, objectKey string) string {
	stringToSign := strings.Join([]string{
		method,
		"",
		headers["Content-Type"],
		headers["Date"],
		canonicalizedOSSHeaders(headers) + canonicalizedResource(bucket, objectKey),
	}, "\n")

	mac := hmac.New(sha1.New, []byte(os.Getenv("OSS_ACCESS_KEY_SECRET")))
	mac.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return "OSS " + os.Getenv("OSS_ACCESS_KEY_ID") + ":" + signature
}

func canonicalizedOSSHeaders(headers map[string]string) string {
	type kv struct {
		key string
		val string
	}
	var items []kv
	for k, v := range headers {
		lk := strings.ToLower(strings.TrimSpace(k))
		if !strings.HasPrefix(lk, "x-oss-") {
			continue
		}
		items = append(items, kv{key: lk, val: strings.TrimSpace(v)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].key < items[j].key })

	var b strings.Builder
	for _, item := range items {
		b.WriteString(item.key)
		b.WriteString(":")
		b.WriteString(item.val)
		b.WriteString("\n")
	}
	return b.String()
}

func canonicalizedResource(bucket, objectKey string) string {
	return "/" + bucket + "/" + strings.TrimLeft(objectKey, "/")
}

func ossObjectURL(objectKey string) string {
	base := ossBaseURL()
	return base + "/" + escapeObjectKey(objectKey)
}

func ossPublicURL(objectKey string) string {
	return ossBaseURL() + "/" + escapeObjectKey(objectKey)
}

func ossBaseURL() string {
	if domain := strings.TrimSpace(os.Getenv("OSS_BUCKET_DOMAIN")); domain != "" {
		if strings.HasPrefix(domain, "http://") || strings.HasPrefix(domain, "https://") {
			return strings.TrimRight(domain, "/")
		}
		return "https://" + strings.TrimRight(domain, "/")
	}

	bucket := strings.TrimSpace(os.Getenv("OSS_BUCKET"))
	region := strings.TrimSpace(os.Getenv("OSS_REGION"))
	return fmt.Sprintf("https://%s.oss-%s.aliyuncs.com", bucket, region)
}

func escapeObjectKey(objectKey string) string {
	parts := strings.Split(strings.TrimLeft(objectKey, "/"), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func normalizeContentType(contentType string, data []byte, objectKey string) string {
	if contentType != "" {
		if mediatype, _, err := mime.ParseMediaType(contentType); err == nil && mediatype != "" {
			return mediatype
		}
	}
	if ext := path.Ext(objectKey); ext != "" {
		if byExt := mime.TypeByExtension(ext); byExt != "" {
			if mediatype, _, err := mime.ParseMediaType(byExt); err == nil && mediatype != "" {
				return mediatype
			}
		}
	}
	return http.DetectContentType(data)
}

func publicBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://112.124.103.65:3002"
}

func publicMediaRoot() string {
	if v := strings.TrimSpace(os.Getenv("PUBLIC_MEDIA_DIR")); v != "" {
		return v
	}
	return "./media"
}

func normalizeImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "/") {
		return ""
	}
	return raw
}

func detectImageExt(rawURL, contentType string) string {
	if contentType != "" {
		if mediatype, _, err := mime.ParseMediaType(contentType); err == nil {
			if exts, _ := mime.ExtensionsByType(mediatype); len(exts) > 0 {
				return exts[0]
			}
		}
	}

	if u, err := url.Parse(rawURL); err == nil {
		if ext := path.Ext(u.Path); ext != "" {
			return ext
		}
		if wxfmt := u.Query().Get("wx_fmt"); wxfmt != "" {
			if strings.HasPrefix(wxfmt, ".") {
				return wxfmt
			}
			return "." + wxfmt
		}
	}

	return ".jpg"
}
