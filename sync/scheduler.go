package sync

import (
	"log"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

// RunDailySync refreshes the published-article table from the official
// freepublish API. It always scans recent pages first and keeps historical
// progress in cursor.Published* fields.
func RunDailySync() error {
	cfg := config.Env
	if cfg.WechatAppID == "" || cfg.WechatSecret == "" {
		log.Println("[Scheduler] Skipping sync: WeChat credentials not configured")
		return nil
	}

	if _, err := wechat.GetToken(); err != nil {
		return err
	}

	log.Printf("[Scheduler] Refreshing published articles on %s", time.Now().In(wechat.ShanghaiLoc()).Format("2006-01-02"))
	result, err := SyncPublishedArticles()
	if err != nil {
		return err
	}
	log.Printf("[Scheduler] Published article sync done: pages=%d records=%d created=%d updated=%d",
		result.PagesScanned, result.RecordsSeen, result.Created, result.Updated)
	return nil
}

// StartScheduler starts the daily cron job at 09:00 CST.
func StartScheduler(stopCh <-chan struct{}) {
	loc := wechat.ShanghaiLoc()

	go func() {
		for {
			now := time.Now().In(loc)
			next := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, loc)
			if !next.After(now) {
				next = next.AddDate(0, 0, 1)
			}
			delay := next.Sub(now)
			log.Printf("[Scheduler] Next sync at %s (in %s)", next.Format("2006-01-02 15:04:05"), delay.Round(time.Second))

			select {
			case <-time.After(delay):
				log.Printf("[Scheduler] Cron triggered at %s", time.Now().In(loc).Format(time.RFC3339))
				if err := RunDailySync(); err != nil {
					log.Printf("[Scheduler] Cron daily sync failed: %v", err)
				}
			case <-stopCh:
				log.Println("[Scheduler] Cron job stopped")
				return
			}
		}
	}()

	log.Println("[Scheduler] Cron job started: daily sync at 09:00 CST")
}
