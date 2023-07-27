# shellcheck shell=sh
redo-ifchange ../version.txt

version="$(cat ../version.txt)"
if [ -n "$version" ]; then
  version="-$version"
fi

redo-ifchange \
  "nixcached${version}-darwin_amd64" \
  "nixcached${version}-darwin_arm64" \
  "nixcached${version}-linux_amd64" \
  "nixcached${version}-linux_arm64"
