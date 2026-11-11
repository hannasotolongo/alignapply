package jobs

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Timings records how long each part of a search took, so slow searches
// can be measured from the outside (see the Server-Timing header).
type Timings struct {
	mu    sync.Mutex
	parts map[string]time.Duration
}

type timingsKey struct{}

// WithTimings returns a context whose search steps record their durations.
func WithTimings(ctx context.Context) (context.Context, *Timings) {
	t := &Timings{parts: map[string]time.Duration{}}
	return context.WithValue(ctx, timingsKey{}, t), t
}

// RecordTiming adds a step's duration to the search's timings, if any.
func RecordTiming(ctx context.Context, name string, took time.Duration) {
	t, _ := ctx.Value(timingsKey{}).(*Timings)
	if t == nil {
		return
	}
	t.mu.Lock()
	t.parts[name] += took
	t.mu.Unlock()
}

// Header renders the timings as Server-Timing entries ("name;dur=ms").
func (t *Timings) Header() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	names := make([]string, 0, len(t.parts))
	for name := range t.parts {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		clean := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
				return r
			}
			return '-'
		}, strings.ToLower(name))
		out = append(out, clean+";dur="+strconv.FormatInt(t.parts[name].Milliseconds(), 10))
	}
	return strings.Join(out, ", ")
}
