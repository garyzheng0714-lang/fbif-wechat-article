package publishcallback

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1" // #nosec G505 -- WeChat callback signatures require SHA-1.
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/autolayout"
)

const (
	massSendJobFinish = "MASSSENDJOBFINISH"
	maxCallbackBody   = 2 << 20
)

type URLSubmitter interface {
	SubmitURL(ctx context.Context, articleURL string) (autolayout.Receipt, error)
}

type Config struct {
	Token          string
	EncodingAESKey string
	AppID          string
	Classifier     URLClassifier
	RetryDelay     time.Duration
	WorkerInterval time.Duration
	BatchSize      int
}

type Service struct {
	store          *archive.Store
	submitter      URLSubmitter
	classifier     URLClassifier
	token          string
	appID          string
	aesKey         []byte
	retryDelay     time.Duration
	workerInterval time.Duration
	batchSize      int
	now            func() time.Time
	runMu          sync.Mutex
}

type RunResult struct {
	Attempted int
	Delivered int
	Failed    int
	Held      int
	Newspic   int
}

type encryptedEnvelope struct {
	Encrypt string `xml:"Encrypt"`
}

type callbackEvent struct {
	ToUserName   string `xml:"ToUserName"`
	FromUserName string `xml:"FromUserName"`
	CreateTime   int64  `xml:"CreateTime"`
	MsgType      string `xml:"MsgType"`
	Event        string `xml:"Event"`
	MsgID        string `xml:"MsgID"`
	Status       string `xml:"Status"`
	TotalCount   int64  `xml:"TotalCount"`
	FilterCount  int64  `xml:"FilterCount"`
	SentCount    int64  `xml:"SentCount"`
	ErrorCount   int64  `xml:"ErrorCount"`
	ArticleURLs  struct {
		Items []struct {
			ArticleIndex int    `xml:"ArticleIdx"`
			ArticleURL   string `xml:"ArticleUrl"`
		} `xml:"ResultList>item"`
	} `xml:"ArticleUrlResult"`
	CopyrightCheck struct {
		Count      int64 `xml:"Count"`
		CheckState int64 `xml:"CheckState"`
		Items      []struct {
			ArticleIndex          int    `xml:"ArticleIdx"`
			UserDeclareState      int64  `xml:"UserDeclareState"`
			AuditState            int64  `xml:"AuditState"`
			OriginalArticleURL    string `xml:"OriginalArticleUrl"`
			OriginalArticleType   int64  `xml:"OriginalArticleType"`
			CanReprint            int64  `xml:"CanReprint"`
			NeedReplaceContent    int64  `xml:"NeedReplaceContent"`
			NeedShowReprintSource int64  `xml:"NeedShowReprintSource"`
		} `xml:"ResultList>item"`
	} `xml:"CopyrightCheckResult"`
}

func New(store *archive.Store, submitter URLSubmitter, cfg Config) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("publish callback store is required")
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, fmt.Errorf("publish callback token is required")
	}
	service := &Service{
		store:          store,
		submitter:      submitter,
		classifier:     cfg.Classifier,
		token:          token,
		appID:          strings.TrimSpace(cfg.AppID),
		retryDelay:     cfg.RetryDelay,
		workerInterval: cfg.WorkerInterval,
		batchSize:      cfg.BatchSize,
	}
	if service.retryDelay <= 0 {
		service.retryDelay = 15 * time.Minute
	}
	if service.workerInterval <= 0 {
		service.workerInterval = 5 * time.Second
	}
	if service.batchSize <= 0 {
		service.batchSize = 20
	}
	if service.classifier == nil {
		service.classifier = &HTTPPageClassifier{}
	}
	if rawKey := strings.TrimSpace(cfg.EncodingAESKey); rawKey != "" {
		key, err := decodeEncodingAESKey(rawKey)
		if err != nil {
			return nil, err
		}
		service.aesKey = key
	}
	return service, nil
}

