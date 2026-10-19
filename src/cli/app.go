package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"go.uber.org/fx"

	"github.com/Deimvis/tgrag/src/ask"
	"github.com/Deimvis/tgrag/src/configs"
	"github.com/Deimvis/tgrag/src/indexer"
	"github.com/Deimvis/tgrag/src/mainfx"
	"github.com/Deimvis/tgrag/src/tg"
	"github.com/Deimvis/tgrag/src/tools"
)

func runApp(ctx context.Context, configFilePath string, invoke any) error {
	app := fx.New(
		fx.NopLogger,
		fx.Provide(
			configs.LoaderFactory(configFilePath),
			mainfx.NewStorage,
			indexer.New,
			tg.New,
			tools.New,
			ask.New,
		),
		fx.Invoke(invoke),
	)
	err := app.Err()
	if err != nil {
		return unwrapFxError(err)
	}
	err = app.Start(ctx)
	if err != nil {
		return err
	}
	return app.Stop(ctx)
}

func unwrapFxError(err error) error {
	for {
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return err
		}
		inner := unwrapped.Unwrap()
		if inner == nil {
			return err
		}
		err = inner
	}
}

func printJson(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	err := enc.Encode(v)
	if err != nil {
		return fmt.Errorf("encode output: %w", err)
	}
	return nil
}
