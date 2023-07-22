# shellcheck shell=sh
exec >&2
case "$1" in
  dist/style.css)
    redo-ifchange style.scss
    sass --style=compressed --no-source-map style.scss "$3"
    ;;
  *)
    echo "no rule for $1"
    exit 24
    ;;
esac
