package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
)

// Config holds runtime settings for the TUI and metrics collector.
type Config struct {
	Context           string
	Host              string
	StatsConcurrency  int
	StatsInterval     time.Duration
	ReconcileInterval time.Duration
	UIRefreshInterval time.Duration
	StatsTimeout      time.Duration
	ViewportBuffer    int
	LogTail           string
	LogBuffer         int
}

// Parse reads flags from args (typically os.Args[1:]).
func Parse(args []string) (Config, error) {
	fs := flag.NewFlagSet("docker-tui", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var cfg Config
	fs.StringVar(&cfg.Context, "context", "", "Docker context name (overrides DOCKER_HOST when set)")
	fs.StringVar(&cfg.Host, "host", "", "Docker daemon host URL (e.g. unix:///var/run/docker.sock, tcp://host:2375)")
	fs.IntVar(&cfg.StatsConcurrency, "stats-concurrency", 16, "max concurrent one-shot stats requests")
	fs.DurationVar(&cfg.StatsInterval, "stats-interval", 2*time.Second, "interval between stats sampling rounds")
	fs.DurationVar(&cfg.ReconcileInterval, "reconcile-interval", 45*time.Second, "full inventory reconcile interval")
	fs.DurationVar(&cfg.UIRefreshInterval, "ui-fps", 200*time.Millisecond, "UI refresh interval (approx; 200ms ≈ 5 Hz)")
	fs.DurationVar(&cfg.StatsTimeout, "stats-timeout", 5*time.Second, "per-container stats request timeout")
	fs.IntVar(&cfg.ViewportBuffer, "viewport-buffer", 5, "extra rows above/below viewport to sample eagerly")
	fs.StringVar(&cfg.LogTail, "log-tail", "200", "initial lines to fetch when opening container logs")
	fs.IntVar(&cfg.LogBuffer, "log-buffer", 5000, "max retained log lines per pane")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Config{}, err
		}
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks configuration bounds.
func (c Config) Validate() error {
	if c.StatsConcurrency < 1 {
		return fmt.Errorf("stats-concurrency must be >= 1")
	}
	if c.StatsInterval < 100*time.Millisecond {
		return fmt.Errorf("stats-interval must be >= 100ms")
	}
	if c.UIRefreshInterval < 50*time.Millisecond {
		return fmt.Errorf("ui-fps interval must be >= 50ms")
	}
	if c.ViewportBuffer < 0 {
		return fmt.Errorf("viewport-buffer must be >= 0")
	}
	if c.LogTail == "" {
		return fmt.Errorf("log-tail must not be empty")
	}
	if c.LogBuffer < 100 {
		return fmt.Errorf("log-buffer must be >= 100")
	}
	return nil
}
