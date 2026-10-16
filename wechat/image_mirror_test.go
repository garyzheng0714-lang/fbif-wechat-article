package wechat

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

var testPNG = []byte("\x89PNG\r\n\x1a\nmirror-test")

func serveTestImage(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func withoutOSS(t *testing.T) {
	t.Helper()
	for _, key := range []string{"OSS_ACCESS_KEY_ID", "OSS_ACCESS_KEY_SECRET", "OSS_BUCKET", "OSS_BUCKET_DOMAIN", "OSS_REGION"} {
		t.Setenv(key, "")
	}
}

func TestMirrorRemoteImageWithoutDestinationLeavesImageAlone(t *testing.T) {
	withoutOSS(t)
	t.Setenv("PUBLIC_BASE_URL", "")
	mediaDir := t.TempDir()
	t.Setenv("PUBLIC_MEDIA_DIR", mediaDir)
	server, hits := serveTestImage(t)

	got, err := MirrorRemoteImage(server.URL + "/unconfigured.png")

	if !errors.Is(err, errImageMirrorNotConfigured) {
		t.Fatalf("err = %v, want errImageMirrorNotConfigured", err)
	}
	if got != "" {
		t.Fatalf("got = %q, want no mirrored URL", got)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("image downloaded %d times, want 0", n)
	}
	entries, err := os.ReadDir(mediaDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("media dir has %d entries, want 0", len(entries))
	}
}

func TestMirrorRemoteImageServesLocalCopyUnderPublicBaseURL(t *testing.T) {
	withoutOSS(t)
	t.Setenv("PUBLIC_BASE_URL", "https://media.example.test/")
	mediaDir := t.TempDir()
	t.Setenv("PUBLIC_MEDIA_DIR", mediaDir)
	server, _ := serveTestImage(t)

	got, err := MirrorRemoteImage(server.URL + "/configured.png")
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(testPNG)
	objectKey := "wechat-images/" + hex.EncodeToString(sum[:]) + ".png"
	if want := "https://media.example.test/media/" + objectKey; got != want {
		t.Fatalf("got = %q, want %q", got, want)
	}
	stored, err := os.ReadFile(filepath.Join(mediaDir, filepath.FromSlash(objectKey)))
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(testPNG) {
		t.Fatalf("stored image differs from downloaded image")
	}
}

func TestExtractArticleImageURLs(t *testing.T) {
	html := `
	<p><img src="https://example.com/a.jpg" /></p>
	<p><img data-src="//mmbiz.qpic.cn/test/b.png" src="placeholder" /></p>
	<p><img data-src="https://example.com/a.jpg" /></p>`

	got := ExtractArticleImageURLs(html)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0] != "https://example.com/a.jpg" {
		t.Fatalf("got[0] = %q", got[0])
	}
	if got[1] != "https://mmbiz.qpic.cn/test/b.png" {
		t.Fatalf("got[1] = %q", got[1])
	}
}
