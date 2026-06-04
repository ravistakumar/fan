#!/bin/sh
# Render docs/demo.gif reproducibly: build fan, seed a scratch git repo with a
# few source files, put a fake "claude" on PATH so the demo never calls a real
# agent, then run the VHS tape inside that repo.
#
# Requires: go, git, vhs (https://github.com/charmbracelet/vhs).
set -e

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

bin=$(mktemp -d)
demo=$(mktemp -d)
trap 'rm -rf "$bin" "$demo"' EXIT

# Build fan and the fake agent into a dir we prepend to PATH.
go build -o "$bin/fan" ./cmd/fan
cp docs/demo/claude "$bin/claude"
chmod +x "$bin/claude"

# Seed a small git repo for the demo to operate on.
mkdir -p "$demo/src"
for f in Button Modal Tabs Card; do
	printf 'export const %s = () => null;\n' "$f" >"$demo/src/$f.tsx"
done
git -C "$demo" init -q -b main
git -C "$demo" -c user.email=demo@local -c user.name=demo add -A
git -C "$demo" -c user.email=demo@local -c user.name=demo commit -q -m "seed components"

# Record inside the demo repo; the tape writes demo.gif to the cwd.
( cd "$demo" && PATH="$bin:$PATH" vhs "$repo_root/docs/demo/demo.tape" )
mv "$demo/demo.gif" "$repo_root/docs/demo.gif"
echo "Wrote docs/demo.gif"
