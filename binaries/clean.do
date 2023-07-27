# shellcheck shell=sh
exec >&2
redo-always
find . -executable -name 'nixcached*' -exec rm -f '{}' +
