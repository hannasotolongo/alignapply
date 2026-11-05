package jobs

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type countingTransport struct{ calls int64 }

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	atomic.AddInt64(&c.calls, 1)
	body := `[{"id":"1","text":"Marketing Manager","hostedUrl":"https://jobs.lever.co/acme/1","applyUrl":"https://jobs.lever.co/acme/1/apply","categories":{"location":"Chicago, IL","commitment":"Full-time"},"descriptionPlain":"Requirements: 5+ years of marketing experience."}]`
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func TestLeverPagesAreCachedBetweenSearches(t *testing.T) {
	transport := &countingTransport{}
	provider := NewLeverProvider([]ATSEmployer{{Name: "Acme", Provider: "lever", Site: "acme"}})
	provider.client = &http.Client{Transport: transport}

	request := SearchRequest{TargetRole: "Marketing Manager", Location: "Chicago, IL"}
	if _, err := provider.Search(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Search(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if transport.calls != 1 {
		t.Fatalf("expected the board to be downloaded once, got %d downloads", transport.calls)
	}
}
