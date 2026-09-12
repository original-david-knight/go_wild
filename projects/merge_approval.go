package gowild_projects

import "strings"

func validReviewCommit(commit string) bool {
	if len(commit) != 40 && len(commit) != 64 {
		return false
	}
	return strings.Trim(commit, "0123456789abcdef") == ""
}
