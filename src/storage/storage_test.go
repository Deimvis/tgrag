package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Deimvis/tgrag/src/models"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

var base = time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

func seed(t *testing.T, s *Storage) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.UpsertChat(ctx, models.Chat{Id: 10, Kind: models.ChatKindPrivate, Title: "Lesha"}))
	require.NoError(t, s.UpsertChat(ctx, models.Chat{Id: 20, Kind: models.ChatKindGroup, Title: "Team"}))
	require.NoError(t, s.UpsertMessages(ctx, []models.Message{
		{ChatId: 10, Id: 1, SenderName: "Lesha", Ts: base, Text: "нашёл квартиру на Арбате"},
		{ChatId: 10, Id: 2, SenderName: "Me", Ts: base.Add(time.Minute), ReplyToMessageId: 1, Text: "сколько стоит аренда?"},
		{ChatId: 10, Id: 3, SenderName: "Lesha", Ts: base.Add(2 * time.Minute), ReplyToMessageId: 2, Text: "80к в месяц"},
		{ChatId: 20, Id: 1, SenderName: "Bob", Ts: base.Add(time.Hour), Text: "deploy is done"},
	}))
	require.NoError(t, s.ReplaceWindows(ctx, 10, []models.Window{{
		ChatId: 10, FirstMessageId: 1, LastMessageId: 3, FirstTs: base, LastTs: base.Add(2 * time.Minute),
		Participants: []string{"Lesha", "Me"}, Text: "Lesha: нашёл квартиру на Арбате\nMe: сколько стоит аренда?\nLesha: 80к в месяц",
	}}))
	require.NoError(t, s.ReplaceWindows(ctx, 20, []models.Window{{
		ChatId: 20, FirstMessageId: 1, LastMessageId: 1, FirstTs: base.Add(time.Hour), LastTs: base.Add(time.Hour),
		Participants: []string{"Bob"}, Text: "Bob: deploy is done",
	}}))
}

func TestSearch(t *testing.T) {
	s := newTestStorage(t)
	seed(t, s)
	ctx := context.Background()
	t.Run("exact term", func(t *testing.T) {
		act, err := s.Search(ctx, models.SearchParams{Query: "аренда", Limit: 10, SnippetChars: 200})
		require.NoError(t, err)
		require.Len(t, act, 1)
		require.Equal(t, int64(10), act[0].ChatId)
		require.Equal(t, "Lesha", act[0].ChatTitle)
		require.Contains(t, act[0].Snippet, "[аренда]")
		require.Equal(t, "tg://openmessage?user_id=10&message_id=1", act[0].Link)
	})
	t.Run("prefix term", func(t *testing.T) {
		act, err := s.Search(ctx, models.SearchParams{Query: "квартир*", Limit: 10, SnippetChars: 200})
		require.NoError(t, err)
		require.Len(t, act, 1)
	})
	t.Run("chat filter", func(t *testing.T) {
		act, err := s.Search(ctx, models.SearchParams{Query: "deploy", ChatId: 10, Limit: 10, SnippetChars: 200})
		require.NoError(t, err)
		require.Empty(t, act)
	})
	t.Run("sender filter", func(t *testing.T) {
		act, err := s.Search(ctx, models.SearchParams{Query: "deploy", SenderName: "bob", Limit: 10, SnippetChars: 200})
		require.NoError(t, err)
		require.Len(t, act, 1)
	})
	t.Run("date filter", func(t *testing.T) {
		act, err := s.Search(ctx, models.SearchParams{Query: "deploy", Before: base.Add(30 * time.Minute), Limit: 10, SnippetChars: 200})
		require.NoError(t, err)
		require.Empty(t, act)
		act, err = s.Search(ctx, models.SearchParams{Query: "deploy", After: base.Add(30 * time.Minute), Limit: 10, SnippetChars: 200})
		require.NoError(t, err)
		require.Len(t, act, 1)
	})
	t.Run("empty query", func(t *testing.T) {
		_, err := s.Search(ctx, models.SearchParams{Query: "   ", Limit: 10})
		require.Error(t, err)
	})
}

