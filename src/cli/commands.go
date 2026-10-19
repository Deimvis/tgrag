package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Deimvis/go-ext/go1.25/xoptional"
	"github.com/spf13/cobra"

	"github.com/Deimvis/tgrag/src/ask"
	"github.com/Deimvis/tgrag/src/configs"
	"github.com/Deimvis/tgrag/src/export"
	"github.com/Deimvis/tgrag/src/indexer"
	"github.com/Deimvis/tgrag/src/storage"
	"github.com/Deimvis/tgrag/src/tg"
	"github.com/Deimvis/tgrag/src/tools"
)

func NewRootCommand() *cobra.Command {
	var configFilePath string
	root := &cobra.Command{
		Use:           "tgrag",
		Short:         "Search and ask questions over your personal Telegram chats",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&configFilePath, "config", configs.DefaultConfigFilePath(), "config file path")
	root.AddCommand(
		newSetupCommand(&configFilePath),
		newLoginCommand(&configFilePath),
		newSyncCommand(&configFilePath),
		newImportCommand(&configFilePath),
		newReindexCommand(&configFilePath),
		newChatsCommand(&configFilePath),
		newSearchCommand(&configFilePath),
		newContextCommand(&configFilePath),
		newThreadCommand(&configFilePath),
		newStatsCommand(&configFilePath),
		newAskCommand(&configFilePath),
	)
	return root
}

func newLoginCommand(configFilePath *string) *cobra.Command {
	var apiId int
	var apiHash, phone string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate Telegram user account (MTProto) and store session; prefer `tgrag setup` for first-time use",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := ensureConfig(*configFilePath, apiId, apiHash)
			if err != nil {
				return err
			}
			return runApp(cmd.Context(), *configFilePath, func(client *tg.Client) error {
				return client.Login(cmd.Context(), tg.NewTerminalAuthenticator(phone))
			})
		},
	}
	cmd.Flags().IntVar(&apiId, "api-id", 0, "api_id from https://my.telegram.org (prompted if config does not exist)")
	cmd.Flags().StringVar(&apiHash, "api-hash", "", "api_hash from https://my.telegram.org (prompted if config does not exist)")
	cmd.Flags().StringVar(&phone, "phone", "", "phone number in international format (prompted if empty)")
	return cmd
}

func ensureConfig(path string, apiId int, apiHash string) error {
	if apiId == 0 && apiHash == "" {
		return ensureConfigInteractive(path)
	}
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if apiId == 0 || apiHash == "" {
			return fmt.Errorf("config %s does not exist, pass both --api-id and --api-hash or run without flags to be prompted", path)
		}
		return configs.Save(path, configs.NewDefault(apiId, apiHash))
	}
	if err != nil {
		return err
	}
	cfg, err := configs.Load(path)
	if err != nil {
		return err
	}
	if apiId != 0 {
		cfg.Telegram.ApiId = apiId
	}
	if apiHash != "" {
		cfg.Telegram.ApiHash = apiHash
	}
	return configs.Save(path, cfg)
}

func newSyncCommand(configFilePath *string) *cobra.Command {
	var chatFilter string
	var isFull bool
	var every time.Duration
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Incrementally download new messages of selected chats from Telegram and index them",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := tg.SyncOptions{ChatFilter: chatFilter, IsFull: isFull}
			if every <= 0 {
				return syncOnce(cmd.Context(), *configFilePath, opts)
			}
			return syncForever(cmd.Context(), *configFilePath, opts, every)
		},
	}
	cmd.Flags().StringVar(&chatFilter, "chat", "", "sync only chats matching id, title substring or @username")
	cmd.Flags().BoolVar(&isFull, "full", false, "drop local data for matched chats and download history again (picks up edits and deletions)")
	cmd.Flags().DurationVar(&every, "every", 0, "keep running and repeat sync with this interval (e.g. 30m); 0 syncs once")
	return cmd
}

func syncOnce(ctx context.Context, configFilePath string, opts tg.SyncOptions) error {
	return runApp(ctx, configFilePath, func(client *tg.Client) error {
		result, err := client.Sync(ctx, opts)
		if err != nil {
			return err
		}
		return printJson(result)
	})
}

func syncForever(ctx context.Context, configFilePath string, opts tg.SyncOptions, every time.Duration) error {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		err := syncOnce(ctx, configFilePath, opts)
		if errors.Is(err, tg.ErrChatSelectionNotConfigured) {
			return err
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "sync failed: %v (retrying in %s)\n", err, every)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func newImportCommand(configFilePath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <result.json>",
		Short: "Import Telegram Desktop JSON export (Settings → Advanced → Export) and index it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApp(cmd.Context(), *configFilePath, func(s *storage.Storage, idx *indexer.Indexer) error {
				return importExport(cmd.Context(), s, idx, args[0])
			})
		},
	}
	return cmd
}

