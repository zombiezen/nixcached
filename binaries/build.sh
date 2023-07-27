# shellcheck shell=sh
build() {
  _build_dir="$1"
  shift

  redo-always
  redo-ifchange "$_build_dir/client/dist/style.css" "$_build_dir/version.txt"
  go build \
    -ldflags="-s -w -X 'main.version=$(cat "$_build_dir/version.txt")'" \
    -o "$3" \
    zombiezen.com/go/nixcached
  unset _build_dir
}
