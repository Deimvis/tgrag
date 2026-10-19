package windows

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Deimvis/tgrag/src/models"
)

var base = time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

func msg(id int64, offset time.Duration, sender, text string) models.Message {
	return models.Message{ChatId: 1, Id: id, SenderName: sender, Ts: base.Add(offset), Text: text}
}

func TestBuild(t *testing.T) {
	params := Params{Gap: 10 * time.Minute, MaxChars: 1000}
	t.Run("empty input", func(t *testing.T) {
		act := Build(1, nil, params)
		require.Empty(t, act)
	})
	t.Run("single window within gap", func(t *testing.T) {
		act := Build(1, []models.Message{
			msg(1, 0, "A", "hi"),
			msg(2, time.Minute, "B", "hello"),
			msg(3, 2*time.Minute, "A", "how are you"),
		}, params)
		require.Len(t, act, 1)
		require.Equal(t, int64(1), act[0].FirstMessageId)
		require.Equal(t, int64(3), act[0].LastMessageId)
		require.Equal(t, []string{"A", "B"}, act[0].Participants)
		require.Equal(t, "A: hi\nB: hello\nA: how are you", act[0].Text)
	})
	t.Run("split on time gap", func(t *testing.T) {
		act := Build(1, []models.Message{
			msg(1, 0, "A", "hi"),
			msg(2, 30*time.Minute, "B", "later"),
		}, params)
		require.Len(t, act, 2)
		require.Equal(t, int64(1), act[0].LastMessageId)
		require.Equal(t, int64(2), act[1].FirstMessageId)
	})
	t.Run("reply crosses time gap", func(t *testing.T) {
		reply := msg(2, 30*time.Minute, "B", "answer")
		reply.ReplyToMessageId = 1
		act := Build(1, []models.Message{msg(1, 0, "A", "question"), reply}, params)
		require.Len(t, act, 1)
	})
	t.Run("split on size limit", func(t *testing.T) {
		long := strings.Repeat("x", 600)
		act := Build(1, []models.Message{
			msg(1, 0, "A", long),
			msg(2, time.Minute, "B", long),
		}, params)
		require.Len(t, act, 2)
	})
	t.Run("skips empty messages and sorts by id", func(t *testing.T) {
		act := Build(1, []models.Message{
			msg(3, 2*time.Minute, "A", "third"),
			msg(2, time.Minute, "B", "   "),
			msg(1, 0, "A", "first"),
		}, params)
		require.Len(t, act, 1)
		require.Equal(t, "A: first\nA: third", act[0].Text)
	})
}
