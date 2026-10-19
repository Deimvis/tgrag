package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/Deimvis/tgrag/src/configs"
	"github.com/Deimvis/tgrag/src/models"
	"github.com/Deimvis/tgrag/src/tg"
)

func newSetupCommand(configFilePath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive first-time setup: Telegram login, choose which chats to sync, run first sync",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(cmd.Context(), *configFilePath)
		},
	}
}

func runSetup(ctx context.Context, configFilePath string) error {
	err := ensureConfigInteractive(configFilePath)
	if err != nil {
		return err
	}
	err = runApp(ctx, configFilePath, func(client *tg.Client) error {
		return client.Login(ctx, tg.NewTerminalAuthenticator(""))
	})
	if err != nil {
		return err
	}
	var dialogs []models.Chat
	err = runApp(ctx, configFilePath, func(client *tg.Client) error {
		fmt.Fprintln(os.Stderr, "loading chat list from Telegram...")
		dialogs, err = client.ListDialogs(ctx)
		return err
	})
	if err != nil {
		return err
	}
	cfg, err := configs.Load(configFilePath)
	if err != nil {
		return err
	}
	selection, err := promptChatSelection(dialogs, cfg.Sync.ChatSelection)
	if err != nil {
		return err
	}
	cfg.Sync.ChatSelection = selection
	err = configs.Save(configFilePath, cfg)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "chat selection saved to %s\n", configFilePath)

	var isSyncingNow bool
	err = huh.NewConfirm().
		Title("Run the first sync now?").
		Description("Downloads the full text history of selected chats, may take a while for big chats. You can run `tgrag sync` later instead.").
		Affirmative("Sync now").
		Negative("Later").
		Value(&isSyncingNow).
		Run()
	if err != nil {
		return err
	}
	if !isSyncingNow {
		return nil
	}
	return runApp(ctx, configFilePath, func(client *tg.Client) error {
		result, err := client.Sync(ctx, tg.SyncOptions{})
		if err != nil {
			return err
		}
		return printJson(result)
	})
}

func ensureConfigInteractive(configFilePath string) error {
	_, err := os.Stat(configFilePath)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var apiIdRaw, apiHash string
	err = huh.NewForm(huh.NewGroup(
		huh.NewNote().
			Title("Telegram application credentials").
			Description("tgrag talks to Telegram as a regular client app, so Telegram requires app credentials once.\nGet them at https://my.telegram.org → API development tools → create any app.\nThey identify the app, not you; your phone login comes next."),
		huh.NewInput().
			Title("api_id").
			Validate(func(s string) error {
				_, err := strconv.Atoi(strings.TrimSpace(s))
				if err != nil {
					return errors.New("api_id is a number")
				}
				return nil
			}).
			Value(&apiIdRaw),
		huh.NewInput().
			Title("api_hash").
			EchoMode(huh.EchoModePassword).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("api_hash is required")
				}
				return nil
			}).
			Value(&apiHash),
	)).Run()
	if err != nil {
		return err
	}
	apiId, err := strconv.Atoi(strings.TrimSpace(apiIdRaw))
	if err != nil {
		return err
	}
	return configs.Save(configFilePath, configs.NewDefault(apiId, strings.TrimSpace(apiHash)))
}

func promptChatSelection(dialogs []models.Chat, current configs.ChatSelectionConfig) (configs.ChatSelectionConfig, error) {
	mode := current.Mode
	if mode == configs.ChatSelectionModeUnset {
		mode = configs.ChatSelectionModeWhitelist
	}
	err := huh.NewSelect[configs.ChatSelectionMode]().
		Title("Which chats should be synced and indexed?").
		Description("Only text of selected chats is stored locally. The index is searchable by anyone with access to this machine.").
		Options(
			huh.NewOption("Selected chats only (recommended)", configs.ChatSelectionModeWhitelist),
			huh.NewOption("⚠ DANGEROUS: ALL chats, including every group and private conversation", configs.ChatSelectionModeAll),
		).
		Value(&mode).
		Run()
	if err != nil {
		return current, err
	}
	if mode == configs.ChatSelectionModeAll {
		var isConfirmed bool
		err = huh.NewConfirm().
			Title("Really index ALL chats?").
			Description("Every chat you have access to will be downloaded and indexed, including chats of people who did not consent. New chats are picked up automatically on each sync.").
			Affirmative("Yes, all chats").
			Negative("No, let me choose").
			Value(&isConfirmed).
			Run()
		if err != nil {
			return current, err
		}
		if isConfirmed {
			return configs.ChatSelectionConfig{Mode: configs.ChatSelectionModeAll, ChatIds: []int64{}}, nil
		}
	}
	chatIds, err := promptWhitelist(dialogs, current.ChatIds)
	if err != nil {
		return current, err
	}
	return configs.ChatSelectionConfig{Mode: configs.ChatSelectionModeWhitelist, ChatIds: chatIds}, nil
}

func promptWhitelist(dialogs []models.Chat, currentIds []int64) ([]int64, error) {
	currentSet := map[int64]struct{}{}
	for _, id := range currentIds {
		currentSet[id] = struct{}{}
	}
	options := make([]huh.Option[int64], 0, len(dialogs))
	for _, d := range dialogs {
		_, isSelected := currentSet[d.Id]
		options = append(options, huh.NewOption(formatDialogOption(d), d.Id).Selected(isSelected))
	}
	selected := append([]int64{}, currentIds...)
	err := huh.NewMultiSelect[int64]().
		Title("Select chats to sync").
		Description("space: toggle, type to filter, enter: confirm").
		Options(options...).
		Filterable(true).
		Height(20).
		Validate(func(ids []int64) error {
			if len(ids) == 0 {
				return errors.New("select at least one chat")
			}
			return nil
		}).
		Value(&selected).
		Run()
	if err != nil {
		return nil, err
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i] < selected[j] })
	return selected, nil
}

func formatDialogOption(chat models.Chat) string {
	label := fmt.Sprintf("[%s] %s", chat.Kind, chat.Title)
	if chat.Username != "" {
		label += " @" + chat.Username
	}
	return label
}
