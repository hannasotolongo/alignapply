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
