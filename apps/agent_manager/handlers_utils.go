package main

import (
	"strconv"
	"strings"
	"time"
)

// normalizeDuration ensures a duration string is valid for Go's time.ParseDuration.
// If the input is a bare number (e.g. "60"), it is treated as minutes ("60m").
func normalizeDuration(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return s
	}
	// If it's already a valid duration, keep it
	if _, err := time.ParseDuration(s); err == nil {
		return s
	}
	// If it's a bare number, assume minutes
	if _, err := strconv.Atoi(s); err == nil {
		return s + "m"
	}
	return s
}
