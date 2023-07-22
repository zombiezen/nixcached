# shellcheck shell=sh
exec >&2
redo-always
redo-ifchange client/dist/style.css
go build -ldflags='-s -w' -o "$3"
