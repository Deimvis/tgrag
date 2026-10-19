package configs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatSelection(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		sel := ChatSelectionConfig{}
		require.False(t, sel.IsConfigured())
		require.False(t, sel.Allows(1))
	})
	t.Run("whitelist", func(t *testing.T) {
		sel := ChatSelectionConfig{Mode: ChatSelectionModeWhitelist, ChatIds: []int64{1, 2}}
		require.True(t, sel.IsConfigured())
		require.True(t, sel.Allows(2))
		require.False(t, sel.Allows(3))
	})
	t.Run("all", func(t *testing.T) {
		sel := ChatSelectionConfig{Mode: ChatSelectionModeAll}
		require.True(t, sel.IsConfigured())
		require.True(t, sel.Allows(3))
	})
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	t.Run("roundtrip", func(t *testing.T) {
		exp := NewDefault(42, "hash")
		require.NoError(t, Save(path, exp))
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		act, err := Load(path)
		require.NoError(t, err)
		require.Equal(t, exp, act)
		require.Equal(t, filepath.Join(exp.Storage.DataDirectoryPath, "tgrag.db"), act.DatabaseFilePath())
	})
	t.Run("missing required field", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path, []byte("telegram: {api_id: 1}\n"), 0o600))
		_, err := Load(path)
		require.Error(t, err)
	})
	t.Run("negative max messages", func(t *testing.T) {
		cfg := NewDefault(1, "h")
		cfg.Sync.MaxMessagesPerChat = -1
		require.Error(t, Save(path, cfg))
	})
	t.Run("invalid chat selection mode", func(t *testing.T) {
		cfg := NewDefault(1, "h")
		cfg.Sync.ChatSelection.Mode = "some"
		require.Error(t, Save(path, cfg))
	})
	t.Run("api hash from file and home expansion", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		require.NoError(t, os.WriteFile(filepath.Join(home, "hash.txt"), []byte("secret\n"), 0o600))
		require.NoError(t, os.WriteFile(path, []byte(`
telegram: {api_id: 1, api_hash_file_path: ~/hash.txt}
storage: {data_directory_path: ~/data}
sync: {chat_selection: {mode: all, chat_ids: []}, max_messages_per_chat: 0, is_skipping_channels: true, is_skipping_bots: true}
windows: {gap_minutes: 20, max_chars: 2500}
ask: {model: m, max_iterations: 1}
`), 0o600))
		act, err := Load(path)
		require.NoError(t, err)
		require.Equal(t, filepath.Join(home, "data"), act.Storage.DataDirectoryPath)
		hash, err := act.Telegram.ResolveApiHash()
		require.NoError(t, err)
		require.Equal(t, "secret", hash)
	})
	t.Run("both api hash sources set", func(t *testing.T) {
		cfg := NewDefault(1, "h")
		cfg.Telegram.ApiHashFilePath = "/x"
		require.Error(t, Save(path, cfg))
	})
	t.Run("missing file", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
		require.Error(t, err)
	})
}
