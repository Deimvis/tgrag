package configs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Deimvis-go/valid"
	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram   TelegramConfig   `yaml:"telegram"`
	Storage    StorageConfig    `yaml:"storage"`
	Sync       SyncConfig       `yaml:"sync"`
	Windows    WindowsConfig    `yaml:"windows"`
	Ask        AskConfig        `yaml:"ask"`
	Embeddings EmbeddingsConfig `yaml:"embeddings"`
}

type TelegramConfig struct {
	ApiId           int    `yaml:"api_id" validate:"required"`
	ApiHash         string `yaml:"api_hash"`
	ApiHashFilePath string `yaml:"api_hash_file_path"`
}

func (c *TelegramConfig) ValidateSelf() error {
	if (c.ApiHash == "") == (c.ApiHashFilePath == "") {
		return errors.New("telegram: exactly one of api_hash and api_hash_file_path must be set")
	}
	return nil
}

func (c *TelegramConfig) ResolveApiHash() (string, error) {
	if c.ApiHash != "" {
		return c.ApiHash, nil
	}
	raw, err := os.ReadFile(c.ApiHashFilePath)
	if err != nil {
		return "", fmt.Errorf("read api_hash_file_path: %w", err)
	}
	hash := strings.TrimSpace(string(raw))
	if hash == "" {
		return "", fmt.Errorf("api hash file %s is empty", c.ApiHashFilePath)
	}
	return hash, nil
}

type StorageConfig struct {
	DataDirectoryPath string `yaml:"data_directory_path" validate:"required"`
}

type SyncConfig struct {
	ChatSelection      ChatSelectionConfig `yaml:"chat_selection"`
	MaxMessagesPerChat int                 `yaml:"max_messages_per_chat"`
	IsSkippingChannels bool                `yaml:"is_skipping_channels"`
	IsSkippingBots     bool                `yaml:"is_skipping_bots"`
}

type ChatSelectionMode string

const (
	ChatSelectionModeUnset     ChatSelectionMode = ""
	ChatSelectionModeWhitelist ChatSelectionMode = "whitelist"
	ChatSelectionModeAll       ChatSelectionMode = "all"
)

type ChatSelectionConfig struct {
	Mode    ChatSelectionMode `yaml:"mode"`
	ChatIds []int64           `yaml:"chat_ids"`
}

func (c *ChatSelectionConfig) ValidateSelf() error {
	switch c.Mode {
	case ChatSelectionModeUnset, ChatSelectionModeWhitelist, ChatSelectionModeAll:
		return nil
	default:
		return fmt.Errorf("sync.chat_selection.mode must be one of %q, %q or empty", ChatSelectionModeWhitelist, ChatSelectionModeAll)
	}
}

func (c *ChatSelectionConfig) IsConfigured() bool {
	return c.Mode != ChatSelectionModeUnset
}

func (c *ChatSelectionConfig) Allows(chatId int64) bool {
	if c.Mode == ChatSelectionModeAll {
		return true
	}
	for _, id := range c.ChatIds {
		if id == chatId {
			return true
		}
	}
	return false
}

type WindowsConfig struct {
	GapMinutes int `yaml:"gap_minutes" validate:"required"`
	MaxChars   int `yaml:"max_chars" validate:"required"`
}

type AskConfig struct {
	Model         string `yaml:"model" validate:"required"`
	MaxIterations int    `yaml:"max_iterations" validate:"required"`
}

type EmbeddingsConfig struct {
	IsEnabled bool   `yaml:"is_enabled"`
	BaseUrl   string `yaml:"base_url" validate:"required_if=IsEnabled true"`
	Model     string `yaml:"model" validate:"required_if=IsEnabled true"`
	ApiKeyEnv string `yaml:"api_key_env"`
	BatchSize int    `yaml:"batch_size" validate:"required_if=IsEnabled true"`
}

func (c *EmbeddingsConfig) ResolveApiKey() string {
	if c.ApiKeyEnv == "" {
		return ""
	}
	return os.Getenv(c.ApiKeyEnv)
}

func NewDefaultEmbeddingsConfig(isEnabled bool) EmbeddingsConfig {
	return EmbeddingsConfig{
		IsEnabled: isEnabled,
		BaseUrl:   "http://localhost:11434/v1",
		Model:     "bge-m3",
		ApiKeyEnv: "",
		BatchSize: 16,
	}
}

func (c *Config) ValidateSelf() error {
	if c.Sync.MaxMessagesPerChat < 0 {
		return errors.New("sync.max_messages_per_chat must be non-negative")
	}
	if c.Embeddings.IsEnabled && c.Embeddings.BatchSize <= 0 {
		return errors.New("embeddings.batch_size must be positive")
	}
	return nil
}

func (c *Config) SessionFilePath() string {
	return filepath.Join(c.Storage.DataDirectoryPath, "session.json")
}

func (c *Config) DatabaseFilePath() string {
	return filepath.Join(c.Storage.DataDirectoryPath, "tgrag.db")
}

var validate = validator.New(validator.WithRequiredStructEnabled())

func DefaultConfigFilePath() string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "tgrag", "config.yaml")
}

func DefaultDataDirectoryPath() string {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "tgrag")
}

func NewDefault(apiId int, apiHash string) Config {
	return Config{
		Telegram: TelegramConfig{ApiId: apiId, ApiHash: apiHash},
		Storage:  StorageConfig{DataDirectoryPath: DefaultDataDirectoryPath()},
		Sync: SyncConfig{
			ChatSelection:      ChatSelectionConfig{Mode: ChatSelectionModeUnset, ChatIds: []int64{}},
			MaxMessagesPerChat: 0,
			IsSkippingChannels: true,
			IsSkippingBots:     true,
		},
		Windows:    WindowsConfig{GapMinutes: 20, MaxChars: 2500},
		Ask:        AskConfig{Model: "claude-opus-5-5", MaxIterations: 20},
		Embeddings: NewDefaultEmbeddingsConfig(false),
	}
}

func Load(path string) (Config, error) {
	var cfg Config
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	err = yaml.Unmarshal(raw, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	cfg.Storage.DataDirectoryPath = ExpandHome(cfg.Storage.DataDirectoryPath)
	cfg.Telegram.ApiHashFilePath = ExpandHome(cfg.Telegram.ApiHashFilePath)
	err = valid.XDeep(validate, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}

func ExpandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

func Save(path string, cfg Config) error {
	err := valid.XDeep(validate, &cfg)
	if err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("serialize config: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	err = os.WriteFile(path, raw, 0o600)
	if err != nil {
		target, linkErr := filepath.EvalSymlinks(path)
		if linkErr == nil && strings.HasPrefix(target, "/nix/store/") {
			return fmt.Errorf("config %s is a read-only nix store symlink (%s): edit the source file in your nix configuration or rebuild with impurity enabled so the symlink is writable", path, target)
		}
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func LoaderFactory(path string) func() (Config, error) {
	return func() (Config, error) {
		return Load(path)
	}
}
