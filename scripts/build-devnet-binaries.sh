#!/usr/bin/env bash
# Build the three stocd binaries required to exercise the devnet upgrade
# path on Ubuntu x86_64 validators (no-evm -> EVM v1.0.0-rc2 -> EVM v0.6.0).
#
# Each binary is compiled inside a linux/amd64 Docker container so the
# resulting ELF file runs on the Ubuntu 24.04 devnet VPS regardless of
# the macOS host architecture (M-series or Intel).
#
# Usage:
#   cd stoc-current-dev
#   ./scripts/build-devnet-binaries.sh
#
# Output:
#   ../bin-devnet/stoc_main_d         (archive/no-evm,            vanilla Dockerfile)
#   ../bin-devnet/stoc_evm_d          (archive/feat-evm-v1.0.0-rc2, vanilla Dockerfile)
#   ../bin-devnet/stoc_fixed_evm_d    (fix/evm-from-v1.0.0-to-v0.6.0, patched Dockerfile)
#
# The patched Dockerfile rewrites two cosmos/evm v0.6.0 upstream bugs that
# only affect chains with 6-decimal base denoms like udstoc / ustoc: the
# eth_gasPrice 10^12x inflation and the eth_getTransactionReceipt null
# result for simple ETH transfers.

set -eo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

if [[ -n "$(git status --porcelain)" ]]; then
  echo "ERROR: working tree is not clean. Commit or stash before running." >&2
  git status --short >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "ERROR: docker is required but was not found on PATH." >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "ERROR: Docker daemon is not running." >&2
  exit 1
fi

set -u

OUT_DIR="$(cd "$REPO_ROOT/.." && pwd)/bin-devnet"
mkdir -p "$OUT_DIR"

ORIG_BRANCH="$(git rev-parse --abbrev-ref HEAD)"

# Preserve the current-branch Dockerfiles so they survive the checkouts
# below; archive/no-evm has no Dockerfile and the rc2 archive only ships
# Dockerfile.openapi, so we always reuse the ones from the HEAD branch.
TMP_DIR="$(mktemp -d)"
cp Dockerfile.stocd Dockerfile.stocd.patched "$TMP_DIR/"

cleanup() {
  local rc=$?
  echo ""
  echo "Restoring branch: $ORIG_BRANCH"
  git checkout --quiet "$ORIG_BRANCH"
  rm -rf "$TMP_DIR"
  exit $rc
}
trap cleanup EXIT

docker_build_and_export() {
  local dockerfile="$1"
  local branch="$2"
  local out_name="$3"
  local image_tag="stoc-build-$(echo "$out_name" | tr '_' '-'):latest"

  echo ""
  echo "===================================================================="
  echo "Building $out_name"
  echo "  branch     = $branch"
  echo "  dockerfile = $dockerfile"
  echo "  image tag  = $image_tag"
  echo "===================================================================="

  git checkout --quiet "$branch"
  git --no-pager log --oneline -1

  docker build \
    --platform linux/amd64 \
    --file "$dockerfile" \
    --tag "$image_tag" \
    .

  docker run --rm \
    --platform linux/amd64 \
    -v "$OUT_DIR:/output" \
    -e BINARY_NAME="$out_name" \
    "$image_tag"

  echo "--- file info ---"
  file "$OUT_DIR/$out_name"
  echo "--- sha256 ---"
  shasum -a 256 "$OUT_DIR/$out_name"
}

docker_build_and_export "$TMP_DIR/Dockerfile.stocd"         "archive/no-evm"                "stoc_main_d"
docker_build_and_export "$TMP_DIR/Dockerfile.stocd"         "archive/feat-evm-v1.0.0-rc2"   "stoc_evm_d"
docker_build_and_export "$TMP_DIR/Dockerfile.stocd.patched" "fix/evm-from-v1.0.0-to-v0.6.0" "stoc_fixed_evm_d"

echo ""
echo "===================================================================="
echo "All binaries built in $OUT_DIR"
echo "===================================================================="
ls -la "$OUT_DIR"
echo ""
echo "--- linux/amd64 verification ---"
file "$OUT_DIR"/stoc_main_d "$OUT_DIR"/stoc_evm_d "$OUT_DIR"/stoc_fixed_evm_d
echo ""
echo "--- sha256 ---"
shasum -a 256 "$OUT_DIR"/stoc_main_d "$OUT_DIR"/stoc_evm_d "$OUT_DIR"/stoc_fixed_evm_d
