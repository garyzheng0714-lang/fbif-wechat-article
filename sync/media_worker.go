package sync

import (
	"log"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	stdsync "sync"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

var mediaWorkerMu stdsync.Mutex

type mediaCandidate struct {
	recordID      string
	uniqueKey     string
	articleURL    string
	html          string
	missingCover  bool
	missingImages bool
	sortTs        int64
}

type MediaSyncResult struct {
	Scanned      int `json:"scanned"`
	Updated      int `json:"updated"`
	CoverUpdated int `json:"coverUpdated"`
	ImageUpdated int `json:"imageUpdated"`
}

func StartMediaWorker(stopCh <-chan struct{}) {
	if !mediaWorkerEnabled() {
		log.Println("[MediaWorker] Disabled")
		return
	}

	go func() {
		delay := mediaWorkerInitialDelay()
		timer := time.NewTimer(delay)
		defer timer.Stop()

		for {
			select {
			case <-timer.C:
				if result, err := RunMediaWorkerOnce(); err != nil {
					log.Printf("[MediaWorker] Run failed: %v", err)
				} else if result != nil && result.Scanned > 0 {
					log.Printf("[MediaWorker] Done: scanned=%d updated=%d cover=%d images=%d",
						result.Scanned, result.Updated, result.CoverUpdated, result.ImageUpdated)
				}
				timer.Reset(mediaWorkerInterval())
			case <-stopCh:
				log.Println("[MediaWorker] Stopped")
				return
			}
		}
	}()

	log.Printf("[MediaWorker] Started: interval=%s initial_delay=%s",
		mediaWorkerInterval().Round(time.Second), mediaWorkerInitialDelay().Round(time.Second))
}

func RunMediaWorkerOnce() (*MediaSyncResult, error) {
	defer debug.FreeOSMemory()

	if !mediaWorkerMu.TryLock() {
		log.Println("[MediaWorker] Already running, skipping")
		return &MediaSyncResult{}, nil
	}
	defer mediaWorkerMu.Unlock()

	tableID, err := PublishedArticleTableID()
	if err != nil {
		return nil, err
	}

	candidates, err := listMediaCandidates(tableID)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return &MediaSyncResult{}, nil
	}

	limit := mediaWorkerBatchSize()
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	type output struct {
		recordID string
		fields   map[string]interface{}
		err      error
	}

	jobs := make(chan mediaCandidate, len(candidates))
	results := make(chan output, len(candidates))
	workers := mediaWorkerConcurrency()

	var wg stdsync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range jobs {
				fields, err := enrichMediaCandidate(c)
				results <- output{recordID: c.recordID, fields: fields, err: err}
			}
		}()
	}

	for _, c := range candidates {
		jobs <- c
	}
	close(jobs)
	wg.Wait()
	close(results)

	result := &MediaSyncResult{Scanned: len(candidates)}
	var updates []map[string]interface{}
	for out := range results {
		if out.err != nil {
			log.Printf("[MediaWorker] Candidate failed %s: %v", out.recordID, out.err)
			continue
		}
		if len(out.fields) == 0 {
			continue
		}
		if _, ok := out.fields["封面图链接"]; ok {
			result.CoverUpdated++
		}
		if _, ok := out.fields["正文图片链接"]; ok {
			result.ImageUpdated++
		}
		updates = append(updates, map[string]interface{}{
			"record_id": out.recordID,
			"fields":    out.fields,
		})
	}

	if len(updates) > 0 {
		if err := feishu.BatchUpdateByRecordID(tableID, updates); err != nil {
			return nil, err
		}
		result.Updated = len(updates)
	}

	return result, nil
}

func listMediaCandidates(tableID string) ([]mediaCandidate, error) {
	records, err := feishu.ListRecords(tableID, []string{
		"唯一键", "文章链接", "正文HTML", "封面图链接", "正文图片链接", "更新时间戳", "同步时间",
	})
	if err != nil {
		return nil, err
	}

	var candidates []mediaCandidate
	for _, record := range records {
		articleURL := feishu.FieldString(record.Fields, "文章链接")
		html := feishu.FieldString(record.Fields, "正文HTML")
		cover := feishu.FieldString(record.Fields, "封面图链接")
		images := feishu.FieldString(record.Fields, "正文图片链接")
		missingCover := articleURL != "" && cover == ""
		missingImages := html != "" && images == ""
		if !missingCover && !missingImages {
			continue
		}

		sortTs := feishu.FieldInt64(record.Fields, "更新时间戳")
		if sortTs == 0 {
			sortTs = feishu.FieldInt64(record.Fields, "同步时间")
		}

		candidates = append(candidates, mediaCandidate{
			recordID:      record.RecordID,
			uniqueKey:     feishu.FieldString(record.Fields, "唯一键"),
			articleURL:    articleURL,
			html:          html,
			missingCover:  missingCover,
			missingImages: missingImages,
			sortTs:        sortTs,
		})
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].sortTs > candidates[j].sortTs })
	return candidates, nil
}

func enrichMediaCandidate(c mediaCandidate) (map[string]interface{}, error) {
	fields := map[string]interface{}{}

	if c.missingCover {
		meta, err := wechat.FetchArticleMetadata(c.articleURL)
		if err != nil {
			return nil, err
		}
		if meta != nil && meta.CoverURL != "" {
			coverURL := meta.CoverURL
			if mirrored, err := wechat.MirrorRemoteImage(meta.CoverURL); err == nil && mirrored != "" {
				coverURL = mirrored
			} else if err != nil {
				return nil, err
			}
			fields["封面图链接"] = map[string]string{"link": coverURL, "text": coverURL}
		}
	}

	if c.missingImages {
		imageURLs, err := wechat.MirrorArticleImages(c.html)
		if err != nil {
			return nil, err
		}
		if len(imageURLs) > 0 {
			fields["正文图片链接"] = strings.Join(imageURLs, ",")
		}
	}

	return fields, nil
}

func mediaWorkerEnabled() bool {
	v := strings.TrimSpace(os.Getenv("ENABLE_MEDIA_WORKER"))
	if v == "" {
		return true
	}
	return v == "1" || strings.EqualFold(v, "true")
}

func mediaWorkerBatchSize() int {
	return envIntDefault("MEDIA_WORKER_BATCH_SIZE", 10)
}

func mediaWorkerConcurrency() int {
	return envIntDefault("MEDIA_WORKER_CONCURRENCY", 2)
}

func mediaWorkerInitialDelay() time.Duration {
	return time.Duration(envIntDefault("MEDIA_WORKER_INITIAL_DELAY_SECONDS", 60)) * time.Second
}

func mediaWorkerInterval() time.Duration {
	return time.Duration(envIntDefault("MEDIA_WORKER_INTERVAL_MINUTES", 30)) * time.Minute
}

func envIntDefault(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
