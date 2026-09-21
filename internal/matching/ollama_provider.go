package matching

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultOllamaBaseURL = "http://127.0.0.1:11434"
	defaultOllamaModel   = "nomic-embed-text"
)

// OllamaSemanticProvider implements SemanticProvider using an Ollama
// embedding model.
//
// Ollama is responsible only for semantic relatedness. Hard qualifications
// such as licenses, certifications, education, and explicit years of
// experience remain under deterministic matching rules.
type OllamaSemanticProvider struct {
	BaseURL string
	Model   string
	Client  *http.Client

	mu    sync.RWMutex
	cache map[string][]float64
}

// NewOllamaSemanticProvider creates the default local semantic provider.
func NewOllamaSemanticProvider() *OllamaSemanticProvider {
	return &OllamaSemanticProvider{
		BaseURL: defaultOllamaBaseURL,
		Model:   defaultOllamaModel,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
		cache: make(map[string][]float64),
	}
}

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

func (p *OllamaSemanticProvider) Similarity(
	requirement string,
	evidence string,
) (float64, error) {
	requirement = normalizeSemanticText(requirement)
	evidence = normalizeSemanticText(evidence)

	if requirement == "" || evidence == "" {
		return 0, nil
	}

	embeddings, err := p.embeddings([]string{requirement, evidence})
	if err != nil {
		return 0, err
	}

	if len(embeddings) != 2 {
		return 0, fmt.Errorf(
			"ollama returned %d embeddings; expected 2",
			len(embeddings),
		)
	}

	return cosineSimilarity(embeddings[0], embeddings[1]), nil
}

func (p *OllamaSemanticProvider) embeddings(
	texts []string,
) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([][]float64, len(texts))
	missingTexts := make([]string, 0, len(texts))
	missingIndexes := make([]int, 0, len(texts))

	for i, text := range texts {
		text = normalizeSemanticText(text)
		if text == "" {
			results[i] = nil
			continue
		}

		if embedding, ok := p.cachedEmbedding(text); ok {
			results[i] = embedding
			continue
		}

		missingTexts = append(missingTexts, text)
		missingIndexes = append(missingIndexes, i)
	}

	if len(missingTexts) == 0 {
		return results, nil
	}

	fetched, err := p.fetchEmbeddings(missingTexts)
	if err != nil {
		return nil, err
	}

	if len(fetched) != len(missingTexts) {
		return nil, fmt.Errorf(
			"ollama returned %d embeddings for %d inputs",
			len(fetched),
			len(missingTexts),
		)
	}

	for i, embedding := range fetched {
		if len(embedding) == 0 {
			return nil, errors.New("ollama returned an empty embedding")
		}

		index := missingIndexes[i]
		results[index] = embedding
		p.storeEmbedding(missingTexts[i], embedding)
	}

	return results, nil
}

func (p *OllamaSemanticProvider) fetchEmbeddings(
	texts []string,
) ([][]float64, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}

	model := strings.TrimSpace(p.Model)
	if model == "" {
		model = defaultOllamaModel
	}

	payload, err := json.Marshal(ollamaEmbedRequest{
		Model: model,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("encode ollama embedding request: %w", err)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		baseURL+"/api/embed",
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("create ollama embedding request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := p.Client
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call ollama embeddings: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read ollama embedding response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"ollama embeddings returned HTTP %d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}

	var decoded ollamaEmbedResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode ollama embedding response: %w", err)
	}

	if len(decoded.Embeddings) == 0 {
		return nil, errors.New("ollama returned no embeddings")
	}

	return decoded.Embeddings, nil
}

func (p *OllamaSemanticProvider) cachedEmbedding(
	text string,
) ([]float64, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.cache == nil {
		return nil, false
	}

	embedding, ok := p.cache[text]
	if !ok {
		return nil, false
	}

	return embedding, true
}

func (p *OllamaSemanticProvider) storeEmbedding(
	text string,
	embedding []float64,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cache == nil {
		p.cache = make(map[string][]float64)
	}

	p.cache[text] = embedding
}
