package wechat

import (
	"testing"
	"time"
)

func TestMessageIDFromArticleURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "standard wechat article url",
			url:  "https://mp.weixin.qq.com/s?__biz=MzA4MDAzNjQzNQ==&mid=2650560574&idx=2&sn=abcdef#rd",
			want: "2650560574_2",
		},
		{
			name: "missing idx",
			url:  "https://mp.weixin.qq.com/s?mid=2650560574",
			want: "",
		},
		{
			name: "invalid url",
			url:  "://bad-url",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MessageIDFromArticleURL(tt.url); got != tt.want {
				t.Fatalf("MessageIDFromArticleURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestArticleIndexFromURL(t *testing.T) {
	idx := ArticleIndexFromURL("https://mp.weixin.qq.com/s?mid=2650560574&idx=3")
	if idx == nil || *idx != 3 {
		t.Fatalf("ArticleIndexFromURL() = %v, want 3", idx)
	}

	if ArticleIndexFromURL("https://mp.weixin.qq.com/s?mid=2650560574") != nil {
		t.Fatal("ArticleIndexFromURL() should return nil when idx is missing")
	}
}

func TestParseArticleMetadataHTML(t *testing.T) {
	html := `
	<html>
	  <head>
	    <meta property="og:image" content="https://mmbiz.qpic.cn/example.jpg" />
	    <script>
	      var ct = "1711094400";
	    </script>
	  </head>
	</html>`

	meta := parseArticleMetadataHTML(html)
	if meta.CoverURL != "https://mmbiz.qpic.cn/example.jpg" {
		t.Fatalf("CoverURL = %q", meta.CoverURL)
	}

	want := time.Unix(1711094400, 0)
	if !meta.PublishTime.Equal(want) {
		t.Fatalf("PublishTime = %v, want %v", meta.PublishTime, want)
	}
}
