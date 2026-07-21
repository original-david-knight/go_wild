package routes

import (
	"errors"
	"strings"
)

var ErrCompanyIDRequired = errors.New("company ID required")

type CompanyRoute struct {
	CompanyID string
	Action    string
	Parts     []string
}

func ParseCompany(path string) (CompanyRoute, error) {
	trimmed := strings.TrimPrefix(path, "/api/companies/")
	parts := strings.SplitN(trimmed, "/", 4)
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return CompanyRoute{}, ErrCompanyIDRequired
	}

	route := CompanyRoute{
		CompanyID: strings.TrimSpace(parts[0]),
		Parts:     append([]string(nil), parts...),
	}
	if len(parts) > 1 {
		route.Action = strings.TrimSpace(parts[1])
	}
	return route, nil
}