func importExport(ctx context.Context, s *storage.Storage, idx *indexer.Indexer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	chats, err := export.Parse(f)
	if err != nil {
		return err
	}
	summary := map[string]int{"chats": 0, "messages": 0, "windows": 0}
	for _, c := range chats {
		err = s.UpsertChat(ctx, c.Chat)
		if err != nil {
			return err
		}
		err = s.UpsertMessages(ctx, c.Messages)
		if err != nil {
			return err
		}
		windowCount, err := idx.ReindexChat(ctx, c.Chat.Id)
		if err != nil {
			return err
		}
		summary["chats"]++
		summary["messages"] += len(c.Messages)
		summary["windows"] += windowCount
	}
	return printJson(summary)
}

func newReindexCommand(configFilePath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild conversation windows and the full-text index from stored messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApp(cmd.Context(), *configFilePath, func(idx *indexer.Indexer) error {
				count, err := idx.ReindexAll(cmd.Context())
				if err != nil {
					return err
				}
				return printJson(map[string]int{"windows": count})
			})
		},
	}
}

func newChatsCommand(configFilePath *string) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "chats [query]",
		Short: "List synced chats, optionally filtered by title/username substring",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApp(cmd.Context(), *configFilePath, func(r *tools.Registry) error {
				result, err := r.ListChats(cmd.Context(), tools.ListChatsInput{Query: strings.Join(args, " "), Limit: limit})
				if err != nil {
					return err
				}
				return printJson(result)
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "max chats")
	return cmd
}

func newSearchCommand(configFilePath *string) *cobra.Command {
	var in tools.SearchInput
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search over conversation windows (FTS5 syntax: term*, OR, NOT, \"phrase\")",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in.Query = strings.Join(args, " ")
			return runApp(cmd.Context(), *configFilePath, func(r *tools.Registry) error {
				result, err := r.Search(cmd.Context(), in)
				if err != nil {
					return err
				}
				return printJson(result)
			})
		},
	}
	cmd.Flags().Int64Var(&in.ChatId, "chat", 0, "restrict to chat id")
	cmd.Flags().StringVar(&in.Sender, "sender", "", "restrict to windows with this participant name substring")
	cmd.Flags().StringVar(&in.After, "after", "", "only windows ending after date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&in.Before, "before", "", "only windows starting before date (YYYY-MM-DD)")
	cmd.Flags().IntVar(&in.Limit, "limit", tools.DefaultSearchLimit, "max hits")
	return cmd
}

func newContextCommand(configFilePath *string) *cobra.Command {
	var in tools.GetContextInput
	var before, after int
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Print raw messages around a message",
		RunE: func(cmd *cobra.Command, args []string) error {
			in.Before = xoptional.New(before)
			in.After = xoptional.New(after)
			return runApp(cmd.Context(), *configFilePath, func(r *tools.Registry) error {
				result, err := r.GetContext(cmd.Context(), in)
				if err != nil {
					return err
				}
				return printJson(result)
			})
		},
	}
	cmd.Flags().Int64Var(&in.ChatId, "chat", 0, "chat id")
	cmd.Flags().Int64Var(&in.MessageId, "message", 0, "message id")
	cmd.Flags().IntVar(&before, "before", tools.DefaultContextSize, "messages before")
	cmd.Flags().IntVar(&after, "after", tools.DefaultContextSize, "messages after")
	_ = cmd.MarkFlagRequired("chat")
	_ = cmd.MarkFlagRequired("message")
	return cmd
}

func newThreadCommand(configFilePath *string) *cobra.Command {
	var in tools.GetThreadInput
	cmd := &cobra.Command{
		Use:   "thread",
		Short: "Print the reply chain of a message",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApp(cmd.Context(), *configFilePath, func(r *tools.Registry) error {
				result, err := r.GetThread(cmd.Context(), in)
				if err != nil {
					return err
				}
				return printJson(result)
			})
		},
	}
	cmd.Flags().Int64Var(&in.ChatId, "chat", 0, "chat id")
	cmd.Flags().Int64Var(&in.MessageId, "message", 0, "message id")
	_ = cmd.MarkFlagRequired("chat")
	_ = cmd.MarkFlagRequired("message")
	return cmd
}

func newStatsCommand(configFilePath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Print index statistics and latest sync time",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApp(cmd.Context(), *configFilePath, func(s *storage.Storage) error {
				result, err := s.Stats(cmd.Context())
				if err != nil {
					return err
				}
				return printJson(result)
			})
		},
	}
}

func newAskCommand(configFilePath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "ask <question>",
		Short: "Answer a question about your chats with Claude (needs ANTHROPIC_API_KEY)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApp(cmd.Context(), *configFilePath, func(agent *ask.Agent) error {
				answer, err := agent.Run(cmd.Context(), strings.Join(args, " "))
				if err != nil {
					return err
				}
				fmt.Println(answer)
				return nil
			})
		},
	}
}
