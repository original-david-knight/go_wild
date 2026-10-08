package gowild_knowledge

import (
	"regexp"
	"strings"
)

// relativeTime matches the relative time words a question carries ("this
// month", "upcoming"). Keyword search requires every term, and records
// rarely contain these words, so they would empty the keyword half; a
// question that has them ranks by age more sharply instead (recency).
var relativeTime = regexp.MustCompile(`(?i)\b((this|next|last)\s+(week|month|year)|today|tonight|tomorrow|yesterday|upcoming|coming\s+up|recently|recent|lately|latest)\b`)

// keywordText is the query text keyword search runs on: text without its
// relative time words, or text itself when nothing else is left.
func keywordText(text string) string {
	rest := strings.Join(strings.Fields(relativeTime.ReplaceAllString(text, " ")), " ")
	if rest == "" {
		return text
	}
	return rest
}
