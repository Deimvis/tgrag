package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
)

const (
	DefaultRepositoryUrl = "https://github.com/Deimvis/tgrag"
	DefaultRef           = "main"
	nixStorePrefix       = "/nix/store/"
)

type Version struct {
	Revision   string `json:"revision"`
	CommitTs   string `json:"commit_ts,omitempty"`
	IsModified bool   `json:"is_modified"`
	GoVersion  string `json:"go_version"`
	Executable string `json:"executable_path"`
}

func CurrentVersion() Version {
	v := Version{Revision: "unknown", GoVersion: runtime.Version()}
	exe, err := ExecutablePath()
	if err == nil {
		v.Executable = exe
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			v.Revision = setting.Value
		case "vcs.time":
			v.CommitTs = setting.Value
		case "vcs.modified":
			v.IsModified = setting.Value == "true"
		}
	}
	return v
}

func ExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe, nil
	}
	return resolved, nil
}

func IsNixStorePath(path string) bool {
	return strings.HasPrefix(path, nixStorePrefix)
}

type Options struct {
	RepositoryUrl string
	Ref           string
	IsForced      bool
}

type Result struct {
	PreviousRevision string `json:"previous_revision"`
	NewRevision      string `json:"new_revision"`
	ExecutablePath   string `json:"executable_path"`
	IsUpdated        bool   `json:"is_updated"`
}

func Update(ctx context.Context, opts Options, log io.Writer) (Result, error) {
	current := CurrentVersion()
	result := Result{PreviousRevision: current.Revision, ExecutablePath: current.Executable}
	if current.Executable == "" {
		return result, errors.New("cannot determine executable path")
	}
	if IsNixStorePath(current.Executable) {
		return result, fmt.Errorf("%s is installed by nix, update with your nix configuration (e.g. `nix flake update tgrag` or `nix profile upgrade`) instead", current.Executable)
	}
	for _, tool := range []string{"git", "go"} {
		_, err := exec.LookPath(tool)
		if err != nil {
			return result, fmt.Errorf("%s is required for self update but not found in PATH", tool)
		}
	}
	remoteRevision, err := remoteHeadRevision(ctx, opts.RepositoryUrl, opts.Ref)
	if err != nil {
		return result, err
	}
	result.NewRevision = remoteRevision
	if !opts.IsForced && remoteRevision == current.Revision && !current.IsModified {
		fmt.Fprintf(log, "already up to date (%s)\n", shortRevision(remoteRevision))
		return result, nil
	}
	workDir, err := os.MkdirTemp("", "tgrag-self-update-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(workDir)
	sourceDir := filepath.Join(workDir, "src")
	fmt.Fprintf(log, "cloning %s (%s)...\n", opts.RepositoryUrl, opts.Ref)
	err = runCommand(ctx, workDir, log, "git", "clone", "--quiet", "--depth", "1", "--branch", opts.Ref, opts.RepositoryUrl, sourceDir)
	if err != nil {
		return result, err
	}
	builtPath := filepath.Join(workDir, "tgrag")
	fmt.Fprintln(log, "building...")
	err = runCommand(ctx, sourceDir, log, "go", "build", "-o", builtPath, "./cmd/tgrag")
	if err != nil {
		return result, err
	}
	err = ReplaceExecutable(current.Executable, builtPath)
	if err != nil {
		return result, err
	}
	result.IsUpdated = true
	fmt.Fprintf(log, "updated %s: %s → %s\n", current.Executable, shortRevision(current.Revision), shortRevision(remoteRevision))
	return result, nil
}

func remoteHeadRevision(ctx context.Context, repositoryUrl string, ref string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-remote", repositoryUrl, ref)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git ls-remote %s %s: %w: %s", repositoryUrl, ref, err, strings.TrimSpace(stderr.String()))
	}
	return ParseLsRemoteRevision(stdout.String(), ref)
}

func ParseLsRemoteRevision(output string, ref string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if fields[1] == ref || fields[1] == "refs/heads/"+ref || fields[1] == "refs/tags/"+ref {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("ref %q not found in remote repository", ref)
}

func runCommand(ctx context.Context, dir string, log io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = log
	cmd.Stderr = log
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func ReplaceExecutable(targetPath string, newBinaryPath string) error {
	source, err := os.Open(newBinaryPath)
	if err != nil {
		return err
	}
	defer source.Close()
	stagingPath := targetPath + ".new"
	staging, err := os.OpenFile(stagingPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("write %s: %w", stagingPath, err)
	}
	_, err = io.Copy(staging, source)
	if err != nil {
		staging.Close()
		os.Remove(stagingPath)
		return fmt.Errorf("write %s: %w", stagingPath, err)
	}
	err = staging.Close()
	if err != nil {
		os.Remove(stagingPath)
		return err
	}
	err = os.Rename(stagingPath, targetPath)
	if err != nil {
		os.Remove(stagingPath)
		return fmt.Errorf("replace %s: %w", targetPath, err)
	}
	return nil
}

func shortRevision(revision string) string {
	if len(revision) > 12 {
		return revision[:12]
	}
	return revision
}
