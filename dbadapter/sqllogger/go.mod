module github.com/networkteam/devlog/dbadapter/sqllogger

go 1.23.8

replace github.com/networkteam/devlog => ../../

require (
	github.com/networkteam/devlog v0.0.0-00010101000000-000000000000
	github.com/networkteam/go-sqllogger v0.4.0
)

require (
	github.com/gofrs/uuid v4.4.0+incompatible // indirect
	github.com/samber/lo v1.50.0 // indirect
	golang.org/x/text v0.25.0 // indirect
)
