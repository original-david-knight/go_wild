package objectives

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dashboard/*
var dashboardFS embed.FS

// DashboardHandler returns an http.Handler that serves the embedded dashboard UI.
func DashboardHandler() http.Handler {
	sub, _ := fs.Sub(dashboardFS, "dashboard")
	return http.FileServer(http.FS(sub))
}
