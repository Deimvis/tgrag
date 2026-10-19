package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Deimvis/tgrag/src/configs"
	"github.com/Deimvis/tgrag/src/embeddings"
	"github.com/Deimvis/tgrag/src/models"
	"github.com/Deimvis/tgrag/src/storage"
)

func newFakeEmbeddingsServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		data := make([]map[string]any, 0, len(req.Input))
		for i, text := range req.Input {
			vector := []float32{0, 1}
			if strings.Contains(strings.ToLower(text), "apartment") || strings.Contains(text, "жиль") {
				vector = []float32{1, 0}
			}
			data = append(data, map[string]any{"index": i, "embedding": vector})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)
	return server
}

func newEnabledEmbedder(serverUrl string) *embeddings.Embedder {
	cfg := configs.NewDefaultEmbeddingsConfig(true)
	cfg.BaseUrl = serverUrl
	return embeddings.NewWithHttpClient(cfg, http.DefaultClient)
}

func embedAllWindows(t *testing.T, r *Registry) {
	t.Helper()
	ctx := context.Background()
	inputs, err := r.storage.WindowsWithoutEmbeddings(ctx, r.embedder.Model(), 0, 100)
	require.NoError(t, err)
	texts := make([]string, 0, len(inputs))
	for _, in := range inputs {
		texts = append(texts, in.Text)
	}
	vectors, err := r.embedder.Embed(ctx, texts)
	require.NoError(t, err)
	require.NoError(t, r.storage.UpsertEmbeddings(ctx, r.embedder.Model(), inputs, vectors))
}

func TestSearchModes(t *testing.T) {
	ctx := context.Background()
	t.Run("disabled embeddings", func(t *testing.T) {
		r := newTestRegistry(t)
		hits, err := r.Search(ctx, SearchInput{Query: "apartment", Mode: SearchModeAuto})
		require.NoError(t, err)
		require.Len(t, hits, 1)
		require.Equal(t, models.SearchHitSourceFts, hits[0].Source)
		_, err = r.Search(ctx, SearchInput{Query: "apartment", Mode: SearchModeSemantic})
		require.ErrorIs(t, err, embeddings.ErrDisabled)
		_, err = r.Search(ctx, SearchInput{Query: "apartment", Mode: SearchModeHybrid})
		require.ErrorIs(t, err, embeddings.ErrDisabled)
		_, err = r.Search(ctx, SearchInput{Query: "apartment", Mode: "fuzzy"})
		require.ErrorContains(t, err, "unknown search mode")
	})
	t.Run("enabled embeddings", func(t *testing.T) {
		server := newFakeEmbeddingsServer(t)
		r := newTestRegistryWithEmbedder(t, newEnabledEmbedder(server.URL))
		embedAllWindows(t, r)
		hits, err := r.Search(ctx, SearchInput{Query: "где жильё", Mode: SearchModeSemantic})
		require.NoError(t, err)
		require.Len(t, hits, 1)
		require.Equal(t, models.SearchHitSourceSemantic, hits[0].Source)
		require.InDelta(t, 1, hits[0].Similarity, 1e-6)
		require.Equal(t, "Lesha: found an apartment\nMe: how much", hits[0].Snippet)

		hits, err = r.Search(ctx, SearchInput{Query: "apartment", Mode: SearchModeAuto})
		require.NoError(t, err)
		require.Len(t, hits, 1)
		require.Equal(t, models.SearchHitSourceHybrid, hits[0].Source)
		require.Contains(t, hits[0].Snippet, "[apartment]")
		require.InDelta(t, 1, hits[0].Similarity, 1e-6)

		hits, err = r.Search(ctx, SearchInput{Query: "nothing", Mode: SearchModeFts})
		require.NoError(t, err)
		require.Empty(t, hits)

		raw, err := r.Call(ctx, "search", []byte(`{"query":"жильё","mode":"semantic"}`))
		require.NoError(t, err)
		require.Len(t, raw.([]models.SearchHit), 1)
	})
	t.Run("semantic server failure", func(t *testing.T) {
		r := newTestRegistryWithEmbedder(t, newEnabledEmbedder("http://127.0.0.1:1"))
		_, err := r.Search(ctx, SearchInput{Query: "apartment", Mode: SearchModeAuto})
		require.ErrorContains(t, err, "embed query")
	})
}

func TestFuseHits(t *testing.T) {
	hit := func(id int64, source models.SearchHitSource) models.SearchHit {
		return models.SearchHit{WindowId: id, Source: source, Snippet: string(source), Similarity: map[bool]float64{true: 0.9, false: 0}[source == models.SearchHitSourceSemantic]}
	}
	fts := []models.SearchHit{hit(1, models.SearchHitSourceFts), hit(2, models.SearchHitSourceFts), hit(3, models.SearchHitSourceFts)}
	semantic := []models.SearchHit{hit(4, models.SearchHitSourceSemantic), hit(2, models.SearchHitSourceSemantic)}
	act := FuseHits(fts, semantic, 3)
	require.Len(t, act, 3)
	require.Equal(t, int64(2), act[0].WindowId)
	require.Equal(t, models.SearchHitSourceHybrid, act[0].Source)
	require.Equal(t, "fts", act[0].Snippet)
	require.InDelta(t, 0.9, act[0].Similarity, 1e-9)
	require.Equal(t, int64(1), act[1].WindowId)
	require.Equal(t, int64(4), act[2].WindowId)
	require.Less(t, act[0].Rank, act[1].Rank)
	require.Empty(t, FuseHits(nil, nil, 5))
}

func TestPlainQuery(t *testing.T) {
	require.Equal(t, "квартир аренд", PlainQuery(`квартир* OR аренд*`))
	require.Equal(t, "exact phrase x", PlainQuery(`"exact phrase" NOT x`))
	require.Equal(t, "", PlainQuery("AND"))
}

func TestWindowsSeedHasHash(t *testing.T) {
	r := newTestRegistry(t)
	inputs, err := r.storage.WindowsWithoutEmbeddings(context.Background(), "m", 0, 10)
	require.NoError(t, err)
	require.Len(t, inputs, 1)
	require.Equal(t, storage.TextHash(inputs[0].Text), inputs[0].TextHash)
}
