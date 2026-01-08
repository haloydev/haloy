package embed

import "embed"

//go:embed data/*
var DataFS embed.FS

//go:embed templates/*
var TemplatesFS embed.FS

//go:embed dockerfiles/*
var DockerfilesFS embed.FS
