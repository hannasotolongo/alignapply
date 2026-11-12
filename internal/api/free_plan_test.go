package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

func TestTrimMatchesForFree(t *testing.T) {
	matches := make([]jobs.JobMatch, 8)
	for i := range matches {
		matches[i] = jobs.JobMatch{
			ID:                  string(rune('a' + i)),
			MissingRequirements: []string{"one", "two", "three", "four", "five"},
			PartialRequirements: []string{"partial"},
		}
	}

	kept, locked := trimMatchesForFree(matches)
	if len(kept) != FreeMatchesShown {
		t.Fatalf("kept %d matches, want %d", len(kept), FreeMatchesShown)
	}
	if locked != 3 {
		t.Fatalf("locked = %d, want 3", locked)
	}
	for _, match := range kept {
		if len(match.MissingRequirements) != FreeMissingShown {
			t.Fatalf("missing = %v, want %d items", match.MissingRequirements, FreeMissingShown)
		}
		if len(match.PartialRequirements) != 0 {
			t.Fatalf("partial requirements should be Pro only, got %v", match.PartialRequirements)
		}
	}
	// The caller's slice is left alone.
	if len(matches[0].MissingRequirements) != 5 || len(matches[0].PartialRequirements) != 1 {
		t.Fatalf("trim changed the original match: %+v", matches[0])
	}
}

func TestTrimMatchesForFreeShortList(t *testing.T) {
	kept, locked := trimMatchesForFree([]jobs.JobMatch{{ID: "a"}, {ID: "b"}})
	if len(kept) != 2 || locked != 0 {
		t.Fatalf("got %d kept, %d locked; want 2, 0", len(kept), locked)
	}
}

func TestStatusRecorder(t *testing.T) {
	plain := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	_, _ = plain.Write([]byte("ok"))
	if plain.status != http.StatusOK {
		t.Fatalf("status after Write = %d, want 200", plain.status)
	}

	failed := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	failed.WriteHeader(http.StatusBadGateway)
	_, _ = failed.Write([]byte("no"))
	if failed.status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", failed.status)
	}
}
