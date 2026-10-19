package selfupdate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLsRemoteRevision(t *testing.T) {
	output := "abc123\tHEAD\ndef456\trefs/heads/main\n789aaa\trefs/tags/v1.0.0\n"
	t.Run("branch", func(t *testing.T) {
		act, err := ParseLsRemoteRevision(output, "main")
		require.NoError(t, err)
		require.Equal(t, "def456", act)
	})
	t.Run("tag", func(t *testing.T) {
		act, err := ParseLsRemoteRevision(output, "v1.0.0")
		require.NoError(t, err)
		require.Equal(t, "789aaa", act)
	})
	t.Run("missing", func(t *testing.T) {
		_, err := ParseLsRemoteRevision(output, "dev")
		require.Error(t, err)
	})
	t.Run("empty output", func(t *testing.T) {
		_, err := ParseLsRemoteRevision("", "main")
		require.Error(t, err)
	})
}

func TestIsNixStorePath(t *testing.T) {
	require.True(t, IsNixStorePath("/nix/store/abc-tgrag-0.1.0/bin/tgrag"))
	require.False(t, IsNixStorePath("/Users/me/.local/bin/tgrag"))
}

func TestReplaceExecutable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "tgrag")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o755))
	fresh := filepath.Join(dir, "fresh")
	require.NoError(t, os.WriteFile(fresh, []byte("new"), 0o644))
	t.Run("replaces content and keeps executable mode", func(t *testing.T) {
		require.NoError(t, ReplaceExecutable(target, fresh))
		act, err := os.ReadFile(target)
		require.NoError(t, err)
		require.Equal(t, "new", string(act))
		info, err := os.Stat(target)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
		_, err = os.Stat(target + ".new")
		require.True(t, os.IsNotExist(err))
	})
	t.Run("missing source", func(t *testing.T) {
		require.Error(t, ReplaceExecutable(target, filepath.Join(dir, "nope")))
	})
}

func TestCurrentVersion(t *testing.T) {
	v := CurrentVersion()
	require.NotEmpty(t, v.GoVersion)
	require.NotEmpty(t, v.Executable)
}
