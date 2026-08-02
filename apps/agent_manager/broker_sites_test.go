package main

import (
	"testing"
)

func TestSiteSlugValidation(t *testing.T) {
	tests := []struct {
		slug  string
		valid bool
	}{
		{"my-site", true},
		{"a", false},       // too short (min 3)
		{"ab", false},      // too short
		{"abc", true},      // exactly 3
		{"-abc", false},    // starts with hyphen
		{"abc-", false},    // ends with hyphen
		{"ABC", false},     // uppercase
		{"my site", false}, // space
		{"api", true},      // passes regex but is reserved
	}
	for _, tt := range tests {
		if siteSlugRegex.MatchString(tt.slug) != tt.valid {
			t.Errorf("slug %q: expected valid=%v", tt.slug, tt.valid)
		}
	}
}

func TestSiteReservedSlugs(t *testing.T) {
	reserved := []string{"api", "admin", "static", "health", "www"}
	for _, s := range reserved {
		if !siteReservedSlugs[s] {
			t.Errorf("expected %q to be reserved", s)
		}
	}
	if siteReservedSlugs["my-cool-site"] {
		t.Error("expected my-cool-site to not be reserved")
	}
}
