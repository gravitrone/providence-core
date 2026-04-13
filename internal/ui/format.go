package ui

import (
	"fmt"
	"time"
)

// FormatTokens formats a token count compactly: "900", "1.3k", "45k", "1.2M".
func FormatTokens(n int) string {
	if n <= 0 {
		return "0"
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		f := float64(n) / 1000.0
		if f >= 100 {
			return fmt.Sprintf("%.0fk", f)
		}
		if f >= 10 {
			return fmt.Sprintf("%.0fk", f)
		}
		return fmt.Sprintf("%.1fk", f)
	}
	f := float64(n) / 1_000_000.0
	if f >= 10 {
		return fmt.Sprintf("%.0fM", f)
	}
	return fmt.Sprintf("%.1fM", f)
}

// FormatFileSize formats a byte count: "512B", "1.3KB", "4.5MB", "1.2GB".
func FormatFileSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	kb := float64(bytes) / 1024
	if kb < 1024 {
		if kb >= 10 {
			return fmt.Sprintf("%.0fKB", kb)
		}
		return fmt.Sprintf("%.1fKB", kb)
	}
	mb := kb / 1024
	if mb >= 10 {
		return fmt.Sprintf("%.0fMB", mb)
	}
	return fmt.Sprintf("%.1fMB", mb)
}

// FormatDuration formats a duration compactly: "3s", "2m 15s", "1h 5m".
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s > 0 {
			return fmt.Sprintf("%dm %ds", m, s)
		}
		return fmt.Sprintf("%dm", m)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dh", h)
}
