#!/usr/bin/env bash
# Packs a natslens release binary as OUT_DIR/natslens_<version>_<os>_<arch>.tar.gz,
# with LICENSE, NOTICE and README.md, all under one directory of that name.
# The version loses a leading "v": v0.2.0 gives natslens_0.2.0_linux_amd64.
#
# Usage: scripts/release-archive.sh VERSION OS ARCH BINARY OUT_DIR
set -euo pipefail

if [ $# -ne 5 ]; then
	echo "usage: $0 VERSION OS ARCH BINARY OUT_DIR" >&2
	exit 2
fi
version=${1#v} os=$2 arch=$3 binary=$4 out=$5
root=$(cd "$(dirname "$0")/.." && pwd)
name="natslens_${version}_${os}_${arch}"

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir "$stage/$name"
cp "$binary" "$stage/$name/natslens"
chmod 0755 "$stage/$name/natslens"
cp "$root/LICENSE" "$root/NOTICE" "$root/README.md" "$stage/$name/"

mkdir -p "$out"
# COPYFILE_DISABLE keeps macOS tar from adding AppleDouble ._ files.
COPYFILE_DISABLE=1 tar -C "$stage" -czf "$out/$name.tar.gz" "$name"
echo "$out/$name.tar.gz"
