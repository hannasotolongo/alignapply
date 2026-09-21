package matching

import (
	"math"
	"sort"
	"strings"
	"sync"
)

type SemanticProvider interface {
	Similarity(requirement string, evidence string) (float64, error)
}

// BatchSemanticProvider lets a provider score all evidence for one requirement
// in a single operation. Ollama uses this to make one embedding request instead
// of one HTTP request per evidence statement.
type BatchSemanticProvider interface {
	Similarities(requirement string, evidence []string) ([]float64, error)
}

type EvidenceCandidate struct {
	Evidence string
	Score    float64
}

type SemanticRetriever interface {
	Retrieve(
		requirement string,
		evidence []string,
		limit int,
	) []EvidenceCandidate
}

type HybridSemanticRetriever struct {
	Provider SemanticProvider
}

func (r HybridSemanticRetriever) Retrieve(
	requirement string,
	evidence []string,
	limit int,
) []EvidenceCandidate {
	requirement = normalizeSemanticText(requirement)

	if requirement == "" || len(evidence) == 0 || limit <= 0 {
		return nil
	}

	cleaned := make([]string, 0, len(evidence))

	for _, item := range evidence {
		item = normalizeSemanticText(item)
		if item != "" {
			cleaned = append(cleaned, item)
		}
	}

	if len(cleaned) == 0 {
		return nil
	}

	semanticScores := make([]float64, len(cleaned))

	if batchProvider, ok := r.Provider.(BatchSemanticProvider); ok {
		if scores, err := batchProvider.Similarities(
			requirement,
			cleaned,
		); err == nil && len(scores) == len(cleaned) {
			copy(semanticScores, scores)
		}
	} else if r.Provider != nil {
		for i, item := range cleaned {
			score, err := r.Provider.Similarity(
				requirement,
				item,
			)

			if err == nil {
				semanticScores[i] = score
			}
		}
	}

	candidates := make(
		[]EvidenceCandidate,
		0,
		len(cleaned),
	)

	for i, item := range cleaned {
		lexical := tokenCoverage(
			requirement,
			item,
		)

		semantic := clamp01(
			semanticScores[i],
		)

		score := lexical

		if semantic > score {
			score = semantic
		}

		candidates = append(
			candidates,
			EvidenceCandidate{
				Evidence: item,
				Score:    score,
			},
		)
	}

	sort.SliceStable(
		candidates,
		func(i, j int) bool {
			return candidates[i].Score >
				candidates[j].Score
		},
	)

	if limit > len(candidates) {
		limit = len(candidates)
	}

	return candidates[:limit]
}

var (
	semanticRetrieverMu sync.RWMutex

	defaultSemanticRetriever SemanticRetriever = HybridSemanticRetriever{}
)

func SetSemanticRetriever(
	retriever SemanticRetriever,
) {
	semanticRetrieverMu.Lock()
	defer semanticRetrieverMu.Unlock()

	if retriever == nil {
		defaultSemanticRetriever =
			HybridSemanticRetriever{}
		return
	}

	defaultSemanticRetriever = retriever
}

func currentSemanticRetriever() SemanticRetriever {
	semanticRetrieverMu.RLock()
	defer semanticRetrieverMu.RUnlock()

	return defaultSemanticRetriever
}

func normalizeSemanticText(text string) string {
	return strings.Join(
		strings.Fields(
			strings.TrimSpace(text),
		),
		" ",
	)
}

func cosineSimilarity(
	left,
	right []float64,
) float64 {
	if len(left) == 0 ||
		len(left) != len(right) {
		return 0
	}

	var dot float64
	var leftNorm float64
	var rightNorm float64

	for i := range left {
		dot += left[i] * right[i]
		leftNorm += left[i] * left[i]
		rightNorm += right[i] * right[i]
	}

	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}

	return clamp01(
		dot /
			(math.Sqrt(leftNorm) *
				math.Sqrt(rightNorm)),
	)
}

func clamp01(value float64) float64 {
	switch {
	case value < 0:
		return 0

	case value > 1:
		return 1

	default:
		return value
	}
}
