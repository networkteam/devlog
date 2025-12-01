package static

import "embed"

//go:embed *.css *.ico *.png *.webmanifest *.svg
var Assets embed.FS
