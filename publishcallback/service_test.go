package publishcallback

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1" // #nosec G505 -- mirrors the WeChat signature contract.
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/autolayout"
)

type fakeURLSubmitter struct {
	URLs     []string
	FailNext bool
}

type fakeClassifier struct {
	Kinds       map[string]ArticleKind
	Errors      map[string]error
	DefaultKind ArticleKind
	Calls       []string
}

func (f *fakeClassifier) Classify(_ context.Context, articleURL string) (Classification, error) {
	f.Calls = append(f.Calls, articleURL)
	if err := f.Errors[articleURL]; err != nil {
		return Classification{Kind: ArticleKindUnknown, Reason: "temporary fetch failure"}, err
	}
	kind := f.DefaultKind
	if configured, ok := f.Kinds[articleURL]; ok {
		kind = configured
	}
	if kind == "" {
		kind = ArticleKindUnknown
	}
	return Classification{Kind: kind, Reason: "test classification"}, nil
}

func (f *fakeURLSubmitter) SubmitURL(_ context.Context, articleURL string) (autolayout.Receipt, error) {
	f.URLs = append(f.URLs, articleURL)
	if f.FailNext {
		f.FailNext = false
		return autolayout.Receipt{}, fmt.Errorf("temporary site-sync failure")
	}
	return autolayout.Receipt{JobID: int64(300 + len(f.URLs)), Stage: "enriching"}, nil
}

