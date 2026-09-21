package matching

import (
	"math"
	"sort"
	"strings"
	"sync"
)

// SemanticProvider provides semantic similarity between two pieces of text.
//
// Providers such as Ollama are responsible only for estimating semantic
// relatedness. A similarity score is not treated as proof that candidate
// evidence satisfies a job requirement.
type SemanticProvider interface {
	Similarity(requirement string, evidence string) (float64, error)
}

// EvidenceCandidate represents one piece of candidate evidence retrieved for a
// requirement.
//
// Score is a retrieval score only. It must not be interpreted as Supported,
// Partial, or Not Clearly Demonstrated.
type EvidenceCandidate struct {
	Evidence string
	Score    float64
}

// SemanticRetriever finds the evidence most relevant to a requirement.
//
// Retrieval deliberately remains separate from verification. The retriever
// answers:
//
//	"What evidence should we inspect?"
//
// A verifier will separately answer:
//
//	"Does this evidence actually demonstrate the requirement?"
type SemanticRetriever interface {
	Retrieve(
		requirement string,
		evidence []string,
		limit int,
	) []EvidenceCandidate
}

// HybridSemanticRetriever combines embedding similarity with conservative
// lexical matching.
//
// Semantic scores are used for ranking evidence, not for making the final
// support decision.
type HybridSemanticRetriever struct {
	Provider SemanticProvider
}

// score returns a retrieval score for one requirement/evidence pair.
//
// If the semantic provider is unavailable, lexical coverage provides a
// deterministic fallback.
func (r HybridSemanticRetriever) score(
	requirement string,
	evidence string,
) float64 {
	lexical := tokenCoverage(requirement, evidence)

	if r.Provider == nil {
		return lexical
	}

	semantic, err := r.Provider.Similarity(
		normalizeSemanticText(requirement),
		normalizeSemanticText(evidence),
	)
	if err != nil {
		return lexical
	}

	semantic = clamp01(semantic)

	if semantic > lexical {
		return semantic
	}

	return lexical
}

// Retrieve ranks candidate evidence by relevance.
//
// No fixed semantic threshold is used here. Real paraphrases can have modest
// cosine similarity, so prematurely discarding them would defeat semantic
// retrieval.
//
// The verifier downstream is responsible for rejecting evidence that is merely
// related but does not actually demonstrate the requirement.
func (r HybridSemanticRetriever) Retrieve(
	requirement string,
	evidence []string,
	limit int,
) []EvidenceCandidate {
	requirement = normalizeSemanticText(requirement)
	if requirement == "" || len(evidence) == 0 || limit <= 0 {
		return nil
	}

	candidates := make([]EvidenceCandidate, 0, len(evidence))

	for _, item := range evidence {
		item = normalizeSemanticText(item)
		if item == "" {
			continue
		}

		candidates = append(candidates, EvidenceCandidate{
			Evidence: item,
			Score:    r.score(requirement, item),
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	if limit > len(candidates) {
		limit = len(candidates)
	}

	return candidates[:limit]
}

var (
	semanticRetrieverMu sync.RWMutex

	defaultSemanticRetriever SemanticRetriever = HybridSemanticRetriever{}
)

// SetSemanticRetriever replaces the process-wide semantic retriever.
//
// Passing nil restores the conservative default implementation.
func SetSemanticRetriever(retriever SemanticRetriever) {
	semanticRetrieverMu.Lock()
	defer semanticRetrieverMu.Unlock()

	if retriever == nil {
		defaultSemanticRetriever = HybridSemanticRetriever{}
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
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func cosineSimilarity(left, right []float64) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}

	var dot, leftNorm, rightNorm float64

	for i := range left {
		dot += left[i] * right[i]
		leftNorm += left[i] * left[i]
		rightNorm += right[i] * right[i]
	}

	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}

	return clamp01(
		dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm)),
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
