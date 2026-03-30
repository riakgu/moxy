package web

import "embed"

//go:embed dashboard/dist/*
var StaticFS embed.FS
