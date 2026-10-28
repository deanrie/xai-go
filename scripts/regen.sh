#!/usr/bin/env sh
# Regenerates internal/gen from a pinned xai-proto commit using the Go plugins
# from xai-proto's buf.gen.yaml (PR xai-org/xai-proto, re-landing #40).
# Requires: buf (https://buf.build/docs/cli/installation/), git.
set -eu

XAI_PROTO_REPO="${XAI_PROTO_REPO:-https://github.com/xai-org/xai-proto.git}"
XAI_PROTO_COMMIT="${XAI_PROTO_COMMIT:-6499fba}"
MODULE="github.com/deanrie/xai-go/internal/gen"

# collections.proto, shared.proto and types.proto are not published in
# xai-proto (xai-org/xai-proto#83). Until they are, they come from a branch
# that renders them back from the descriptors compiled into xai-sdk-python;
# the structure is checked identical to the gencode there.
RECOVERED_REPO="${RECOVERED_REPO:-https://github.com/deanrie/xai-proto.git}"
RECOVERED_COMMIT="${RECOVERED_COMMIT:-d76b522}"
RECOVERED_FILES="collections.proto shared.proto types.proto"

root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

git clone --quiet --filter=blob:none "$XAI_PROTO_REPO" "$work/xai-proto"
git -C "$work/xai-proto" checkout --quiet "$XAI_PROTO_COMMIT"

git clone --quiet --filter=blob:none "$RECOVERED_REPO" "$work/recovered"
git -C "$work/recovered" checkout --quiet "$RECOVERED_COMMIT"
for f in $RECOVERED_FILES; do
  cp "$work/recovered/proto/xai/api/v1/$f" "$work/xai-proto/proto/xai/api/v1/$f"
done

cat > "$work/xai-proto/buf.gen.go.yaml" <<YAML
version: v2
clean: true
managed:
  enabled: true
  override:
    - file_option: go_package_prefix
      value: ${MODULE}
    # The recovered files carry their own proto packages (collections, shared,
    # rag) and share a directory with package xai_api, so each needs its own
    # Go package or the generated symbols collide (shared.Ordering vs
    # xai_api.Ordering).
    - file_option: go_package
      path: xai/api/v1/collections.proto
      value: ${MODULE}/xai/api/v1/collections
    - file_option: go_package
      path: xai/api/v1/shared.proto
      value: ${MODULE}/xai/api/v1/shared
    - file_option: go_package
      path: xai/api/v1/types.proto
      value: ${MODULE}/xai/api/v1/rag
  disable:
    - module: buf.build/googleapis/googleapis
      file_option: go_package_prefix
    - module: buf.build/grpc-ecosystem/grpc-gateway
      file_option: go_package_prefix
plugins:
  - remote: buf.build/protocolbuffers/go:v1.36.5
    out: gen/go
  - remote: buf.build/grpc/go:v1.5.1
    out: gen/go
YAML

(cd "$work/xai-proto" && buf generate --template buf.gen.go.yaml)

rm -rf "$root/internal/gen"
mkdir -p "$root/internal/gen"
# paths=import lays files out under the full import path; keep the part
# below the module.
cp -R "$work/xai-proto/gen/go/${MODULE}/." "$root/internal/gen/"
printf '%s\n' "$XAI_PROTO_COMMIT" > "$root/internal/gen/XAI_PROTO_COMMIT"
printf '%s\n' "$RECOVERED_COMMIT" > "$root/internal/gen/RECOVERED_PROTO_COMMIT"
echo "regenerated internal/gen from xai-proto ${XAI_PROTO_COMMIT} + recovered ${RECOVERED_COMMIT}"
