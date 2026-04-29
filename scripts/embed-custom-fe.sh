#!/bin/bash
#
# Embed the latest n9e-custom-fe build into this backend repo.
#
# Why a separate script (instead of just letting fe.sh do it):
#   fe.sh is the upstream contract — it pulls a release tarball from
#   github.com/n9e/fe. We don't want that path: we have our own fork
#   with new pages. This script builds OUR fork's dist into pub/, then
#   delegates to the same statik step fe.sh would run, so the resulting
#   front/statik/statik.go shape is identical to upstream.
#
# Prereqs:
#   - n9e-custom-fe checked out as a sibling directory (../n9e-custom-fe)
#   - npm + statik installed (see fe.sh's installation note)
#
# Output:
#   - ./pub/                 — fresh dist from npm run build
#   - ./front/statik/statik.go — re-generated; commit this if you want
#                                 the docker image to include the change

set -euo pipefail

# Locate the fe repo. Override with N9E_FE_DIR env var if you keep it
# elsewhere (e.g. on CI where the layout differs).
FE_DIR="${N9E_FE_DIR:-../n9e-custom-fe}"

if [ ! -d "$FE_DIR" ]; then
  echo "fe repo not found at $FE_DIR" >&2
  echo "Set N9E_FE_DIR or clone n9e-custom-fe alongside this repo." >&2
  exit 1
fi

echo "==> building fe at $FE_DIR"
(
  cd "$FE_DIR"
  if [ ! -d node_modules ]; then
    echo "==> npm install (first time)"
    npm install --no-audit --no-fund
  fi
  npm run build
)

# Replace any previous pub/ with the fresh build. Safer than rsync because
# we want stale assets gone, not merged.
echo "==> syncing pub/"
rm -rf pub
cp -R "$FE_DIR/pub" ./pub

# Locate statik binary the same way fe.sh does.
GOPATH=$(go env GOPATH)
GOPATH=${GOPATH:-/home/runner/go}
STATIK="$GOPATH/bin/statik"
if [ ! -x "$STATIK" ]; then
  echo "==> installing statik"
  go install github.com/rakyll/statik@latest
fi

echo "==> regenerating front/statik/statik.go"
"$STATIK" -src=./pub -dest=./front -f

echo "==> done. front/statik/statik.go updated ($(wc -c <front/statik/statik.go | tr -d ' ') bytes)"
echo "Commit the changed statik.go to ship the new fe inside the docker image."
