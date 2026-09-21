package matching

import (
	"errors"
	"testing"
)

type fakeSemanticProvider struct {
	scores map[string]float64
	err    error
}

func (f fakeSemanticProvider) Similarity(
	requirement string,
	evidence string,
) (float64, error) {
	if f.err != nil {
		return 0, f.err
	}

	return f.scores[requirement+"\x00"+evidence], nil
}

func TestHybridSemanticRetrieverWorksAcrossDomains(t *testing.T) {
	tests := []struct {
		name        string
		requirement string
		evidence    string
		score       float64
	}{
		{
			"software",
			"container orchestration",
			"deployed workloads on Kubernetes",
			0.92,
		},
		{
			"sales",
			"maintain enterprise client relationships",
			"managed a portfolio of B2B accounts",
			0.90,
		},
		{
			"healthcare",
			"patient education",
			"counseled patients on medication use",
			0.91,
		},
		{
			"finance",
			"financial modeling",
			"built valuation and cash-flow models",
			0.93,
		},
		{
			"marketing",
			"campaign performance analysis",
			"measured conversion and acquisition metrics",
			0.89,
		},
		{
			"operations",
			"vendor management",
			"negotiated with suppliers and monitored fulfillment",
			0.88,
		},
		{
			"hr",
			"candidate sourcing",
			"identified and contacted prospective hires",
			0.90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := fakeSemanticProvider{
				scores: map[string]float64{
					tt.requirement + "\x00" + tt.evidence: tt.score,
				},
			}

			retriever := HybridSemanticRetriever{
				Provider: provider,
			}

			got := retriever.Retrieve(
				tt.requirement,
				[]string{tt.evidence},
				1,
			)

			if len(got) != 1 {
				t.Fatalf(
					"expected one retrieved evidence candidate; got %d",
					len(got),
				)
			}

			if got[0].Evidence != tt.evidence {
				t.Fatalf(
					"retrieved evidence = %q; want %q",
					got[0].Evidence,
					tt.evidence,
				)
			}

			if got[0].Score != tt.score {
				t.Fatalf(
					"retrieval score = %.2f; want %.2f",
					got[0].Score,
					tt.score,
				)
			}
		})
	}
}

func TestHybridSemanticRetrieverRanksRelevantEvidenceFirst(t *testing.T) {
	requirement := "financial modeling"

	relevant := "built valuation and cash-flow models"
	unrelated := "managed social media campaigns"

	provider := fakeSemanticProvider{
		scores: map[string]float64{
			requirement + "\x00" + relevant:  0.67,
			requirement + "\x00" + unrelated: 0.08,
		},
	}

	retriever := HybridSemanticRetriever{
		Provider: provider,
	}

	got := retriever.Retrieve(
		requirement,
		[]string{
			unrelated,
			relevant,
		},
		2,
	)

	if len(got) != 2 {
		t.Fatalf(
			"expected two retrieved candidates; got %d",
			len(got),
		)
	}

	if got[0].Evidence != relevant {
		t.Fatalf(
			"expected relevant evidence first; got %#v",
			got,
		)
	}

	if got[0].Score <= got[1].Score {
		t.Fatalf(
			"expected relevant evidence score %.2f to exceed unrelated score %.2f",
			got[0].Score,
			got[1].Score,
		)
	}
}

func TestHybridSemanticRetrieverDoesNotTreatScoreAsVerdict(t *testing.T) {
	requirement := "container orchestration"
	evidence := "deployed production workloads on Kubernetes"

	provider := fakeSemanticProvider{
		scores: map[string]float64{
			requirement + "\x00" + evidence: 0.49,
		},
	}

	retriever := HybridSemanticRetriever{
		Provider: provider,
	}

	got := retriever.Retrieve(
		requirement,
		[]string{evidence},
		1,
	)

	if len(got) != 1 {
		t.Fatalf(
			"expected evidence to remain retrievable; got %d candidates",
			len(got),
		)
	}

	if got[0].Score != 0.49 {
		t.Fatalf(
			"retrieval score = %.2f; want 0.49",
			got[0].Score,
		)
	}
}

func TestSemanticProviderFailureFallsBackConservatively(t *testing.T) {
	requirement := "experience using SQL"
	evidence := "built reporting systems with SQL"

	retriever := HybridSemanticRetriever{
		Provider: fakeSemanticProvider{
			err: errors.New("unavailable"),
		},
	}

	got := retriever.Retrieve(
		requirement,
		[]string{evidence},
		1,
	)

	if len(got) != 1 {
		t.Fatalf(
			"expected one fallback candidate; got %d",
			len(got),
		)
	}

	want := tokenCoverage(
		requirement,
		evidence,
	)

	if got[0].Score != want {
		t.Fatalf(
			"fallback = %.2f; want lexical %.2f",
			got[0].Score,
			want,
		)
	}
}

func TestHybridSemanticRetrieverRespectsLimit(t *testing.T) {
	requirement := "backend engineering"

	evidence := []string{
		"built backend APIs",
		"developed distributed services",
		"managed marketing campaigns",
	}

	provider := fakeSemanticProvider{
		scores: map[string]float64{
			requirement + "\x00" + evidence[0]: 0.80,
			requirement + "\x00" + evidence[1]: 0.70,
			requirement + "\x00" + evidence[2]: 0.10,
		},
	}

	retriever := HybridSemanticRetriever{
		Provider: provider,
	}

	got := retriever.Retrieve(
		requirement,
		evidence,
		2,
	)

	if len(got) != 2 {
		t.Fatalf(
			"expected retrieval limit of 2; got %d",
			len(got),
		)
	}

	if got[0].Evidence != evidence[0] {
		t.Fatalf(
			"expected highest-ranked evidence first; got %#v",
			got,
		)
	}
}

func TestCosineSimilarity(t *testing.T) {
	if got := cosineSimilarity(
		[]float64{1, 0},
		[]float64{1, 0},
	); got != 1 {
		t.Fatalf(
			"identical vectors = %.2f; want 1",
			got,
		)
	}

	if got := cosineSimilarity(
		[]float64{1, 0},
		[]float64{0, 1},
	); got != 0 {
		t.Fatalf(
			"orthogonal vectors = %.2f; want 0",
			got,
		)
	}
}