func NewFromEnv(store *archive.Store, submitter URLSubmitter, defaultAppID string) (*Service, error) {
	token := strings.TrimSpace(os.Getenv("WECHAT_CALLBACK_TOKEN"))
	if token == "" {
		return nil, nil
	}
	appID := strings.TrimSpace(os.Getenv("WECHAT_CALLBACK_APP_ID"))
	if appID == "" {
		appID = strings.TrimSpace(defaultAppID)
	}
	return New(store, submitter, Config{
		Token:          token,
		EncodingAESKey: strings.TrimSpace(os.Getenv("WECHAT_CALLBACK_AES_KEY")),
		AppID:          appID,
	})
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleValidation(w, r)
	case http.MethodPost:
		s.handleEvent(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleValidation(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	timestamp := query.Get("timestamp")
	nonce := query.Get("nonce")
	echo := query.Get("echostr")
	if timestamp == "" || nonce == "" || echo == "" {
		http.Error(w, "missing callback validation parameters", http.StatusBadRequest)
		return
	}
	if msgSignature := query.Get("msg_signature"); msgSignature != "" {
		if len(s.aesKey) == 0 || !validSignature(msgSignature, s.token, timestamp, nonce, echo) {
			http.Error(w, "invalid callback signature", http.StatusUnauthorized)
			return
		}
		plaintext, err := decryptMessage(s.aesKey, echo, s.appID)
		if err != nil {
			http.Error(w, "invalid encrypted callback", http.StatusUnauthorized)
			return
		}
		echo = string(plaintext)
	} else if !validSignature(query.Get("signature"), s.token, timestamp, nonce) {
		http.Error(w, "invalid callback signature", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, echo)
}

func (s *Service) handleEvent(w http.ResponseWriter, r *http.Request) {
	timestamp := r.URL.Query().Get("timestamp")
	nonce := r.URL.Query().Get("nonce")
	if timestamp == "" || nonce == "" {
		http.Error(w, "missing callback signature parameters", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCallbackBody)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "callback body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "read callback body failed", http.StatusBadRequest)
		}
		return
	}
	if len(rawBody) == 0 || strings.Contains(strings.ToUpper(string(rawBody)), "<!DOCTYPE") {
		http.Error(w, "invalid callback XML", http.StatusBadRequest)
		return
	}
	eventXML := rawBody
	var envelope encryptedEnvelope
	if err := xml.Unmarshal(rawBody, &envelope); err != nil {
		http.Error(w, "invalid callback XML", http.StatusBadRequest)
		return
	}
	encrypted := strings.TrimSpace(envelope.Encrypt)
	if encrypted != "" {
		msgSignature := r.URL.Query().Get("msg_signature")
		if len(s.aesKey) == 0 || !validSignature(msgSignature, s.token, timestamp, nonce, encrypted) {
			http.Error(w, "invalid callback signature", http.StatusUnauthorized)
			return
		}
		eventXML, err = decryptMessage(s.aesKey, encrypted, s.appID)
		if err != nil {
			http.Error(w, "invalid encrypted callback", http.StatusUnauthorized)
			return
		}
	} else if r.URL.Query().Get("msg_signature") != "" || !validSignature(r.URL.Query().Get("signature"), s.token, timestamp, nonce) {
		http.Error(w, "invalid callback signature", http.StatusUnauthorized)
		return
	}
	var parsed callbackEvent
	if err := xml.Unmarshal(eventXML, &parsed); err != nil {
		http.Error(w, "invalid event XML", http.StatusBadRequest)
		return
	}
	if !strings.EqualFold(strings.TrimSpace(parsed.Event), massSendJobFinish) {
		writeSuccess(w)
		return
	}
	if !strings.EqualFold(strings.TrimSpace(parsed.MsgType), "event") || strings.TrimSpace(parsed.MsgID) == "" {
		http.Error(w, "invalid mass-send event", http.StatusBadRequest)
		return
	}
	event := archive.PublishEvent{
		EventKey:            "masssendjobfinish:" + strings.TrimSpace(parsed.MsgID),
		EventType:           massSendJobFinish,
		MsgID:               strings.TrimSpace(parsed.MsgID),
		ToUserName:          parsed.ToUserName,
		FromUserName:        parsed.FromUserName,
		CreateTime:          parsed.CreateTime,
		MsgType:             parsed.MsgType,
		Status:              parsed.Status,
		TotalCount:          parsed.TotalCount,
		FilterCount:         parsed.FilterCount,
		SentCount:           parsed.SentCount,
		ErrorCount:          parsed.ErrorCount,
		CopyrightCount:      parsed.CopyrightCheck.Count,
		CopyrightCheckState: parsed.CopyrightCheck.CheckState,
		RawXML:              append([]byte(nil), rawBody...),
		EventXML:            append([]byte(nil), eventXML...),
	}
	for _, item := range parsed.CopyrightCheck.Items {
		if item.ArticleIndex <= 0 {
			continue
		}
		event.CopyrightArticles = append(event.CopyrightArticles, archive.PublishEventCopyrightArticle{
			ArticleIndex:          item.ArticleIndex,
			UserDeclareState:      item.UserDeclareState,
			AuditState:            item.AuditState,
			OriginalArticleURL:    strings.TrimSpace(item.OriginalArticleURL),
			OriginalArticleType:   item.OriginalArticleType,
			CanReprint:            item.CanReprint,
			NeedReplaceContent:    item.NeedReplaceContent,
			NeedShowReprintSource: item.NeedShowReprintSource,
		})
	}
	eligible := strings.EqualFold(strings.TrimSpace(parsed.Status), "send success")
	for ordinal, item := range parsed.ArticleURLs.Items {
		articleURL := strings.TrimSpace(item.ArticleURL)
		sourceKey, err := autolayout.CanonicalSourceKey(articleURL)
		if err != nil {
			continue
		}
		articleIndex := item.ArticleIndex
		if articleIndex <= 0 {
			articleIndex = ordinal + 1
		}
		event.Articles = append(event.Articles, archive.PublishEventArticle{
			ArticleIndex: articleIndex,
			SourceKey:    sourceKey,
			SourceURL:    articleURL,
			Eligible:     eligible,
		})
	}
	if err := s.store.SavePublishEvent(r.Context(), event, s.nowTime()); err != nil {
		http.Error(w, "persist callback failed", http.StatusInternalServerError)
		return
	}
	writeSuccess(w)
}

func writeSuccess(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "success")
}

func validSignature(got string, values ...string) bool {
	if got == "" {
		return false
	}
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	digest := sha1.Sum([]byte(strings.Join(sorted, "")))
	want := hex.EncodeToString(digest[:])
	return len(got) == len(want) && subtle.ConstantTimeCompare([]byte(strings.ToLower(got)), []byte(want)) == 1
}

func decodeEncodingAESKey(raw string) ([]byte, error) {
	if len(raw) != 43 {
		return nil, fmt.Errorf("WECHAT_CALLBACK_AES_KEY must contain 43 characters")
	}
	key, err := base64.StdEncoding.DecodeString(raw + "=")
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("WECHAT_CALLBACK_AES_KEY is invalid")
	}
	return key, nil
}

