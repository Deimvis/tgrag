package tg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	tgapi "github.com/gotd/td/tg"

	"github.com/Deimvis/tgrag/src/configs"
	"github.com/Deimvis/tgrag/src/indexer"
	"github.com/Deimvis/tgrag/src/models"
	"github.com/Deimvis/tgrag/src/storage"
)

const upsertBatchSize = 500

type Client struct {
	cfg     configs.Config
	storage *storage.Storage
	indexer *indexer.Indexer
	log     io.Writer
}

func New(cfg configs.Config, storage *storage.Storage, indexer *indexer.Indexer) *Client {
	return &Client{cfg: cfg, storage: storage, indexer: indexer, log: os.Stderr}
}

type SyncOptions struct {
	ChatFilter string
	IsFull     bool
}

type SyncResult struct {
	SyncedChatCount int `json:"synced_chat_count"`
	NewMessageCount int `json:"new_message_count"`
}

func (c *Client) Login(ctx context.Context, authenticator auth.UserAuthenticator) error {
	return c.run(ctx, authenticator, func(ctx context.Context, client *telegram.Client) error {
		self, err := client.Self(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(c.log, "logged in as %s (id=%d)\n", userName(self), self.ID)
		return os.Chmod(c.cfg.SessionFilePath(), 0o600)
	})
}

var ErrChatSelectionNotConfigured = errors.New("chat selection is not configured, run `tgrag setup` to choose which chats to sync")

func (c *Client) ListDialogs(ctx context.Context) ([]models.Chat, error) {
	var result []models.Chat
	err := c.run(ctx, nil, func(ctx context.Context, client *telegram.Client) error {
		return query.GetDialogs(client.API()).BatchSize(100).ForEach(ctx, func(ctx context.Context, elem dialogs.Elem) error {
			chat, ok := c.chatFromDialog(elem)
			if ok {
				result = append(result, chat)
			}
			return nil
		})
	})
	return result, err
}

func (c *Client) Sync(ctx context.Context, opts SyncOptions) (SyncResult, error) {
	var result SyncResult
	if !c.cfg.Sync.ChatSelection.IsConfigured() {
		return result, ErrChatSelectionNotConfigured
	}
	err := c.run(ctx, nil, func(ctx context.Context, client *telegram.Client) error {
		self, err := client.Self(ctx)
		if err != nil {
			return fmt.Errorf("get self: %w", err)
		}
		api := client.API()
		return query.GetDialogs(api).BatchSize(100).ForEach(ctx, func(ctx context.Context, elem dialogs.Elem) error {
			chat, ok := c.chatFromDialog(elem)
			if !ok || !c.cfg.Sync.ChatSelection.Allows(chat.Id) || !matchesChatFilter(chat, opts.ChatFilter) {
				return nil
			}
			newCount, err := c.syncChat(ctx, api, elem, chat, self, opts.IsFull)
			if err != nil {
				return fmt.Errorf("sync chat %d (%s): %w", chat.Id, chat.Title, err)
			}
			result.SyncedChatCount++
			result.NewMessageCount += newCount
			return nil
		})
	})
	if err != nil {
		return result, err
	}
	err = c.storage.RecordSyncRun(ctx, result.SyncedChatCount, result.NewMessageCount)
	return result, err
}

func (c *Client) syncChat(ctx context.Context, api *tgapi.Client, elem dialogs.Elem, chat models.Chat, self *tgapi.User, isFull bool) (int, error) {
	if isFull {
		err := c.storage.DeleteChatData(ctx, chat.Id)
		if err != nil {
			return 0, err
		}
	}
	err := c.storage.UpsertChat(ctx, chat)
	if err != nil {
		return 0, err
	}
	maxId, err := c.storage.MaxMessageId(ctx, chat.Id)
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(c.log, "syncing %s (id=%d, kind=%s) from message id %d\n", chat.Title, chat.Id, chat.Kind, maxId)

	var batch []models.Message
	count := 0
	flush := func() error {
		err := c.storage.UpsertMessages(ctx, batch)
		batch = batch[:0]
		return err
	}
	iter := elem.Messages(api).BatchSize(100).Iter()
	for iter.Next(ctx) {
		value := iter.Value()
		msg, ok := value.Msg.(*tgapi.Message)
		if !ok {
			continue
		}
		if int64(msg.ID) <= maxId {
			break
		}
		if msg.Message == "" {
			continue
		}
		batch = append(batch, convertMessage(msg, chat, value.Entities, self))
		count++
		if len(batch) >= upsertBatchSize {
			err = flush()
			if err != nil {
				return count, err
			}
			fmt.Fprintf(c.log, "  %d messages\n", count)
		}
		if c.cfg.Sync.MaxMessagesPerChat > 0 && count >= c.cfg.Sync.MaxMessagesPerChat {
			break
		}
	}
	err = iter.Err()
	if err != nil {
		return count, err
	}
	err = flush()
	if err != nil {
		return count, err
	}
	if count > 0 || isFull {
		_, err = c.indexer.ReindexChat(ctx, chat.Id)
		if err != nil {
			return count, err
		}
	}
	return count, nil
}

func (c *Client) chatFromDialog(elem dialogs.Elem) (models.Chat, bool) {
	var chat models.Chat
	switch p := elem.Peer.(type) {
	case *tgapi.InputPeerUser:
		user, ok := elem.Entities.Users()[p.UserID]
		if !ok || (c.cfg.Sync.IsSkippingBots && user.Bot) {
			return chat, false
		}
		chat = models.Chat{Id: user.ID, Kind: models.ChatKindPrivate, Title: userName(user), Username: user.Username, AccessHash: user.AccessHash}
	case *tgapi.InputPeerChat:
		group, ok := elem.Entities.Chats()[p.ChatID]
		if !ok {
			return chat, false
		}
		chat = models.Chat{Id: group.ID, Kind: models.ChatKindGroup, Title: group.Title}
	case *tgapi.InputPeerChannel:
		channel, ok := elem.Entities.Channels()[p.ChannelID]
		if !ok || (c.cfg.Sync.IsSkippingChannels && channel.Broadcast) {
			return chat, false
		}
		kind := models.ChatKindGroup
		if channel.Broadcast {
			kind = models.ChatKindChannel
		}
		chat = models.Chat{Id: channel.ID, Kind: kind, Title: channel.Title, Username: channel.Username, AccessHash: channel.AccessHash}
	default:
		return chat, false
	}
	return chat, true
}

func matchesChatFilter(chat models.Chat, filter string) bool {
	if filter == "" {
		return true
	}
	id, err := strconv.ParseInt(filter, 10, 64)
	if err == nil {
		return chat.Id == id
	}
	return strings.Contains(strings.ToLower(chat.Title), strings.ToLower(filter)) ||
		strings.EqualFold(chat.Username, strings.TrimPrefix(filter, "@"))
}

func convertMessage(msg *tgapi.Message, chat models.Chat, entities peer.Entities, self *tgapi.User) models.Message {
	senderId, senderName := resolveSender(msg, chat, entities, self)
	result := models.Message{
		ChatId:       chat.Id,
		Id:           int64(msg.ID),
		SenderUserId: senderId,
		SenderName:   senderName,
		Ts:           time.Unix(int64(msg.Date), 0).UTC(),
		Text:         msg.Message,
		IsOutgoing:   msg.Out,
	}
	editDate, ok := msg.GetEditDate()
	if ok {
		result.EditTs = time.Unix(int64(editDate), 0).UTC()
	}
	replyTo, ok := msg.GetReplyTo()
	if ok {
		header, isHeader := replyTo.(*tgapi.MessageReplyHeader)
		if isHeader {
			result.ReplyToMessageId = int64(header.ReplyToMsgID)
		}
	}
	return result
}

func resolveSender(msg *tgapi.Message, chat models.Chat, entities peer.Entities, self *tgapi.User) (int64, string) {
	fromId, ok := msg.GetFromID()
	if !ok {
		if msg.Out {
			return self.ID, userName(self)
		}
		return chat.Id, chat.Title
	}
	switch p := fromId.(type) {
	case *tgapi.PeerUser:
		if p.UserID == self.ID {
			return self.ID, userName(self)
		}
		user, found := entities.Users()[p.UserID]
		if found {
			return user.ID, userName(user)
		}
		return p.UserID, fmt.Sprintf("user%d", p.UserID)
	case *tgapi.PeerChannel:
		channel, found := entities.Channels()[p.ChannelID]
		if found {
			return 0, channel.Title
		}
		return 0, fmt.Sprintf("channel%d", p.ChannelID)
	case *tgapi.PeerChat:
		group, found := entities.Chats()[p.ChatID]
		if found {
			return 0, group.Title
		}
		return 0, fmt.Sprintf("chat%d", p.ChatID)
	}
	return 0, chat.Title
}

func userName(user *tgapi.User) string {
	name := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if name == "" {
		name = user.Username
	}
	if name == "" {
		name = fmt.Sprintf("user%d", user.ID)
	}
	return name
}

func (c *Client) run(ctx context.Context, authenticator auth.UserAuthenticator, fn func(ctx context.Context, client *telegram.Client) error) error {
	err := os.MkdirAll(c.cfg.Storage.DataDirectoryPath, 0o700)
	if err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	apiHash, err := c.cfg.Telegram.ResolveApiHash()
	if err != nil {
		return err
	}
	waiter := floodwait.NewSimpleWaiter().WithMaxWait(10 * time.Minute)
	client := telegram.NewClient(c.cfg.Telegram.ApiId, apiHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: c.cfg.SessionFilePath()},
		Middlewares:    []telegram.Middleware{waiter},
		NoUpdates:      true,
		Device:         telegram.DeviceTDesktopWindows(),
		Resolver:       telegram.TDesktopResolver(),
	})
	return client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("get auth status: %w", err)
		}
		if !status.Authorized {
			if authenticator == nil {
				return errors.New("not logged in, run `tgrag setup` first")
			}
			terminal, isTerminal := authenticator.(*terminalAuthenticator)
			if isTerminal {
				terminal.resend = client.Auth().ResendCode
			}
			err = client.Auth().IfNecessary(ctx, auth.NewFlow(authenticator, auth.SendCodeOptions{}))
			if err != nil {
				return fmt.Errorf("authenticate: %w", err)
			}
		}
		return fn(ctx, client)
	})
}
