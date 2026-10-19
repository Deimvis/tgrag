package tools

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Deimvis/tgrag/src/models"
	"github.com/Deimvis/tgrag/src/storage"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	s, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	base := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, s.UpsertChat(ctx, models.Chat{Id: 10, Kind: models.ChatKindPrivate, Title: "Lesha"}))
	require.NoError(t, s.UpsertMessages(ctx, []models.Message{
		{ChatId: 10, Id: 1, SenderName: "Lesha", Ts: base, Text: "found an apartment"},
		{ChatId: 10, Id: 2, SenderName: "Me", Ts: base.Add(time.Minute), ReplyToMessageId: 1, Text: "how much"},
	}))
	require.NoError(t, s.ReplaceWindows(ctx, 10, []models.Window{{
		ChatId: 10, FirstMessageId: 1, LastMessageId: 2, FirstTs: base, LastTs: base.Add(time.Minute),
		Participants: []string{"Lesha", "Me"}, Text: "Lesha: found an apartment\nMe: how much",
	}}))
	return New(s)
}

func TestCall(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()
	t.Run("list_chats", func(t *testing.T) {
		act, err := r.Call(ctx, "list_chats", []byte(`{}`))
		require.NoError(t, err)
		require.Len(t, act.([]models.Chat), 1)
	})
	t.Run("search", func(t *testing.T) {
		act, err := r.Call(ctx, "search", []byte(`{"query":"apartment","after":"2024-03-01","before":"2024-03-01"}`))
		require.NoError(t, err)
		require.Len(t, act.([]models.SearchHit), 1)
	})
	t.Run("search returns empty slice", func(t *testing.T) {
		act, err := r.Call(ctx, "search", []byte(`{"query":"nothing"}`))
		require.NoError(t, err)
		require.Equal(t, []models.SearchHit{}, act)
	})
	t.Run("get_context", func(t *testing.T) {
		act, err := r.Call(ctx, "get_context", []byte(`{"chat_id":10,"message_id":2,"before":5,"after":5}`))
		require.NoError(t, err)
		msgs := act.([]MessageOutput)
		require.Len(t, msgs, 2)
		require.Equal(t, "tg://openmessage?user_id=10&message_id=1", msgs[0].Link)
	})
	t.Run("get_context with explicit zero after", func(t *testing.T) {
		act, err := r.Call(ctx, "get_context", []byte(`{"chat_id":10,"message_id":1,"before":0,"after":0}`))
		require.NoError(t, err)
		require.Len(t, act.([]MessageOutput), 1)
	})
	t.Run("get_context defaults when omitted", func(t *testing.T) {
		act, err := r.Call(ctx, "get_context", []byte(`{"chat_id":10,"message_id":1}`))
		require.NoError(t, err)
		require.Len(t, act.([]MessageOutput), 2)
	})
	t.Run("get_thread", func(t *testing.T) {
		act, err := r.Call(ctx, "get_thread", []byte(`{"chat_id":10,"message_id":2}`))
		require.NoError(t, err)
		require.Len(t, act.([]MessageOutput), 2)
	})
	t.Run("unknown chat", func(t *testing.T) {
		_, err := r.Call(ctx, "get_context", []byte(`{"chat_id":99,"message_id":2}`))
		require.Error(t, err)
	})
	t.Run("unknown tool", func(t *testing.T) {
		_, err := r.Call(ctx, "nope", []byte(`{}`))
		require.Error(t, err)
	})
	t.Run("invalid input", func(t *testing.T) {
		_, err := r.Call(ctx, "search", []byte(`{`))
		require.Error(t, err)
	})
}

func TestParseDate(t *testing.T) {
	act, err := ParseDate("2024-03-01")
	require.NoError(t, err)
	require.Equal(t, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), act)
	act, err = ParseDate("2024-03-01T10:00:00Z")
	require.NoError(t, err)
	require.Equal(t, time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC), act)
	act, err = ParseDate("")
	require.NoError(t, err)
	require.True(t, act.IsZero())
	_, err = ParseDate("yesterday")
	require.Error(t, err)
}

func TestDefinitions(t *testing.T) {
	defs := Definitions()
	require.Len(t, defs, 4)
	for _, d := range defs {
		require.NotEmpty(t, d.Name)
		require.NotEmpty(t, d.Description)
		require.Equal(t, "object", d.InputSchema["type"])
	}
}