func decryptMessage(key []byte, ciphertext, expectedAppID string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(ciphertext))
	if err != nil || len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid encrypted payload")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(plain, data)
	plain, err = unpadPKCS7(plain)
	if err != nil || len(plain) < 20 {
		return nil, fmt.Errorf("invalid encrypted payload padding")
	}
	messageLength := int(binary.BigEndian.Uint32(plain[16:20]))
	if messageLength < 0 || 20+messageLength > len(plain) {
		return nil, fmt.Errorf("invalid encrypted payload length")
	}
	message := append([]byte(nil), plain[20:20+messageLength]...)
	appID := string(plain[20+messageLength:])
	if expectedAppID != "" && subtle.ConstantTimeCompare([]byte(appID), []byte(expectedAppID)) != 1 {
		return nil, fmt.Errorf("encrypted callback app id mismatch")
	}
	return message, nil
}

func unpadPKCS7(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty padded payload")
	}
	padding := int(data[len(data)-1])
	if padding <= 0 || padding > 32 || padding > len(data) {
		return nil, fmt.Errorf("invalid PKCS#7 padding")
	}
	for _, value := range data[len(data)-padding:] {
		if int(value) != padding {
			return nil, fmt.Errorf("invalid PKCS#7 padding")
		}
	}
	return data[:len(data)-padding], nil
}

