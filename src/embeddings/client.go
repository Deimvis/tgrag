package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Deimvis/tgrag/src/configs"
)

var ErrDisabled = errors.New("embeddings are disabled, set embeddings.is_enabled: true in config")

type Embedder struct {
	cfg        configs.EmbeddingsConfig
	httpClient *http.Client
}

func New(cfg configs.Config) *Embedder {
	return NewWithHttpClient(cfg.Embeddings, &http.Client{Timeout: 5 * time.Minute})
}

func NewWithHttpClient(cfg configs.EmbeddingsConfig, httpClient *http.Client) *Embedder {
	return &Embedder{cfg: cfg, httpClient: httpClient}
}

func (e *Embedder) IsEnabled() bool {
	return e.cfg.IsEnabled
}

func (e *Embedder) Model() string {
	return e.cfg.Model
}

func (e *Embedder) BatchSize() int {
	return e.cfg.BatchSize
}

func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if !e.cfg.IsEnabled {
		return nil, ErrDisabled
	}
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	result := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += e.cfg.BatchSize {
		end := min(start+e.cfg.BatchSize, len(texts))
		vectors, err := e.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		result = append(result, vectors...)
	}
	return result, nil
}

func (e *Embedder) EmbedOne(ctx context.Context, text string) ([]float32, error) {
	vectors, err := e.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingsResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (e *Embedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embeddingsRequest{Model: e.cfg.Model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(e.cfg.BaseUrl, "/")+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	apiKey := e.cfg.ResolveApiKey()
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings request to %s: %w", e.cfg.BaseUrl, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, fmt.Errorf("read embeddings response: %w", err)
	}
	var parsed embeddingsResponse
	err = json.Unmarshal(raw, &parsed)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings server returned %d: %s", resp.StatusCode, truncate(responseErrorMessage(parsed, raw), 300))
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings server returned %d vectors for %d texts", len(parsed.Data), len(texts))
	}
	result := make([][]float32, len(texts))
	for i, item := range parsed.Data {
		index := item.Index
		if index < 0 || index >= len(texts) {
			index = i
		}
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("embeddings server returned empty vector for text %d", index)
		}
		result[index] = item.Embedding
	}
	return result, nil
}

func responseErrorMessage(parsed embeddingsResponse, raw []byte) string {
	if parsed.Error != nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return string(raw)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
