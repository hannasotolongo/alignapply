package jobs

import (
	"context"
	"testing"
)

type sizedStore struct {
	BoardStore
	bytes int64
}

func (s sizedStore) DatabaseBytes(context.Context) (int64, error) { return s.bytes, nil }

func TestStorageGuardStopsForeignPostingsNearFull(t *testing.T) {
	saved := StorageLimitBytes
	defer func() { StorageLimitBytes = saved; foreignPostingsOff.Store(false) }()
	StorageLimitBytes = 100

	job := Job{Location: "Toronto, ON", Description: "Full posting."}
	checkStorage(context.Background(), sizedStore{bytes: 50})
	finishCrawledJob(&job, Board{ATS: "greenhouse"})
	if job.Description == "" {
		t.Fatal("at 50% a Canadian posting should be kept in full")
	}

	checkStorage(context.Background(), sizedStore{bytes: 85})
	job = Job{Location: "Toronto, ON", Description: "Full posting."}
	finishCrawledJob(&job, Board{ATS: "greenhouse"})
	if job.Description != "" {
		t.Fatal("at 85% postings abroad should be kept as a short record")
	}
	us := Job{Location: "Austin, TX", Description: "Full posting."}
	finishCrawledJob(&us, Board{ATS: "greenhouse"})
	if us.Description == "" {
		t.Fatal("US postings are always kept")
	}

	other := Job{Location: "Berlin, Germany", Description: "Full posting."}
	checkStorage(context.Background(), sizedStore{bytes: 10})
	finishCrawledJob(&other, Board{ATS: "greenhouse"})
	if other.Description != "" {
		t.Fatal("countries Jane doesn't search abroad stay a short record")
	}
}
