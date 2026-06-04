#!/bin/sh
# Render the GitHub social-preview image (1280x640) from social-card.html using
# headless Chrome. Output: docs/social-preview.png
set -e

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
[ -x "$chrome" ] || chrome=$(command -v google-chrome || command -v chromium || echo chrome)

"$chrome" --headless=new --disable-gpu --hide-scrollbars \
  --force-device-scale-factor=1 --window-size=1280,640 \
  --screenshot="$repo_root/docs/social-preview.png" \
  "file://$repo_root/docs/demo/social-card.html"

echo "Wrote docs/social-preview.png"
