# shellcheck shell=sh
exec >&2
redo-always
redo-ifchange client/dist/style.css
go test -race ./...
