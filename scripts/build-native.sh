#!/bin/sh
# Creates a native release archive for the host OS and architecture. GitHub
# Actions invokes the same build inputs for every supported target.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version=${TGSTRM_VERSION:-dev}
os=${GOOS:-$(go env GOOS)}
arch=${GOARCH:-$(go env GOARCH)}
name="tgstrm_${version}_${os}_${arch}"
out="$root/bin/$name"
extension=""
if [ "$os" = "windows" ]; then
  extension=".exe"
fi

(cd "$root/web" && npm ci && npm run build)
"$root/scripts/prepare-web-assets.sh"
mkdir -p "$out"
CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$out/tgstrm$extension" ./cmd/tgstrm
cp "$root/deploy/native.env.example" "$out/.env.example"
cp "$root/README.md" "$out/README.md"

echo "Native package ready at $out"
