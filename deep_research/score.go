package deepresearch

import (
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

var tokenSplitRE = regexp.MustCompile(`[^a-z0-9]+`)

var stopWords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {}, "by": {},
	"for": {}, "from": {}, "in": {}, "into": {}, "is": {}, "it": {}, "of": {}, "on": {},
	"or": {}, "that": {}, "the": {}, "their": {}, "this": {}, "to": {}, "was": {}, "with": {},
}

func scoreForHit(baseQuery string, objective Objective, hit SearchHit, rank, depth int, now time.Time) float64 {
	rankScore := 1.0 / float64(rank+1)
	relevance := tokenOverlap(baseQuery+" "+objective.Key+" "+objective.Description, hit.Title+" "+hit.Snippet)
	trust := domainTrustScore(hit.URL)

	score := (0.50 * rankScore) + (0.35 * relevance) + (0.15 * trust) - (0.08 * float64(depth))
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

func domainTrustScore(rawURL string) float64 {
	host := strings.ToLower(strings.TrimSpace(domainFromURL(rawURL)))
	if host == "" {
		return 0.4
	}
	if strings.HasSuffix(host, ".gov") || strings.HasSuffix(host, ".edu") {
		return 1.0
	}
	if strings.HasSuffix(host, ".org") {
		return 0.8
	}
	highTrust := []string{
		"reuters.com",
		"apnews.com",
		"bloomberg.com",
		"wsj.com",
		"ft.com",
		"sec.gov",
		"who.int",
		"imf.org",
		"worldbank.org",
	}
	for _, suffix := range highTrust {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return 0.95
		}
	}
	return 0.6
}

func domainFromURL(rawURL string) string {
	if strings.TrimSpace(rawURL) == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func tokenOverlap(a, b string) float64 {
	aTokens := tokenize(a)
	bTokens := tokenize(b)
	if len(aTokens) == 0 || len(bTokens) == 0 {
		return 0
	}

	setA := make(map[string]struct{}, len(aTokens))
	for _, token := range aTokens {
		setA[token] = struct{}{}
	}
	setB := make(map[string]struct{}, len(bTokens))
	for _, token := range bTokens {
		setB[token] = struct{}{}
	}

	matches := 0
	for token := range setA {
		if _, ok := setB[token]; ok {
			matches++
		}
	}
	union := len(setA) + len(setB) - matches
	if union <= 0 {
		return 0
	}
	return float64(matches) / float64(union)
}

func tokenize(value string) []string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil
	}
	pieces := tokenSplitRE.Split(value, -1)
	out := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		if len(piece) < 3 {
			continue
		}
		if _, isStopWord := stopWords[piece]; isStopWord {
			continue
		}
		out = append(out, piece)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	uniq := out[:0]
	prev := ""
	for _, token := range out {
		if token == prev {
			continue
		}
		uniq = append(uniq, token)
		prev = token
	}
	return uniq
}

func round3(value float64) float64 {
	return math.Round(value*1000) / 1000
}
