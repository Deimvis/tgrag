package indexer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Deimvis/tgrag/src/configs"
	"github.com/Deimvis/tgrag/src/embeddings"
	"github.com/Deimvis/tgrag/src/models"
	"github.com/Deimvis/tgrag/src/storage"
)

func newTestIndexer(t *testing.T, isEmbeddingEnabled bool) (*Indexer, *storage.Storage, *int) {
	t.Helper()
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var req struct {
			Input []string `json:"input"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		data := make([]map[string]any, 0, len(req.Input))
		for i := range req.Input {
			data = append(data, map[string]any{"index": i, "embedding": []float32{1, float32(i)}})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)
	cfg := configs.NewDefault(1, "h")
	cfg.Embeddings = configs.NewDefaultEmbeddingsConfig(isEmbeddingEnabled)
	cfg.Embeddings.BaseUrl = server.URL
	s, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	idx := New(cfg, s, embeddings.NewWithHttpClient(cfg.Embeddings, http.DefaultClient))
	idx.log = io.Discard
	return idx, s, &requestCount
}

func seedMessages(t *testing.T, s *storage.Storage) {
	t.Helper()
	ctx := context.Background()
	base := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, s.UpsertChat(ctx, models.Chat{Id: 10, Kind: models.ChatKindPrivate, Title: "Lesha"}))
	require.NoError(t, s.UpsertMessages(ctx, []models.Message{
		{ChatId: 10, Id: 1, SenderName: "Lesha", Ts: base, Text: "first"},
		{ChatId: 10, Id: 2, SenderName: "Me", Ts: base.Add(5 * time.Hour), Text: "second"},
	}))
}

func TestReindex(t *testing.T) {
	ctx := context.Background()
	t.Run("without embeddings", func(t *testing.T) {
		idx, s, requestCount := newTestIndexer(t, false)
		seedMessages(t, s)
		count, err := idx.ReindexAll(ctx)
		require.NoError(t, err)
		require.Equal(t, 2, count)
		require.Equal(t, 0, *requestCount)
		_, err = idx.EmbedAll(ctx)
		require.ErrorIs(t, err, embeddings.ErrDisabled)
	})
	t.Run("with embeddings", func(t *testing.T) {
		idx, s, requestCount := newTestIndexer(t, true)
		seedMessages(t, s)
		count, err := idx.ReindexChat(ctx, 10)
		require.NoError(t, err)
		require.Equal(t, 2, count)
		require.Equal(t, 1, *requestCount)
		embedded, err := s.EmbeddingCount(ctx, idx.embedder.Model())
		require.NoError(t, err)
		require.Equal(t, int64(2), embedded)

		result, err := idx.EmbedAll(ctx)
		require.NoError(t, err)
		require.Equal(t, 0, result.EmbeddedWindowCount)
		require.Equal(t, int64(0), result.PrunedEmbeddingCount)
		require.Equal(t, 1, *requestCount)

		require.NoError(t, s.UpsertMessages(ctx, []models.Message{{ChatId: 10, Id: 3, SenderName: "Me", Ts: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Text: "third"}}))
		_, err = idx.ReindexChat(ctx, 10)
		require.NoError(t, err)
		require.Equal(t, 2, *requestCount)
		embedded, err = s.EmbeddingCount(ctx, idx.embedder.Model())
		require.NoError(t, err)
		require.Equal(t, int64(3), embedded)
	})
}