func TestContextAndThread(t *testing.T) {
	s := newTestStorage(t)
	seed(t, s)
	ctx := context.Background()
	t.Run("context around message", func(t *testing.T) {
		act, err := s.GetContext(ctx, 10, 2, 1, 1)
		require.NoError(t, err)
		require.Len(t, act, 3)
		require.Equal(t, int64(1), act[0].Id)
		require.Equal(t, int64(3), act[2].Id)
	})
	t.Run("context with zero before", func(t *testing.T) {
		act, err := s.GetContext(ctx, 10, 2, 0, 0)
		require.NoError(t, err)
		require.Len(t, act, 1)
		require.Equal(t, int64(2), act[0].Id)
	})
	t.Run("thread from leaf", func(t *testing.T) {
		act, err := s.GetThread(ctx, 10, 3, 10)
		require.NoError(t, err)
		require.Len(t, act, 3)
	})
	t.Run("thread from root includes direct replies", func(t *testing.T) {
		act, err := s.GetThread(ctx, 10, 1, 10)
		require.NoError(t, err)
		require.Len(t, act, 2)
	})
}

func TestChatsAndSync(t *testing.T) {
	s := newTestStorage(t)
	seed(t, s)
	ctx := context.Background()
	t.Run("list chats ordered by latest message", func(t *testing.T) {
		act, err := s.ListChats(ctx, "", 10)
		require.NoError(t, err)
		require.Len(t, act, 2)
		require.Equal(t, int64(20), act[0].Id)
		require.Equal(t, int64(3), act[1].MessageCount)
	})
	t.Run("list chats by title", func(t *testing.T) {
		act, err := s.ListChats(ctx, "lesh", 10)
		require.NoError(t, err)
		require.Len(t, act, 1)
	})
	t.Run("max message id", func(t *testing.T) {
		act, err := s.MaxMessageId(ctx, 10)
		require.NoError(t, err)
		require.Equal(t, int64(3), act)
		act, err = s.MaxMessageId(ctx, 999)
		require.NoError(t, err)
		require.Equal(t, int64(0), act)
	})
	t.Run("upsert updates text", func(t *testing.T) {
		require.NoError(t, s.UpsertMessages(ctx, []models.Message{{ChatId: 10, Id: 3, SenderName: "Lesha", Ts: base, Text: "edited"}}))
		msgs, err := s.ChatMessages(ctx, 10)
		require.NoError(t, err)
		require.Equal(t, "edited", msgs[2].Text)
	})
	t.Run("delete chat data", func(t *testing.T) {
		require.NoError(t, s.DeleteChatData(ctx, 10))
		msgs, err := s.ChatMessages(ctx, 10)
		require.NoError(t, err)
		require.Empty(t, msgs)
		hits, err := s.Search(ctx, models.SearchParams{Query: "аренда", Limit: 10, SnippetChars: 100})
		require.NoError(t, err)
		require.Empty(t, hits)
	})
	t.Run("stats", func(t *testing.T) {
		require.NoError(t, s.RecordSyncRun(ctx, 2, 4))
		act, err := s.Stats(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(2), act.ChatCount)
		require.False(t, act.LatestSyncTs.IsZero())
	})
}

func TestBuildFtsQuery(t *testing.T) {
	require.Equal(t, `"hello" "world"`, BuildFtsQuery("hello world"))
	require.Equal(t, `"квартир"* OR "аренд"*`, BuildFtsQuery("квартир* OR аренд*"))
	require.Equal(t, `"a:b" NOT "c-d"`, BuildFtsQuery("a:b NOT c-d"))
	require.Equal(t, `"exact phrase"`, BuildFtsQuery(`"exact phrase"`))
	require.Equal(t, `"exact phrase" "x"`, BuildFtsQuery(`"exact phrase" x`))
	require.Equal(t, `"unclosed phrase"`, BuildFtsQuery(`"unclosed phrase`))
	require.Equal(t, ``, BuildFtsQuery(""))
}

func TestMessageLink(t *testing.T) {
	require.Equal(t, "https://t.me/c/5/7", MessageLink(models.Chat{Id: 5, Kind: models.ChatKindChannel}, 7))
	require.Equal(t, "https://t.me/news/7", MessageLink(models.Chat{Id: 5, Kind: models.ChatKindChannel, Username: "news"}, 7))
	require.Equal(t, "tg://openmessage?chat_id=5&message_id=7", MessageLink(models.Chat{Id: 5, Kind: models.ChatKindGroup}, 7))
}
