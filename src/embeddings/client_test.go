package embeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Deimvis/tgrag/src/configs"
)

func newTestServer(t *testing.T, handler func(w http.ResponseWriter, req embeddingsRequest, authorization string)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/embeddings", r.URL.Path)
		var req embeddingsRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		handler(w, req, r.Header.Get("Authorization"))
	}))
	t.Cleanup(server.Close)
	return server
}

func newTestEmbedder(serverUrl string, batchSize int) *Embedder {
	cfg := configs.NewDefaultEmbeddingsConfig(true)
	cfg.BaseUrl = serverUrl + "/v1/"
	cfg.BatchSize = batchSize
	cfg.ApiKeyEnv = "TGRAG_TEST_EMBEDDINGS_KEY"
	return NewWithHttpClient(cfg, http.DefaultClient)
}

func TestEmbed(t *testing.T) {
	ctx := context.Background()
	t.Run("batches and keeps order by index", func(t *testing.T) {
		t.Setenv("TGRAG_TEST_EMBEDDINGS_KEY", "secret")
		var batchSizes []int
		server := newTestServer(t, func(w http.ResponseWriter, req embeddingsRequest, authorization string) {
			require.Equal(t, "Bearer secret", authorization)
			require.Equal(t, "bge-m3", req.Model)
			batchSizes = append(batchSizes, len(req.Input))
			data := make([]map[string]any, 0, len(req.Input))
			for i := len(req.Input) - 1; i >= 0; i-- {
				data = append(data, map[string]any{"index": i, "embedding": []float32{float32(len(req.Input[i]))}})
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data})
		})
		act, err := newTestEmbedder(server.URL, 2).Embed(ctx, []string{"a", "bb", "ccc"})
		require.NoError(t, err)
		require.Equal(t, [][]float32{{1}, {2}, {3}}, act)
		require.Equal(t, []int{2, 1}, batchSizes)
	})
	t.Run("no authorization header without key", func(t *testing.T) {
		t.Setenv("TGRAG_TEST_EMBEDDINGS_KEY", "")
		server := newTestServer(t, func(w http.ResponseWriter, req embeddingsRequest, authorization string) {
			require.Empty(t, authorization)
			json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": []float32{1, 2}}}})
		})
		act, err := newTestEmbedder(server.URL, 8).EmbedOne(ctx, "x")
		require.NoError(t, err)
		require.Equal(t, []float32{1, 2}, act)
	})
	t.Run("server error message", func(t *testing.T) {
		server := newTestServer(t, func(w http.ResponseWriter, req embeddingsRequest, authorization string) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "model not found"}})
		})
		_, err := newTestEmbedder(server.URL, 8).Embed(ctx, []string{"x"})
		require.ErrorContains(t, err, "404")
		require.ErrorContains(t, err, "model not found")
	})
	t.Run("vector count mismatch", func(t *testing.T) {
		server := newTestServer(t, func(w http.ResponseWriter, req embeddingsRequest, authorization string) {
			json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
		})
		_, err := newTestEmbedder(server.URL, 8).Embed(ctx, []string{"x"})
		require.ErrorContains(t, err, "0 vectors for 1 texts")
	})
	t.Run("disabled", func(t *testing.T) {
		_, err := New(configs.NewDefault(1, "h")).Embed(ctx, []string{"x"})
		require.ErrorIs(t, err, ErrDisabled)
	})
	t.Run("empty input", func(t *testing.T) {
		act, err := newTestEmbedder("http://127.0.0.1:1", 8).Embed(ctx, nil)
		require.NoError(t, err)
		require.Empty(t, act)
	})
}
