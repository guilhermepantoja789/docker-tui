package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/guilhermepantoja789/docker-tui/internal/collector"
	"github.com/guilhermepantoja789/docker-tui/internal/config"
	"github.com/guilhermepantoja789/docker-tui/internal/dockerx"
	"github.com/guilhermepantoja789/docker-tui/internal/ui"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "docker-tui: %v\n", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handle := collector.NewHandle(cfg)
	defer handle.Close()

	opts := dockerx.Options{Host: cfg.Host, Context: cfg.Context}
	if err := handle.Switch(ctx, opts); err != nil {
		fmt.Fprintf(os.Stderr, "docker-tui: %v\n", err)
		return 1
	}

	onHost := func(next dockerx.Options) error {
		if cfg.Host != "" && next.Host == "" && next.Context != "" {
			// allow context switch to override initial --host
			next.Host = ""
		}
		return handle.Switch(ctx, next)
	}

	if err := ui.Run(ctx, cfg, handle, onHost); err != nil {
		fmt.Fprintf(os.Stderr, "docker-tui: %v\n", err)
		return 1
	}
	return 0
}
