package static

import "embed"

//go:embed *.css *.ico
var Assets embed.FS
