package web

import "embed"

//go:embed templates/*.html templates/partials/*.html static/*
var FS embed.FS
