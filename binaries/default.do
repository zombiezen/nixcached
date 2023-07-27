# shellcheck shell=sh

exec >&2
case "$1" in
  nixcached*-darwin_amd64)
    export GOOS=darwin
    export GOARCH=amd64
    ;;
  nixcached*-darwin_arm64)
    export GOOS=darwin
    export GOARCH=arm64
    ;;
  nixcached*-linux_amd64)
    export GOOS=linux
    export GOARCH=amd64
    ;;
  nixcached*-linux_arm64)
    export GOOS=linux
    export GOARCH=arm64
    ;;
  *)
    echo "no rule for $1"
    exit 24
    ;;
esac

export CGO_ENABLED=0
. ./build.sh
build .. "$@"
