package mainfx

import (
	"context"

	"go.uber.org/fx"

	"github.com/Deimvis/tgrag/src/configs"
	"github.com/Deimvis/tgrag/src/storage"
)

func NewStorage(lc fx.Lifecycle, cfg configs.Config) (*storage.Storage, error) {
	s, err := storage.Open(cfg.DatabaseFilePath())
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: func(ctx context.Context) error {
		return s.Close()
	}})
	return s, nil
}
