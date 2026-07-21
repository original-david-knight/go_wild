package main

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var staticFiles embed.FS

var staticSubFS, _ = fs.Sub(staticFiles, "static")
