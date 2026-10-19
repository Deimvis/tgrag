package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Deimvis/tgrag/src/models"
)

func TestVectorCodec(t *testing.T) {
	exp := []float32{0.5, -1.25, 3}
	act, err := DecodeVector(EncodeVector(exp))
	require.NoError(t, err)
	require.Equal(t, exp, act)
	_, err = DecodeVector([]byte{1, 2, 3})
	require.Error(t, err)
}

func TestCosineSimilarity(t *testing.T) {
	require.InDelta(t, 1, CosineSimilarity([]float32{1, 0}, []float32{2, 0}), 1e-9)
	require.InDelta(t, 0, CosineSimilarity([]float32{1, 0}, []float32{0, 1}), 1e-9)
	require.InDelta(t, -1, CosineSimilarity([]float32{1, 0}, []float32{-1, 0}), 1e-9)
	require.Equal(t, float64(0), CosineSimilarity([]float32{1}, []float32{1, 2}))
	require.Equal(t, float64(0), CosineSimilarity([]float32{0, 0}, []float32{1, 2}))
}

func TestTopK(t *testing.T) {
	top := NewTopK[string](2)
	top.Add("a", 0.1)
	top.Add("b", 0.9)
	top.Add("c", 0.5)
	top.Add("d", 0.05)
	items, scores := top.SortedDescending()
	require.Equal(t, []string{"b", "c"}, items)
	require.Equal(t, []float64{0.9, 0.5}, scores)
	empty := NewTopK[string](0)
	empty.Add("x", 1)
	items, _ = empty.SortedDescending()
	require.Empty(t, items)
}

func TestSnippetPrefix(t *testing.T) {
	require.Equal(t, "привет", SnippetPrefix("привет", 10))
	require.Equal(t, "при…", SnippetPrefix("привет", 3))
	require.Equal(t, "abc", SnippetPrefix("abc", 0))
}

