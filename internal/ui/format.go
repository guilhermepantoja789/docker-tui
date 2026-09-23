package ui

import (
	"strconv"
	"strings"
)

func formatBytes(n uint64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case n >= gb:
		return strconv.FormatFloat(float64(n)/float64(gb), 'f', 1, 64) + "GB"
	case n >= mb:
		return strconv.FormatFloat(float64(n)/float64(mb), 'f', 1, 64) + "MB"
	case n >= kb:
		return strconv.FormatFloat(float64(n)/float64(kb), 'f', 1, 64) + "KB"
	default:
		return strconv.FormatUint(n, 10) + "B"
	}
}

func formatBps(n float64) string {
	if n < 0 {
		n = 0
	}
	return formatBytes(uint64(n)) + "/s"
}

func formatCPU(pct float64) string {
	return strconv.FormatFloat(pct, 'f', 1, 64) + "%"
}

func formatMem(usage, limit uint64) string {
	if limit == 0 {
		return formatBytes(usage)
	}
	return formatBytes(usage) + "/" + formatBytes(limit)
}

func formatSize(n int64) string {
	if n < 0 {
		n = 0
	}
	return formatBytes(uint64(n))
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func pad(s string, width int) string {
	if width <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	return s + strings.Repeat(" ", width-len(r))
}

func joinTags(tags []string) string {
	if len(tags) == 0 {
		return "<none>"
	}
	return strings.Join(tags, ", ")
}
