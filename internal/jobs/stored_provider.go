package jobs

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

// StoredJobsProvider searches the jobs Jane's crawler has saved. The
// database narrows by title; the pipeline then applies the exact title,
// location and preference rules.
type StoredJobsProvider struct {
	store func() BoardStore
	limit int
}

// NewStoredJobsProvider takes a function so the store can be connected
// after the provider is built.
func NewStoredJobsProvider(store func() BoardStore) *StoredJobsProvider {
	return &StoredJobsProvider{store: store, limit: 1500}
}

func (p *StoredJobsProvider) Name() string { return "jane-crawler" }

func (p *StoredJobsProvider) Search(ctx context.Context, request SearchRequest) ([]Job, error) {
	if p == nil || p.store == nil {
		return nil, nil
	}
	store := p.store()
	if store == nil {
		return nil, nil
	}
	pattern := TitleSearchPattern(request.TargetRole)
	if pattern == "" {
		return nil, nil
	}
	return store.SearchJobs(ctx, pattern, p.limit)
}

// TitleSearchPattern is a PostgreSQL regular expression that finds every
// title jobTitleMatchesTargetRole could accept for the role: titles with
// one of the role's meaningful words, including short forms (a "registered
// nurse" search also finds "ICU RN").
func TitleSearchPattern(role string) string {
	expanded := expandTitleAliases(normalizePreferenceText(role))
	tokens := roleRelevanceTokens(expanded)
	if len(tokens) == 0 {
		tokens = preferenceTokenSet(expanded)
	}
	if len(tokens) == 0 {
		return ""
	}

	words := make(map[string]struct{}, len(tokens))
	for token := range tokens {
		words[token] = struct{}{}
	}
	for short, expansion := range titleAliases {
		for _, word := range strings.Fields(expansion) {
			if _, ok := tokens[word]; ok {
				words[short] = struct{}{}
			}
		}
	}

	list := make([]string, 0, len(words))
	for word := range words {
		list = append(list, regexp.QuoteMeta(word))
	}
	sort.Strings(list)
	return `\m(` + strings.Join(list, "|") + `)`
}