func (s *Service) RunOnce(ctx context.Context) (*RunResult, error) {
	if s.submitter == nil {
		return &RunResult{}, nil
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	now := s.nowTime()
	deliveries, err := s.store.ListDuePublishEventDeliveries(ctx, now, s.batchSize)
	if err != nil {
		return nil, err
	}
	result := &RunResult{}
	var runErrors []error
	for _, delivery := range deliveries {
		result.Attempted++
		classifyCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		classification, classifyErr := s.classifier.Classify(classifyCtx, delivery.SourceURL)
		cancel()
		attemptedAt := s.nowTime()
		if classifyErr != nil {
			reason := classification.Reason
			reason = fmt.Sprintf("%s: %v", reason, classifyErr)
			if strings.TrimSpace(reason) == "" {
				reason = "public page classification failed"
			}
			result.Failed++
			runErrors = append(runErrors, classifyErr)
			if err := s.store.MarkPublishEventFailed(
				ctx,
				delivery.EventKey,
				delivery.ArticleIndex,
				reason,
				attemptedAt.Add(s.retryDelay),
				attemptedAt,
			); err != nil {
				runErrors = append(runErrors, err)
			}
			continue
		}
		if classification.Kind != ArticleKindNormal && classification.Kind != ArticleKindNewspic {
			reason := strings.TrimSpace(classification.Reason)
			if reason == "" {
				reason = "public page type is unclassified"
			}
			if err := s.store.MarkPublishEventClassified(
				ctx,
				delivery.EventKey,
				delivery.ArticleIndex,
				archive.PublishEventArticleHeld,
				reason,
				attemptedAt,
			); err != nil {
				runErrors = append(runErrors, err)
				continue
			}
			result.Held++
			continue
		}
		if classification.Kind == ArticleKindNewspic {
			if err := s.store.MarkPublishEventClassified(
				ctx,
				delivery.EventKey,
				delivery.ArticleIndex,
				archive.PublishEventArticleNewspic,
				classification.Reason,
				attemptedAt,
			); err != nil {
				runErrors = append(runErrors, err)
				continue
			}
			result.Newspic++
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		receipt, callErr := s.submitter.SubmitURL(callCtx, delivery.SourceURL)
		cancel()
		attemptedAt = s.nowTime()
		if callErr != nil {
			result.Failed++
			runErrors = append(runErrors, callErr)
			if persistErr := s.store.MarkPublishEventFailed(
				ctx,
				delivery.EventKey,
				delivery.ArticleIndex,
				callErr.Error(),
				attemptedAt.Add(s.retryDelay),
				attemptedAt,
			); persistErr != nil {
				runErrors = append(runErrors, persistErr)
			}
			continue
		}
		if err := s.store.MarkPublishEventDelivered(
			ctx,
			delivery.EventKey,
			delivery.ArticleIndex,
			receipt.JobID,
			receipt.Stage,
			receipt.Existing,
			attemptedAt,
		); err != nil {
			runErrors = append(runErrors, err)
			continue
		}
		result.Delivered++
	}
	return result, errors.Join(runErrors...)
}

func (s *Service) Start(stopCh <-chan struct{}) {
	if s == nil || s.submitter == nil {
		return
	}
	go func() {
		run := func() {
			result, err := s.RunOnce(context.Background())
			if result != nil && result.Attempted > 0 {
				log.Printf("[PublishCallback] attempted=%d delivered=%d failed=%d held=%d skipped_newspic=%d", result.Attempted, result.Delivered, result.Failed, result.Held, result.Newspic)
			}
			if err != nil {
				log.Printf("[PublishCallback] worker completed with errors: %v", err)
			}
		}
		run()
		ticker := time.NewTicker(s.workerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				run()
			case <-stopCh:
				return
			}
		}
	}()
}

func (s *Service) nowTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}
