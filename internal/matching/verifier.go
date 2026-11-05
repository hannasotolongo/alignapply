package matching

import "sync"

// EvidenceVerifier decides whether retrieved candidate evidence actually
// demonstrates a job requirement. Retrieval scores are intentionally not
// provided here: semantic similarity chooses what to inspect; the verifier
// independently decides whether the evidence supports the requirement.
type EvidenceVerifier interface {
	Verify(requirement string, evidence string) evidenceResult
}

type deterministicEvidenceVerifier struct{}

func (deterministicEvidenceVerifier) Verify(requirement string, evidence string) evidenceResult {
	return verifyEvidenceDeterministically(requirement, evidence)
}

var (
	evidenceVerifierMu sync.RWMutex
	evidenceVerifier   EvidenceVerifier = deterministicEvidenceVerifier{}
)

func SetEvidenceVerifier(verifier EvidenceVerifier) {
	evidenceVerifierMu.Lock()
	defer evidenceVerifierMu.Unlock()

	if verifier == nil {
		evidenceVerifier = deterministicEvidenceVerifier{}
		return
	}

	evidenceVerifier = verifier
}

func currentEvidenceVerifier() EvidenceVerifier {
	evidenceVerifierMu.RLock()
	defer evidenceVerifierMu.RUnlock()
	return evidenceVerifier
}

// scorer picks the AI helpers for one Match call. A fast scorer (used for
// search result lists) skips the per-requirement OpenAI verifier and
// embedding lookups and relies on the instant word, synonym and date checks;
// the full scorer (Fit Check, résumé recommendation) uses AI as well.
type scorer struct{ fast bool }

func (s scorer) verifier() EvidenceVerifier {
	if s.fast {
		return deterministicEvidenceVerifier{}
	}
	return currentEvidenceVerifier()
}

func (s scorer) retriever() SemanticRetriever {
	if s.fast {
		return HybridSemanticRetriever{}
	}
	return currentSemanticRetriever()
}
