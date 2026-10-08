package gowild_knowledge

import (
	"regexp"
	"strings"
)

// relativeTime matches the relative time words a question carries ("this
// month", "upcoming"). Keyword search requires every term, and records
// rarely contain these words, so they would empty the keyword half.
var relativeTime = regexp.MustCompile(`(?i)\b((this|next|last)\s+(week|month|year)|today|tonight|tomorrow|yesterday|upcoming|coming\s+up|recently|recent|lately|latest)\b`)

// presentTime is the part of relativeTime that names a span of now or what
// comes next; a question with it ranks by age more sharply (recency).
// "Last week" and "yesterday" name a past span such a fade would bury, and
// "latest" or "recent" often ask for the current value of something durable
// ("latest phone number"), which the fade would bury under fresh mail.
var presentTime = regexp.MustCompile(`(?i)\b((this|next)\s+(week|month)|today|tonight|tomorrow|upcoming|coming\s+up)\b`)

// keywordText is the query text keyword search runs on: text without its
// relative time words, or text itself when nothing else is left.
func keywordText(text string) string {
	rest := strings.Join(strings.Fields(relativeTime.ReplaceAllString(text, " ")), " ")
	if rest == "" {
		return text
	}
	return rest
}