func TestEmbeddings(t *testing.T) {
	s := newTestStorage(t)
	seed(t, s)
	ctx := context.Background()
	const model = "test-model"
	t.Run("windows without embeddings", func(t *testing.T) {
		act, err := s.WindowsWithoutEmbeddings(ctx, model, 0, 10)
		require.NoError(t, err)
		require.Len(t, act, 2)
		require.Equal(t, TextHash(act[0].Text), act[0].TextHash)
		act, err = s.WindowsWithoutEmbeddings(ctx, model, 20, 10)
		require.NoError(t, err)
		require.Len(t, act, 1)
		require.Equal(t, "Bob: deploy is done", act[0].Text)
	})
	t.Run("upsert and semantic search", func(t *testing.T) {
		inputs, err := s.WindowsWithoutEmbeddings(ctx, model, 0, 10)
		require.NoError(t, err)
		vectors := make([][]float32, 0, len(inputs))
		for _, in := range inputs {
			if in.Text == "Bob: deploy is done" {
				vectors = append(vectors, []float32{1, 0})
			} else {
				vectors = append(vectors, []float32{0, 1})
			}
		}
		require.NoError(t, s.UpsertEmbeddings(ctx, model, inputs, vectors))
		count, err := s.EmbeddingCount(ctx, model)
		require.NoError(t, err)
		require.Equal(t, int64(2), count)
		remaining, err := s.WindowsWithoutEmbeddings(ctx, model, 0, 10)
		require.NoError(t, err)
		require.Empty(t, remaining)

		hits, err := s.SemanticSearch(ctx, models.SearchParams{Limit: 10, SnippetChars: 10}, model, []float32{1, 0.1})
		require.NoError(t, err)
		require.Len(t, hits, 2)
		require.Equal(t, int64(20), hits[0].ChatId)
		require.Equal(t, models.SearchHitSourceSemantic, hits[0].Source)
		require.Greater(t, hits[0].Similarity, hits[1].Similarity)
		require.Equal(t, "Bob: deplo…", hits[0].Snippet)
		require.Equal(t, "tg://openmessage?chat_id=20&message_id=1", hits[0].Link)

		hits, err = s.SemanticSearch(ctx, models.SearchParams{Limit: 10, SnippetChars: 100, ChatId: 10}, model, []float32{1, 0})
		require.NoError(t, err)
		require.Len(t, hits, 1)
		require.Equal(t, int64(10), hits[0].ChatId)

		hits, err = s.SemanticSearch(ctx, models.SearchParams{Limit: 10, SnippetChars: 100, SenderName: "bob"}, model, []float32{1, 0})
		require.NoError(t, err)
		require.Len(t, hits, 1)

		hits, err = s.SemanticSearch(ctx, models.SearchParams{Limit: 1, SnippetChars: 100}, model, []float32{0, 1})
		require.NoError(t, err)
		require.Len(t, hits, 1)
		require.Equal(t, int64(10), hits[0].ChatId)
	})
	t.Run("dimension mismatch", func(t *testing.T) {
		_, err := s.SemanticSearch(ctx, models.SearchParams{Limit: 10}, model, []float32{1, 0, 0})
		require.ErrorContains(t, err, "dimensions")
	})
	t.Run("unknown model has no hits", func(t *testing.T) {
		hits, err := s.SemanticSearch(ctx, models.SearchParams{Limit: 10}, "other", []float32{1, 0})
		require.NoError(t, err)
		require.Empty(t, hits)
	})
	t.Run("stats counts embedded windows", func(t *testing.T) {
		st, err := s.Stats(ctx, model)
		require.NoError(t, err)
		require.Equal(t, int64(2), st.EmbeddedWindowCount)
		require.Equal(t, model, st.EmbeddingModel)
	})
	t.Run("reindex keeps embeddings by text hash", func(t *testing.T) {
		require.NoError(t, s.ReplaceWindows(ctx, 20, []models.Window{
			{ChatId: 20, FirstMessageId: 1, LastMessageId: 1, Participants: []string{"Bob"}, Text: "Bob: deploy is done"},
			{ChatId: 20, FirstMessageId: 2, LastMessageId: 2, Participants: []string{"Bob"}, Text: "Bob: new text"},
		}))
		remaining, err := s.WindowsWithoutEmbeddings(ctx, model, 0, 10)
		require.NoError(t, err)
		require.Len(t, remaining, 1)
		require.Equal(t, "Bob: new text", remaining[0].Text)
	})
	t.Run("prune", func(t *testing.T) {
		require.NoError(t, s.UpsertEmbeddings(ctx, "old-model", []EmbeddingInput{{TextHash: "x", Text: "x"}}, [][]float32{{1}}))
		require.NoError(t, s.ReplaceWindows(ctx, 20, nil))
		pruned, err := s.PruneEmbeddings(ctx, model)
		require.NoError(t, err)
		require.Equal(t, int64(2), pruned)
		count, err := s.EmbeddingCount(ctx, model)
		require.NoError(t, err)
		require.Equal(t, int64(1), count)
	})
	t.Run("upsert length mismatch", func(t *testing.T) {
		require.Error(t, s.UpsertEmbeddings(ctx, model, []EmbeddingInput{{TextHash: "a"}}, nil))
	})
}

func TestMigrateFromSchemaWithoutTextHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE windows (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER NOT NULL,
			first_message_id INTEGER NOT NULL,
			last_message_id INTEGER NOT NULL,
			first_ts INTEGER NOT NULL,
			last_ts INTEGER NOT NULL,
			participants TEXT NOT NULL,
			text TEXT NOT NULL
		);
		INSERT INTO windows (chat_id, first_message_id, last_message_id, first_ts, last_ts, participants, text) VALUES (1, 1, 1, 0, 0, '[]', 'hello');`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	s, err := Open(path)
	require.NoError(t, err)
	defer s.Close()
	inputs, err := s.WindowsWithoutEmbeddings(context.Background(), "m", 0, 10)
	require.NoError(t, err)
	require.Len(t, inputs, 1)
	require.Equal(t, TextHash("hello"), inputs[0].TextHash)
	s2, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, s2.Close())
}
