package jobs

import (
	"context"
	"testing"
	"time"
)

type countingProvider struct{ calls int }

func (p *countingProvider) Name() string { return "counting" }
func (p *countingProvider) Search(context.Context, SearchRequest) ([]Job, error) {
	p.calls++
	return []Job{{ID: "1", Title: "RN", Company: "Acme"}}, nil
}

func TestCachedProviderReusesResults(t *testing.T) {
	inner := &countingProvider{}
	cache := NewCachedProvider(inner, time.Hour)
	ctx := context.Background()

	cache.Search(ctx, SearchRequest{TargetRole: "Registered Nurse", Location: "Miami, FL", Remote: true})
	cache.Search(ctx, SearchRequest{TargetRole: "registered  nurse", Location: "miami, fl", Onsite: true})
	if inner.calls != 1 {
		t.Fatalf("expected 1 upstream call for the same role and place, got %d", inner.calls)
	}
	cache.Search(ctx, SearchRequest{TargetRole: "Registered Nurse", Location: "Tampa, FL"})
	if inner.calls != 2 {
		t.Fatalf("a different place should search again, got %d calls", inner.calls)
	}

	expired := NewCachedProvider(inner, -time.Second)
	expired.Search(ctx, SearchRequest{TargetRole: "x"})
	expired.Search(ctx, SearchRequest{TargetRole: "x"})
	if inner.calls != 4 {
		t.Fatalf("expired entries should refetch, got %d calls", inner.calls)
	}
}
