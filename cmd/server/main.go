package main

import (
	"log"
	"net/http"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/api"
	"github.com/hannasotolongo/casemade-backend/internal/matching"
)

func main() {
	// Semantic embeddings are used for evidence retrieval, not as the final
	// judgment of whether a candidate satisfies a requirement.
	//
	// For local development we use Ollama with nomic-embed-text.
	// HybridSemanticRetriever combines semantic similarity with conservative
	// lexical matching and ranks the evidence most relevant to each
	// requirement.
	//
	// A separate verification step determines whether retrieved evidence
	// supports, partially supports, or does not clearly demonstrate the
	// requirement.
	//
	// Hard constraints such as explicit years of experience, education,
	// licenses, and certifications remain outside semantic inference.
	semanticProvider := matching.NewOllamaSemanticProvider()

	matching.SetSemanticRetriever(matching.HybridSemanticRetriever{
		Provider: semanticProvider,
	})

	log.Println(
		"Semantic evidence retrieval configured with Ollama (nomic-embed-text)",
	)

	server := api.NewServer()

	httpServer := &http.Server{
		Addr:              ":8080",
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println("CaseMade API listening on http://localhost:8080")

	if err := httpServer.ListenAndServe(); err != nil &&
		err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
