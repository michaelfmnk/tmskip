#!/bin/sh
# Render the Homebrew formula for a release.
# Usage: render.sh <version without v> <zip path>   (formula on stdout)
set -eu

version=$1
zip=$2
case $version in
  v*) echo "render.sh: pass the version without the leading v" >&2; exit 1 ;;
esac
sha=$(shasum -a 256 "$zip" | cut -d' ' -f1)
dir=$(cd "$(dirname "$0")" && pwd)
sed -e "s/@VERSION@/$version/g" -e "s/@SHA256@/$sha/g" "$dir/tmskip.rb.tmpl"
