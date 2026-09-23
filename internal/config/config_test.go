package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/guilhermepantoja789/docker-tui/internal/config"
)

func TestParse_LogFlags(t *testing.T) {
	cfg, err := config.Parse([]string{
		"--log-tail", "100",
		"--log-buffer", "1000",
		"--stats-concurrency", "4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogTail != "100" || cfg.LogBuffer != 1000 {
		t.Fatalf("got tail=%q buffer=%d", cfg.LogTail, cfg.LogBuffer)
	}
	if cfg.StatsInterval != 2*time.Second {
		t.Fatalf("default stats-interval=%v", cfg.StatsInterval)
	}
}

func TestParse_RejectsTinyLogBuffer(t *testing.T) {
	_, err := config.Parse([]string{"--log-buffer", "10"})
	if err == nil || !strings.Contains(err.Error(), "log-buffer") {
		t.Fatalf("got err=%v", err)
	}
}
