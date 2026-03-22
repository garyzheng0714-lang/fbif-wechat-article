package wechat

import "testing"

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
