module github.com/networkteam/devlog/example

go 1.23.8

replace github.com/networkteam/devlog => ../

require (
	github.com/networkteam/devlog v0.0.0-00010101000000-000000000000
	github.com/samber/slog-multi v1.4.0
)

require (
	github.com/samber/lo v1.49.1 // indirect
	golang.org/x/text v0.21.0 // indirect
)
