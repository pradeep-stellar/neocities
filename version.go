package neocities

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// Version is the client version, read from the VERSION file.
var Version = strings.TrimSpace(versionFile)
