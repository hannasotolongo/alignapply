package main

import (
	"log"
	"net/http"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/api"
	"github.com/hannasotolongo/casemade-backend/internal/matching"
)

func main() {
	// nomic-embed-text performs retrieval only. Its similarity score determines
	// which candidate evidence should be inspected, not whether a requirement
	// is satisfied.
	semanticProvider := matching.NewOllamaSemanticProvider()

	matching.SetSemanticRetriever(matching.HybridSemanticRetriever{
		Provider: semanticProvider,
	})

	// qwen3:4b independently verifies whether retrieved evidence actually
	// demonstrates the requirement.
	evidenceVerifier := matching.NewOllamaEvidenceVerifier()
	matching.SetEvidenceVerifier(evidenceVerifier)

	log.Println(
		"Semantic evidence retrieval configured with Ollama (nomic-embed-text)",
	)
	log.Println(
		"Evidence verification configured with Ollama (qwen3:4b)",
	)

	server := api.NewServer()

	httpServer := &http.Server{
		Addr:              ":8080",
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println("AlignApply API listening on http://localhost:8080")

	if err := httpServer.ListenAndServe(); err != nil &&
		err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
