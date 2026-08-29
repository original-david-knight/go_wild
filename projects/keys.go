package gowild_projects

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	projectKeyRe = regexp.MustCompile(`^[A-Z]{2,6}$`)
	itemKeyRe    = regexp.MustCompile(`^([A-Z]{2,6})-([0-9]{1,9})$`)
	agentNameRe  = regexp.MustCompile(`^[a-z0-9]{2,16}$`)
	mentionRe    = regexp.MustCompile(`(?:^|[^A-Za-z0-9_@])@([a-z][a-z0-9_-]*)`)
	slugStripRe  = regexp.MustCompile(`[^a-z0-9]+`)
)

// ValidProjectKey reports whether key is 2–6 uppercase ASCII letters.
func ValidProjectKey(key string) bool { return projectKeyRe.MatchString(key) }

// ValidAgentName reports whether name can be an agent: 2–16 lowercase
// letters and digits, and not the owner.
func ValidAgentName(name string) bool {
	return name != ActorOwner && agentNameRe.MatchString(name)
}

// ValidCLI reports whether cli is one the runner can spawn.
func ValidCLI(cli string) bool { return cli == CLIClaude || cli == CLICodex }

// ValidEffort reports whether effort is one the CLI accepts: low, medium,
// high or xhigh for either CLI, max for claude only, or "" for the CLI's
// own default.
func ValidEffort(cli, effort string) bool {
	if effort == "" {
		return true
	}
	if effort == EffortMax {
		return cli == CLIClaude
	}
	for _, e := range Efforts {
		if e == effort {
			return true
		}
	}
	return false
}

// ItemKey is the item's human key, "EA-12".
func ItemKey(project *Project, item *Item) string {
	return fmt.Sprintf("%s-%d", project.Key, item.Number)
}

// ParseItemKey splits "EA-12" into its project key and number. Lowercase
// input is accepted and upcased, since keys are typed by hand.
func ParseItemKey(s string) (string, int, error) {
	m := itemKeyRe.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(s)))
	if m == nil {
		return "", 0, fmt.Errorf("%w: item key %q is not KEY-N", ErrValidation, s)
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n <= 0 {
		return "", 0, fmt.Errorf("%w: item key %q has no number", ErrValidation, s)
	}
	return m[1], n, nil
}

// ExtractMentions returns the distinct @names in body, in order of first
// appearance, lowercased. Email addresses do not count: the character before
// the @ must not be a word character.
func ExtractMentions(body string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mentionRe.FindAllStringSubmatch(strings.ToLower(body), -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// joinMentions is the stored form of a mention list.
func joinMentions(names []string) string { return strings.Join(names, ",") }

// PriorityRank orders priorities: urgent 3 … low 0. Unknown ranks as normal.
func PriorityRank(p string) int {
	switch p {
	case PriorityUrgent:
		return 3
	case PriorityHigh:
		return 2
	case PriorityLow:
		return 0
	default:
		return 1
	}
}

// StatusOrder orders statuses for a board: the ones needing attention first,
// the finished ones last.
func StatusOrder(status string) int {
	for i, s := range []string{
		StatusPendingApproval, StatusBlocked, StatusApproved, StatusInReview,
		StatusInProgress, StatusOpen, StatusDone, StatusClosed,
	} {
		if s == status {
			return i
		}
	}
	return len(Statuses)
}

// Slug reduces a title to a branch-safe fragment of at most max runes.
func Slug(title string, max int) string {
	s := strings.Trim(slugStripRe.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if max > 0 && len(s) > max {
		s = strings.TrimRight(s[:max], "-")
	}
	return s
}

// BranchName is the branch an implement job works on: pm/EA-12-short-slug.
func BranchName(itemKey, title string) string {
	slug := Slug(title, 40)
	if slug == "" {
		return "pm/" + strings.ToLower(itemKey)
	}
	return "pm/" + strings.ToLower(itemKey) + "-" + slug
}

func validStatus(s string) bool {
	for _, v := range Statuses {
		if v == s {
			return true
		}
	}
	return false
}

func validType(s string) bool {
	for _, v := range Types {
		if v == s {
			return true
		}
	}
	return false
}

func validPriority(s string) bool {
	for _, v := range Priorities {
		if v == s {
			return true
		}
	}
	return false
}
