package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/fang"

	"github.com/Deimvis/tgrag/src/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err := fang.Execute(ctx, cli.NewRootCommand())
	if err != nil {
		os.Exit(1)
	}
}
