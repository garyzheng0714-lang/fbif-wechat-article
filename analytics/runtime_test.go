package analytics

import (
	"context"
	"testing"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
)

func TestSumQuotaCountsUsesIndependentEndpointReservations(t *testing.T) {
	counts := map[string]int{
		"freepublish_batchget":     8,
		"datacube_getarticleread":  126,
		"datacube_getarticleshare": 125,
	}
	if got := sumQuotaCounts(counts); got != 259 {
		t.Fatalf("sum=%d, want 259", got)
	}
}

func TestArchivedQuotaKeyMatchesClientReservationKeys(t *testing.T) {
	if got := archivedQuotaKey("getarticleread"); got != "datacube_getarticleread" {
		t.Fatalf("data cube key=%q", got)
	}
	if got := archivedQuotaKey("freepublish_batchget"); got != "freepublish_batchget" {
		t.Fatalf("content key=%q", got)
	}
}

func TestArchiveWriteRoundNotifiesBaseSync(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	notified := 0
	runtime := &Runtime{
		Store:          store,
		Analytics:      &Collector{Client: &fakeClient{}, Store: store, MaxCalls: 15},
		ArchiveChanged: func() { notified++ },
	}
	if _, err := runtime.RetryDeferred(context.Background()); err != nil {
		t.Fatal(err)
	}
	if notified != 1 {
		t.Fatalf("Base sync notified %d times after one archive round, want 1", notified)
	}
}
