package jobs

import (
	"context"
	"testing"
	"time"
)

type slowProvider struct {
	delay time.Duration
	calls int
}

func (s *slowProvider) Name() string { return "slow" }

func (s *slowProvider) Search(ctx context.Context, request SearchRequest) ([]Job, error) {
	s.calls++
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return []Job{{ID: "a", Title: "Nurse"}}, nil
}

func TestQuickProviderDoesNotWaitForSlowSources(t *testing.T) {
	slow := &slowProvider{delay: 150 * time.Millisecond}
	quick := NewQuickProvider(slow, 20*time.Millisecond, time.Hour)
	request := SearchRequest{TargetRole: "Nurse", Location: "Ocala, FL"}

	started := time.Now()
	jobs, err := quick.Search(context.Background(), request)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("first search = %d jobs, %v; want none yet", len(jobs), err)
	}
	if time.Since(started) > 100*time.Millisecond {
		t.Fatalf("first search waited %s", time.Since(started))
	}

	// The source keeps going in the background; the next search gets it.
	time.Sleep(250 * time.Millisecond)
	jobs, err = quick.Search(context.Background(), request)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("second search = %d jobs, %v; want the cached job", len(jobs), err)
	}
	if slow.calls != 1 {
		t.Fatalf("source was asked %d times, want 1", slow.calls)
	}
}

func TestQuickProviderReturnsFastSources(t *testing.T) {
	quick := NewQuickProvider(&slowProvider{delay: time.Millisecond}, time.Second, time.Hour)
	jobs, err := quick.Search(context.Background(), SearchRequest{TargetRole: "Teacher", Location: "Austin, TX"})
	if err != nil || len(jobs) != 1 {
		t.Fatalf("got %d jobs, %v", len(jobs), err)
	}
}
