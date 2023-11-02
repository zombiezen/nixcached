# shellcheck shell=sh
exec >&2
case "$1" in
  dist/style.css)
    find ./templates \
      -name '*.html' \
      -print0 | \
      xargs -0 redo-ifchange style.css tailwind.config.js
    tailwindcss --input style.css --output "$3" --minify
    ;;
  *)
    echo "no rule for $1"
    exit 24
    ;;
esac
