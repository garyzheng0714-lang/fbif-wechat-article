package archive

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// 服务在写库时，另一个进程（服务器上临时跑的 wechat-sync collect-once）也打开同一个库。
// 2026-11-06 14:32 服务首轮采集的 SaveFetch 先读后写，被另一个进程夹在中间写了一下，
// SQLite 直接报 database is locked，不排队等，那一条接口结果就丢了。两个进程同时写都要排队成功。
func TestTwoProcessesWritingSameDatabaseWaitForEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	service, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, 400)
	write := func(store *Store, name string) {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_, err := store.SaveFetch(ctx, FetchRecord{
				Endpoint:     name,
				Category:     "test",
				BeginDate:    "2026-11-05",
				EndDate:      "2026-11-05",
				RequestJSON:  []byte(fmt.Sprintf(`{"i":%d}`, i)),
				ResponseJSON: []byte(fmt.Sprintf(`{"list":[{"ref_date":"2026-11-05","n":%d}]}`, i)),
				Success:      true,
				FetchedAt:    time.Now(),
			})
			if err != nil {
				errs <- fmt.Errorf("%s write %d: %w", name, i, err)
				return
			}
		}
	}

	// 第二个进程照现在的命令行那样整个打开（含迁移），和服务同时写。
	wg.Add(1)
	go write(service, "service")
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	wg.Add(1)
	go write(other, "other")
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	count, err := service.QueryInt64(ctx, `SELECT COUNT(*) FROM official_api_fetches`)
	if err != nil {
		t.Fatal(err)
	}
	if count != 200 {
		t.Fatalf("fetches = %d, want 200", count)
	}
}
