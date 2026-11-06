package api

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

func TestMatchPages(t *testing.T) {
	var all []jobs.JobMatch
	for i := 0; i < 45; i++ {
		all = append(all, jobs.JobMatch{ID: strconv.Itoa(i)})
	}
	pages := []struct{ offset, want, next int }{{0, 20, 20}, {20, 20, 40}, {40, 5, 45}, {60, 0, 45}}
	for _, p := range pages {
		rec := httptest.NewRecorder()
		writeMatchPage(rec, all, p.offset, 20)
		var got []jobs.JobMatch
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if len(got) != p.want || rec.Header().Get("X-Jane-Total") != "45" || rec.Header().Get("X-Jane-Next-Offset") != strconv.Itoa(p.next) {
			t.Errorf("offset %d: got %d next=%s total=%s", p.offset, len(got), rec.Header().Get("X-Jane-Next-Offset"), rec.Header().Get("X-Jane-Total"))
		}
		if p.want > 0 && got[0].ID != strconv.Itoa(p.offset) {
			t.Errorf("offset %d starts at %s", p.offset, got[0].ID)
		}
	}
	a := jobs.SearchRequest{TargetRole: "Dentist", Location: "Miami, FL", PageSize: 20}
	b := a
	b.Offset = 40
	if matchPageKey("u1", a) != matchPageKey("u1", b) || matchPageKey("u1", a) == matchPageKey("u2", a) {
		t.Error("page key should ignore the page and depend on the user")
	}
}
