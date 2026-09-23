package ui

import (
	"strings"
	"testing"
)

func TestFilterLines_EmptyPassThrough(t *testing.T) {
	lines := []string{"a", "b", "c"}
	out, matched, total := filterLines(lines, paneFilter{})
	if matched != 3 || total != 3 || len(out) != 3 {
		t.Fatalf("got matched=%d total=%d len=%d", matched, total, len(out))
	}
}

func TestFilterLines_RegexCaseInsensitive(t *testing.T) {
	lines := []string{"Hello", "world", "HELLO again"}
	f := paneFilter{query: "hello", caseSens: false}
	f.compile()
	out, matched, total := filterLines(lines, f)
	if total != 3 || matched != 2 {
		t.Fatalf("matched=%d total=%d, want 2/3", matched, total)
	}
	if len(out) != 2 || out[0] != "Hello" || out[1] != "HELLO again" {
		t.Fatalf("out=%v", out)
	}
}

func TestFilterLines_CaseSensitive(t *testing.T) {
	lines := []string{"Error", "error", "ERROR"}
	f := paneFilter{query: "error", caseSens: true}
	f.compile()
	out, matched, _ := filterLines(lines, f)
	if matched != 1 || out[0] != "error" {
		t.Fatalf("matched=%d out=%v, want only lowercase error", matched, out)
	}
}

func TestFilterLines_Invert(t *testing.T) {
	lines := []string{"keep", "drop me", "keep2"}
	f := paneFilter{query: "drop", invert: true}
	f.compile()
	out, matched, total := filterLines(lines, f)
	if total != 3 || matched != 2 {
		t.Fatalf("matched=%d total=%d", matched, total)
	}
	if strings.Join(out, ",") != "keep,keep2" {
		t.Fatalf("out=%v", out)
	}
}

func TestFilterLines_InvalidShowsAll(t *testing.T) {
	lines := []string{"a", "b"}
	f := paneFilter{query: "[unterminated"}
	f.compile()
	if !f.invalid {
		t.Fatal("expected invalid regex")
	}
	out, matched, total := filterLines(lines, f)
	if matched != 2 || total != 2 || len(out) != 2 {
		t.Fatalf("invalid should pass all: matched=%d total=%d len=%d", matched, total, len(out))
	}
}

func TestFilterLines_ErrorPreset(t *testing.T) {
	lines := []string{
		"info ok",
		"ERROR: boom",
		"something fatal happened",
		"warn only",
		"panic: nil",
		"stderr: err code",
	}
	f := paneFilter{query: filterPresetError}
	f.compile()
	out, matched, total := filterLines(lines, f)
	if total != 6 {
		t.Fatalf("total=%d", total)
	}
	if matched != 4 {
		t.Fatalf("matched=%d out=%v, want 4 error-like lines", matched, out)
	}
}

func TestFilterLines_WarnPreset(t *testing.T) {
	lines := []string{"ok", "WARN: disk", "warning: slow", "error"}
	f := paneFilter{query: filterPresetWarn}
	f.compile()
	out, matched, _ := filterLines(lines, f)
	if matched != 2 {
		t.Fatalf("matched=%d out=%v, want 2", matched, out)
	}
}

func TestLogGridDims(t *testing.T) {
	var m Model
	cases := []struct {
		n, rows, cols int
	}{
		{0, 1, 1},
		{1, 1, 1},
		{2, 1, 2},
		{3, 2, 2},
		{4, 2, 2},
	}
	for _, tc := range cases {
		rows, cols := m.logGridDims(tc.n)
		if rows != tc.rows || cols != tc.cols {
			t.Fatalf("n=%d got %dx%d want %dx%d", tc.n, rows, cols, tc.rows, tc.cols)
		}
	}
}
