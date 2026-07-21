package tools

import (
	"fmt"
	"strings"
)

// FormatIntervalForDisplay formats a minute interval into a compact human string.
// Examples: 30m, 1h30m, 1d1h.
func FormatIntervalForDisplay(minutes int) string {
	if minutes <= 0 {
		return "0m"
	}

	days := minutes / (24 * 60)
	remainder := minutes % (24 * 60)
	hours := remainder / 60
	mins := remainder % 60

	var b strings.Builder
	if days > 0 {
		b.WriteString(fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		b.WriteString(fmt.Sprintf("%dh", hours))
	}
	if mins > 0 || b.Len() == 0 {
		b.WriteString(fmt.Sprintf("%dm", mins))
	}

	return b.String()
}
