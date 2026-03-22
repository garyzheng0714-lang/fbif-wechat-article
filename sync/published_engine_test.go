package sync

import (
	"testing"

	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

func TestToPublishedArticleRecord(t *testing.T) {
	item := wechat.PublishedArticleItem{
		ArticleID:  "article-123",
		UpdateTime: 1711094400,
	}
	article := wechat.PublishedArticle{
		Title:        "已发布文章",
		Author:       "FBIF",
		Digest:       "摘要内容",
		Content:      "<p>正文内容</p>",
		ThumbMediaID: "thumb-media-id",
		URL:          "https://mp.weixin.qq.com/s?__biz=MzA4MDAzNjQzNQ==&mid=2650560574&idx=2&sn=abcdef#rd",
	}

	record, ok := toPublishedArticleRecord(item, article, 2)
	if !ok {
		t.Fatal("expected published article record to be accepted")
	}

	if record.UniqueKey != "2650560574_2" {
		t.Fatalf("UniqueKey = %q, want %q", record.UniqueKey, "2650560574_2")
	}
	if record.Fields["消息ID"] != "2650560574_2" {
		t.Fatalf("消息ID = %v", record.Fields["消息ID"])
	}
	if record.Fields["文章ID"] != "article-123" {
		t.Fatalf("文章ID = %v", record.Fields["文章ID"])
	}
	if record.Fields["正文来源"] != "official_api" {
		t.Fatalf("正文来源 = %v", record.Fields["正文来源"])
	}
	if record.Fields["文章内容"] != "正文内容" {
		t.Fatalf("文章内容 = %v", record.Fields["文章内容"])
	}
	if record.Fields["作者"] != "FBIF" {
		t.Fatalf("作者 = %v", record.Fields["作者"])
	}
	if record.Fields["封面素材ID"] != "thumb-media-id" {
		t.Fatalf("封面素材ID = %v", record.Fields["封面素材ID"])
	}
}