func openTestService(t *testing.T, submitter URLSubmitter, cfg Config) (*Service, *archive.Store) {
	t.Helper()
	store, err := archive.Open(t.TempDir() + "/callback.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if cfg.Classifier == nil {
		cfg.Classifier = &fakeClassifier{DefaultKind: ArticleKindNormal}
	}
	service, err := New(store, submitter, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func callbackSignature(values ...string) string {
	sort.Strings(values)
	digest := sha1.Sum([]byte(strings.Join(values, "")))
	return hex.EncodeToString(digest[:])
}

func plainEventXML(status string) string {
	return `<xml>
<ToUserName><![CDATA[gh_fbif]]></ToUserName>
<FromUserName><![CDATA[openid]]></FromUserName>
<CreateTime>1784162171</CreateTime>
<MsgType><![CDATA[event]]></MsgType>
<Event><![CDATA[MASSSENDJOBFINISH]]></Event>
<MsgID>2652929817</MsgID>
<Status><![CDATA[` + status + `]]></Status>
<TotalCount>100</TotalCount><FilterCount>99</FilterCount><SentCount>98</SentCount><ErrorCount>1</ErrorCount>
<CopyrightCheckResult><Count>1</Count><ResultList><item>
<ArticleIdx>1</ArticleIdx><UserDeclareState>2</UserDeclareState><AuditState>3</AuditState>
<OriginalArticleUrl><![CDATA[https://example.com/original]]></OriginalArticleUrl><OriginalArticleType>4</OriginalArticleType>
<CanReprint>1</CanReprint><NeedReplaceContent>1</NeedReplaceContent><NeedShowReprintSource>1</NeedShowReprintSource>
</item></ResultList><CheckState>1</CheckState><UnknownFutureField><![CDATA[preserved]]></UnknownFutureField></CopyrightCheckResult>
<ArticleUrlResult><Count>2</Count><ResultList>
<item><ArticleIdx>1</ArticleIdx><ArticleUrl><![CDATA[https://mp.weixin.qq.com/s/first]]></ArticleUrl></item>
<item><ArticleIdx>2</ArticleIdx><ArticleUrl><![CDATA[https://mp.weixin.qq.com/s?__biz=MzA3&mid=2&idx=2&sn=x]]></ArticleUrl></item>
</ResultList></ArticleUrlResult>
</xml>`
}

func postPlain(t *testing.T, service *Service, token, body, signature string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/wechat/publish-callback?timestamp=1784162171&nonce=n-1&signature=" + url.QueryEscape(signature)
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	recorder := httptest.NewRecorder()
	service.ServeHTTP(recorder, req)
	return recorder
}

func TestPlainValidationAndSignatureRejection(t *testing.T) {
	const token = "callback-secret"
	service, _ := openTestService(t, nil, Config{Token: token})
	signature := callbackSignature(token, "123", "nonce")
	req := httptest.NewRequest(http.MethodGet, "/?timestamp=123&nonce=nonce&echostr=verified&signature="+signature, nil)
	recorder := httptest.NewRecorder()
	service.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "verified" {
		t.Fatalf("code=%d body=%q", recorder.Code, recorder.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/?timestamp=123&nonce=nonce&echostr=verified&signature=bad", nil)
	recorder = httptest.NewRecorder()
	service.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature code=%d", recorder.Code)
	}
}

func TestPlainEventPersistsIdempotentlyThenWorkerUsesSiteSync(t *testing.T) {
	const token = "callback-secret"
	fake := &fakeURLSubmitter{}
	service, store := openTestService(t, fake, Config{Token: token})
	body := plainEventXML("send success")
	signature := callbackSignature(token, "1784162171", "n-1")
	for attempt := 0; attempt < 2; attempt++ {
		recorder := postPlain(t, service, token, body, signature)
		if recorder.Code != http.StatusOK || recorder.Body.String() != "success" {
			t.Fatalf("attempt=%d code=%d body=%q", attempt, recorder.Code, recorder.Body.String())
		}
	}
	changedBody := strings.Replace(body, "preserved", "second-payload", 1)
	recorder := postPlain(t, service, token, changedBody, signature)
	if recorder.Code != http.StatusOK {
		t.Fatalf("changed payload code=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if len(fake.URLs) != 0 {
		t.Fatalf("HTTP callback must not synchronously call layout: %#v", fake.URLs)
	}
	stats, err := store.PublishEventStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Events != 1 || stats.Pending != 2 || stats.Delivered != 0 {
		t.Fatalf("duplicate callback must be idempotent: %+v", stats)
	}
	rawXML, eventXML, err := store.PublishEventPayload(context.Background(), "masssendjobfinish:2652929817")
	if err != nil {
		t.Fatal(err)
	}
	if string(rawXML) != changedBody || string(eventXML) != changedBody || !bytes.Contains(rawXML, []byte("UnknownFutureField")) {
		t.Fatalf("full raw XML was not retained")
	}
	history, err := store.PublishEventPayloadHistory(context.Background(), "masssendjobfinish:2652929817")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("distinct callback payloads must both survive: %+v", history)
	}
	totalReceives := history[0].ReceiveCount + history[1].ReceiveCount
	if totalReceives != 3 {
		t.Fatalf("payload retry counts=%d history=%+v", totalReceives, history)
	}
	copyrightArticles, err := store.PublishEventCopyrightArticles(context.Background(), "masssendjobfinish:2652929817")
	if err != nil {
		t.Fatal(err)
	}
	if len(copyrightArticles) != 1 || copyrightArticles[0].UserDeclareState != 2 ||
		copyrightArticles[0].AuditState != 3 || copyrightArticles[0].OriginalArticleURL != "https://example.com/original" ||
		copyrightArticles[0].OriginalArticleType != 4 || copyrightArticles[0].CanReprint != 1 ||
		copyrightArticles[0].NeedReplaceContent != 1 || copyrightArticles[0].NeedShowReprintSource != 1 {
		t.Fatalf("copyright fields=%+v", copyrightArticles)
	}

	result, err := service.RunOnce(context.Background())
	if err != nil || result.Attempted != 2 || result.Delivered != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(fake.URLs) != 2 || fake.URLs[0] != "https://mp.weixin.qq.com/s/first" {
		t.Fatalf("URL deliveries=%#v", fake.URLs)
	}
	stats, _ = store.PublishEventStats(context.Background())
	if stats.Pending != 0 || stats.Delivered != 2 {
		t.Fatalf("delivered stats=%+v", stats)
	}
	recorder = postPlain(t, service, token, body, signature)
	if recorder.Code != http.StatusOK {
		t.Fatalf("post-delivery duplicate code=%d body=%q", recorder.Code, recorder.Body.String())
	}
	again, err := service.RunOnce(context.Background())
	if err != nil || again.Attempted != 0 || len(fake.URLs) != 2 {
		t.Fatalf("delivered callback must stay idempotent: result=%+v calls=%#v err=%v", again, fake.URLs, err)
	}
}

func TestInvalidPostSignatureCannotPersist(t *testing.T) {
	service, store := openTestService(t, nil, Config{Token: "callback-secret"})
	recorder := postPlain(t, service, "callback-secret", plainEventXML("send success"), "invalid")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", recorder.Code)
	}
	stats, err := store.PublishEventStats(context.Background())
	if err != nil || stats.Events != 0 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}

func TestFailedMassSendIsStoredButNeverDelivered(t *testing.T) {
	const token = "callback-secret"
	fake := &fakeURLSubmitter{}
	service, store := openTestService(t, fake, Config{Token: token})
	body := plainEventXML("err(10001)")
	recorder := postPlain(t, service, token, body, callbackSignature(token, "1784162171", "n-1"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", recorder.Code, recorder.Body.String())
	}
	stats, _ := store.PublishEventStats(context.Background())
	if stats.Events != 1 || stats.Ignored != 2 || stats.Pending != 0 {
		t.Fatalf("stats=%+v", stats)
	}
	result, err := service.RunOnce(context.Background())
	if err != nil || result.Attempted != 0 || len(fake.URLs) != 0 {
		t.Fatalf("result=%+v calls=%#v err=%v", result, fake.URLs, err)
	}
}

func TestWorkerPersistsFailureAndRetriesWhenDue(t *testing.T) {
	const token = "callback-secret"
	fake := &fakeURLSubmitter{FailNext: true}
	service, store := openTestService(t, fake, Config{Token: token, RetryDelay: 15 * time.Minute})
	now := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	recorder := postPlain(t, service, token, plainEventXML("send success"), callbackSignature(token, "1784162171", "n-1"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("code=%d", recorder.Code)
	}
	first, err := service.RunOnce(context.Background())
	if err == nil || first.Failed != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	stats, _ := store.PublishEventStats(context.Background())
	if stats.Failed != 1 || stats.Pending != 0 || stats.Delivered != 1 {
		t.Fatalf("stats after failure=%+v", stats)
	}

	now = now.Add(16 * time.Minute)
	second, err := service.RunOnce(context.Background())
	if err != nil || second.Delivered != 1 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	stats, _ = store.PublishEventStats(context.Background())
	if stats.Failed != 0 || stats.Pending != 0 || stats.Delivered != 2 {
		t.Fatalf("stats after retry=%+v", stats)
	}
}

func TestWorkerFailsClosedForNewspicAndUnknownPages(t *testing.T) {
	const token = "callback-secret"
	firstURL := "https://mp.weixin.qq.com/s/first"
	secondURL := "https://mp.weixin.qq.com/s?__biz=MzA3&mid=2&idx=2&sn=x"
	classifier := &fakeClassifier{Kinds: map[string]ArticleKind{
		firstURL:  ArticleKindNormal,
		secondURL: ArticleKindNewspic,
	}}
	fake := &fakeURLSubmitter{}
	service, store := openTestService(t, fake, Config{Token: token, Classifier: classifier})
	body := plainEventXML("send success")
	recorder := postPlain(t, service, token, body, callbackSignature(token, "1784162171", "n-1"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("code=%d", recorder.Code)
	}
	result, err := service.RunOnce(context.Background())
	if err != nil || result.Delivered != 1 || result.Newspic != 1 || result.Held != 0 {
		t.Fatalf("normal/newspic gate result=%+v err=%v", result, err)
	}
	if len(fake.URLs) != 1 || fake.URLs[0] != firstURL {
		t.Fatalf("only normal long article may reach site-sync: %#v", fake.URLs)
	}
	stats, _ := store.PublishEventStats(context.Background())
	if stats.Delivered != 1 || stats.Newspic != 1 {
		t.Fatalf("stats=%+v", stats)
	}

	unknownBody := strings.Replace(body, "2652929817", "2652929818", 1)
	unknownBody = strings.ReplaceAll(unknownBody, "/s/first", "/s/unknown-first")
	unknownBody = strings.ReplaceAll(unknownBody, "mid=2", "mid=3")
	unknownBody = strings.ReplaceAll(unknownBody, "sn=x", "sn=y")
	recorder = postPlain(t, service, token, unknownBody, callbackSignature(token, "1784162171", "n-1"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unknown callback code=%d", recorder.Code)
	}
	unknownResult, err := service.RunOnce(context.Background())
	if err != nil || unknownResult.Held != 2 || unknownResult.Delivered != 0 || unknownResult.Newspic != 0 {
		t.Fatalf("unknown gate result=%+v err=%v", unknownResult, err)
	}
	if len(fake.URLs) != 1 {
		t.Fatalf("unknown pages must never reach site-sync: %#v", fake.URLs)
	}
	stats, _ = store.PublishEventStats(context.Background())
	if stats.Held != 2 || stats.Pending != 0 {
		t.Fatalf("unknown pages must persist as hold: %+v", stats)
	}
	again, err := service.RunOnce(context.Background())
	if err != nil || again.Attempted != 0 {
		t.Fatalf("held pages must not retry automatically: result=%+v err=%v", again, err)
	}

	unexpectedBody := strings.Replace(body, "2652929817", "2652929819", 1)
	unexpectedBody = strings.ReplaceAll(unexpectedBody, "/s/first", "/s/unexpected-first")
	unexpectedBody = strings.ReplaceAll(unexpectedBody, "mid=2", "mid=4")
	unexpectedBody = strings.ReplaceAll(unexpectedBody, "sn=x", "sn=z")
	classifier.Kinds["https://mp.weixin.qq.com/s/unexpected-first"] = ArticleKind("future-type")
	classifier.Kinds["https://mp.weixin.qq.com/s?__biz=MzA3&mid=4&idx=2&sn=z"] = ArticleKind("future-type")
	recorder = postPlain(t, service, token, unexpectedBody, callbackSignature(token, "1784162171", "n-1"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected callback code=%d", recorder.Code)
	}
	unexpectedResult, err := service.RunOnce(context.Background())
	if err != nil || unexpectedResult.Held != 2 || unexpectedResult.Delivered != 0 {
		t.Fatalf("unexpected classifier values must fail closed: result=%+v err=%v", unexpectedResult, err)
	}
	if len(fake.URLs) != 1 {
		t.Fatalf("unexpected classifier values must never reach site-sync: %#v", fake.URLs)
	}
}

func TestWorkerRetriesTransientClassificationFailure(t *testing.T) {
	const token = "callback-secret"
	firstURL := "https://mp.weixin.qq.com/s/first"
	secondURL := "https://mp.weixin.qq.com/s?__biz=MzA3&mid=2&idx=2&sn=x"
	classifier := &fakeClassifier{
		Kinds: map[string]ArticleKind{
			firstURL:  ArticleKindNormal,
			secondURL: ArticleKindNormal,
		},
		Errors: map[string]error{firstURL: fmt.Errorf("temporary timeout")},
	}
	fake := &fakeURLSubmitter{}
	service, store := openTestService(t, fake, Config{Token: token, Classifier: classifier, RetryDelay: 15 * time.Minute})
	now := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	body := plainEventXML("send success")
	recorder := postPlain(t, service, token, body, callbackSignature(token, "1784162171", "n-1"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("code=%d", recorder.Code)
	}
	first, err := service.RunOnce(context.Background())
	if err == nil || first.Failed != 1 || first.Delivered != 1 || first.Held != 0 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	stats, _ := store.PublishEventStats(context.Background())
	if stats.Failed != 1 || stats.Delivered != 1 || stats.Held != 0 {
		t.Fatalf("transient failure must be retryable: %+v", stats)
	}
	delete(classifier.Errors, firstURL)
	now = now.Add(16 * time.Minute)
	second, err := service.RunOnce(context.Background())
	if err != nil || second.Delivered != 1 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	stats, _ = store.PublishEventStats(context.Background())
	if stats.Failed != 0 || stats.Delivered != 2 {
		t.Fatalf("retry stats=%+v", stats)
	}
}

func TestPublicPageMarkerClassification(t *testing.T) {
	tests := []struct {
		name string
		html string
		want ArticleKind
	}{
		{"normal", `<script>var item_show_type = "0"; window.appmsg_type = 9;</script>`, ArticleKindNormal},
		{"newspic", `<script>var item_show_type = '8'; var appmsg_type = "10002";</script>`, ArticleKindNewspic},
		{"missing", `<script>var appmsg_type = 9;</script>`, ArticleKindUnknown},
		{"other", `<script>var item_show_type = 1; var appmsg_type = 9;</script>`, ArticleKindUnknown},
		{"ambiguous", `<script>var item_show_type = 0; item_show_type = 8; var appmsg_type = 9;</script>`, ArticleKindUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyHTML([]byte(test.html))
			if got.Kind != test.want {
				t.Fatalf("classification=%+v want=%s", got, test.want)
			}
		})
	}
}

func TestEncryptedValidationAndEvent(t *testing.T) {
	const (
		token = "callback-secret"
		appID = "wx-fbif-app"
	)
	key := []byte("0123456789abcdef0123456789abcdef")
	encodingKey := strings.TrimSuffix(base64.StdEncoding.EncodeToString(key), "=")
	service, store := openTestService(t, nil, Config{Token: token, EncodingAESKey: encodingKey, AppID: appID})

	encryptedEcho := encryptForTest(t, key, []byte("encrypted-verified"), appID)
	getSignature := callbackSignature(token, "123", "nonce", encryptedEcho)
	getTarget := "/?timestamp=123&nonce=nonce&echostr=" + url.QueryEscape(encryptedEcho) + "&msg_signature=" + getSignature
	recorder := httptest.NewRecorder()
	service.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, getTarget, nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "encrypted-verified" {
		t.Fatalf("GET code=%d body=%q", recorder.Code, recorder.Body.String())
	}

	eventXML := plainEventXML("send success")
	encryptedEvent := encryptForTest(t, key, []byte(eventXML), appID)
	wrapper := `<xml><Encrypt><![CDATA[` + encryptedEvent + `]]></Encrypt></xml>`
	postSignature := callbackSignature(token, "1784162171", "n-1", encryptedEvent)
	postTarget := "/?timestamp=1784162171&nonce=n-1&msg_signature=" + postSignature
	recorder = httptest.NewRecorder()
	service.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, postTarget, strings.NewReader(wrapper)))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "success" {
		t.Fatalf("POST code=%d body=%q", recorder.Code, recorder.Body.String())
	}
	rawXML, decryptedXML, err := store.PublishEventPayload(context.Background(), "masssendjobfinish:2652929817")
	if err != nil {
		t.Fatal(err)
	}
	if string(rawXML) != wrapper || string(decryptedXML) != eventXML {
		t.Fatalf("encrypted transport and decrypted event were not both retained")
	}
}

func encryptForTest(t *testing.T, key, message []byte, appID string) string {
	t.Helper()
	plain := bytes.Repeat([]byte{0x42}, 16)
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(message)))
	plain = append(plain, length...)
	plain = append(plain, message...)
	plain = append(plain, appID...)
	padding := 32 - len(plain)%32
	plain = append(plain, bytes.Repeat([]byte{byte(padding)}, padding)...)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(encrypted, plain)
	return base64.StdEncoding.EncodeToString(encrypted)
}
