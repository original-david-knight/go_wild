package main

import (
	"fmt"
	"os"
	"strings"
)

func ingressPublicBaseURLWithError() (string, error) {
	raw := strings.TrimSpace(os.Getenv("INGRESS_PUBLIC_URL"))
	if raw == "" {
		return "", nil
	}
	parsed, err := parseHTTPSURL(raw)
	if err != nil {
		return "", fmt.Errorf("invalid INGRESS_PUBLIC_URL: %w", err)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid INGRESS_PUBLIC_URL: query strings and fragments are not allowed")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}
