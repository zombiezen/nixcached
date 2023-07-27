# shellcheck shell=sh
exec >&2
. binaries/build.sh
build . "$@"
