#!/usr/bin/env sh
# Regenerates internal/gen from a pinned xai-proto commit using the Go plugins
# from xai-proto's buf.gen.yaml (PR xai-org/xai-proto, re-landing #40).
# Requires: buf (https://buf.build/docs/cli/installation/), git.
set -eu

XAI_PROTO_REPO="${XAI_PROTO_REPO:-https://github.com/xai-org/xai-proto.git}"
XAI_PROTO_COMMIT="${XAI_PROTO_COMMIT:-6499fba}"
MODULE="github.com/deanrie/xai-go/internal/gen"

root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

git clone --quiet --filter=blob:none "$XAI_PROTO_REPO" "$work/xai-proto"
git -C "$work/xai-proto" checkout --quiet "$XAI_PROTO_COMMIT"

cat > "$work/xai-proto/buf.gen.go.yaml" <<YAML
version: v2
clean: true
managed:
  enabled: true
  override:
    - file_option: go_package_prefix
      value: ${MODULE}
  disable:
    - module: buf.build/googleapis/googleapis
      file_option: go_package_prefix
    - module: buf.build/grpc-ecosystem/grpc-gateway
      file_option: go_package_prefix
plugins:
  - remote: buf.build/protocolbuffers/go:v1.36.5
    out: gen/go
    opt:
      - paths=source_relative
  - remote: buf.build/grpc/go:v1.5.1
    out: gen/go
    opt:
      - paths=source_relative
YAML

(cd "$work/xai-proto" && buf generate --template buf.gen.go.yaml)

rm -rf "$root/internal/gen"
mkdir -p "$root/internal/gen"
cp -R "$work/xai-proto/gen/go/." "$root/internal/gen/"
printf '%s\n' "$XAI_PROTO_COMMIT" > "$root/internal/gen/XAI_PROTO_COMMIT"
echo "regenerated internal/gen from xai-proto ${XAI_PROTO_COMMIT}"
