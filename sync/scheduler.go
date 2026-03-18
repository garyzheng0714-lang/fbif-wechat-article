package sync

import (
	"encoding/json"
	"log"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

// RunDailySync syncs data from cursor.newestSyncedDate+1 to yesterday.
func RunDailySync() error {
	cfg := config.Env
	if cfg.WechatAppID == "" || cfg.WechatSecret == "" {
		log.Println("[Scheduler] Skipping sync: WeChat credentials not configured")
		return nil
	}

	if _, err := wechat.GetToken(); err != nil {
		return err
	}

	cursor, err := ReadCursor()
	if err != nil {
		return err
	}

	yesterday := wechat.Yesterday()

	if cursor != nil && cursor.NewestSyncedDate >= yesterday {
		log.Printf("[Scheduler] Already synced up to %s, skipping daily sync", cursor.NewestSyncedDate)
		return nil
	}

	var beginDate string
	if cursor != nil {
		bd, err := wechat.AddDays(cursor.NewestSyncedDate, 1)
		if err != nil {
			return err
		}
		beginDate = bd
	} else {
		beginDate = yesterday
	}

	log.Printf("[Scheduler] Daily sync: %s ~ %s", beginDate, yesterday)
	result, err := RunFullSync(beginDate, yesterday)
	if err != nil {
		return err
	}

	resultJSON, _ := json.Marshal(result)
	log.Printf("[Scheduler] Daily sync complete: %s", string(resultJSON))

	oldestDate := beginDate
	backfillComplete := false
	if cursor != nil {
		oldestDate = cursor.OldestSyncedDate
		backfillComplete = cursor.BackfillComplete
	}

	return WriteCursor(&SyncCursor{
		OldestSyncedDate: oldestDate,
		NewestSyncedDate: yesterday,
		BackfillComplete: backfillComplete,
	})
}

// RunBackfillSync continues from cursor.oldestSyncedDate backwards.
// Each chunk is 7 days. Stops when API returns empty data.
func RunBackfillSync() error {
	cfg := config.Env
	if cfg.WechatAppID == "" || cfg.WechatSecret == "" {
		log.Println("[Scheduler] Skipping backfill: WeChat credentials not configured")
		return nil
	}

	if _, err := wechat.GetToken(); err != nil {
		return err
	}

	cursor, err := ReadCursor()
	if err != nil {
		return err
	}

	if cursor != nil && cursor.BackfillComplete {
		log.Println("[Scheduler] Backfill already complete, skipping")
		return nil
	}

	startFrom := wechat.Yesterday()
	newestSyncedDate := wechat.Yesterday()
	if cursor != nil {
		startFrom = cursor.OldestSyncedDate
		newestSyncedDate = cursor.NewestSyncedDate
	}

	log.Printf("[Scheduler] Starting backfill from %s backwards", startFrom)

	currentEnd, err := wechat.AddDays(startFrom, -1)
	if err != nil {
		return err
	}

	chunkSize := 7

	for i := 0; i < 200; i++ {
		chunkBegin, err := wechat.AddDays(currentEnd, -(chunkSize - 1))
		if err != nil {
			return err
		}

		log.Printf("[Scheduler] Backfill chunk: %s ~ %s", chunkBegin, currentEnd)

		result, err := RunFullSync(chunkBegin, currentEnd)
		if err != nil {
			if _, ok := err.(*wechat.QuotaLimitError); ok {
				log.Println("[Scheduler] API quota limit reached, pausing backfill. Will resume next run.")
				return nil
			}
			return err
		}

		resultJSON, _ := json.Marshal(result)
		log.Printf("[Scheduler] Chunk done: %s", string(resultJSON))

		allEmpty := IsAllEmpty(result)

		if err := WriteCursor(&SyncCursor{
			OldestSyncedDate: chunkBegin,
			NewestSyncedDate: newestSyncedDate,
			BackfillComplete: allEmpty,
		}); err != nil {
			return err
		}

		if allEmpty {
			log.Printf("[Scheduler] Backfill complete: no data before %s", chunkBegin)
			break
		}

		currentEnd, err = wechat.AddDays(chunkBegin, -1)
		if err != nil {
			return err
		}
	}

	log.Println("[Scheduler] Backfill finished")
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
				if err := RunBackfillSync(); err != nil {
					log.Printf("[Scheduler] Cron backfill failed: %v", err)
				}
			case <-stopCh:
				log.Println("[Scheduler] Cron job stopped")
				return
			}
		}
	}()

	log.Println("[Scheduler] Cron job started: daily sync at 09:00 CST")
}
