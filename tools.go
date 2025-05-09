//go:build tools
// +build tools

package devlog

// Import modules for external tools for correct version pinning und usage with "go run ..."
import (
	_ "github.com/a-h/templ/cmd/templ"
	_ "github.com/networkteam/refresh"
)
