package export

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Deimvis/tgrag/src/models"
)

const multiChatExport = `{
  "about": "x",
  "chats": {
    "list": [
      {
        "name": "Lesha",
        "type": "personal_chat",
        "id": 111,
        "messages": [
          {"id": 1, "type": "message", "date": "2024-03-01T12:00:00", "date_unixtime": "1709294400", "from": "Lesha", "from_id": "user111", "text": "hi"},
          {"id": 2, "type": "service", "date": "2024-03-01T12:01:00", "date_unixtime": "1709294460", "actor": "Lesha", "text": ""},
          {"id": 3, "type": "message", "date": "2024-03-01T12:02:00", "date_unixtime": "1709294520", "edited": "2024-03-01T12:05:00", "from": "Me", "from_id": "user222", "reply_to_message_id": 1, "text": ["see ", {"type": "link", "text": "https://x.y"}, " now"]}
        ]
      },
      {
        "name": "Team",
        "type": "private_supergroup",
        "id": 333,
        "messages": []
      }
    ]
  }
}`

const singleChatExport = `{
  "name": "Lesha",
  "type": "personal_chat",
  "id": 111,
  "messages": [
    {"id": 1, "type": "message", "date": "2024-03-01T12:00:00", "date_unixtime": "1709294400", "from": "Lesha", "from_id": "user111", "text": "hi"}
  ]
}`

func TestParse(t *testing.T) {
	t.Run("multi chat export", func(t *testing.T) {
		act, err := Parse(strings.NewReader(multiChatExport))
		require.NoError(t, err)
		require.Len(t, act, 2)
		require.Equal(t, models.Chat{Id: 111, Kind: models.ChatKindPrivate, Title: "Lesha", LatestMessageTs: time.Unix(1709294520, 0).UTC(), MessageCount: 2}, act[0].Chat)
		require.Len(t, act[0].Messages, 2)
		exp := models.Message{ChatId: 111, Id: 3, SenderUserId: 222, SenderName: "Me", Ts: time.Unix(1709294520, 0).UTC(), ReplyToMessageId: 1, Text: "see https://x.y now"}
		act[0].Messages[1].EditTs = time.Time{}
		require.Equal(t, exp, act[0].Messages[1])
		require.Equal(t, models.ChatKindGroup, act[1].Chat.Kind)
		require.Empty(t, act[1].Messages)
	})
	t.Run("single chat export", func(t *testing.T) {
		act, err := Parse(strings.NewReader(singleChatExport))
		require.NoError(t, err)
		require.Len(t, act, 1)
		require.Equal(t, int64(111), act[0].Chat.Id)
		require.Equal(t, "hi", act[0].Messages[0].Text)
	})
	t.Run("invalid json", func(t *testing.T) {
		_, err := Parse(strings.NewReader("{"))
		require.Error(t, err)
	})
}

func TestChatKind(t *testing.T) {
	require.Equal(t, models.ChatKindPrivate, chatKind("personal_chat"))
	require.Equal(t, models.ChatKindPrivate, chatKind("bot_chat"))
	require.Equal(t, models.ChatKindGroup, chatKind("private_group"))
	require.Equal(t, models.ChatKindGroup, chatKind("public_supergroup"))
	require.Equal(t, models.ChatKindChannel, chatKind("private_channel"))
}

func TestParseSenderId(t *testing.T) {
	require.Equal(t, int64(42), parseSenderId("user42"))
	require.Equal(t, int64(7), parseSenderId("channel7"))
	require.Equal(t, int64(0), parseSenderId(""))
}
