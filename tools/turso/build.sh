#!/bin/sh
# Builds Turso's native library from the Turso source and vendored crates
# in the folder this script lies in, without the network, as the platform
# module's build_libs.sh builds it upstream: the same package, profile,
# target and RUSTFLAGS, pinned in turso.env. It prints the library's
# SHA-256 next to the one garnerd embeds; see docs/source.md for what a
# difference means.
#
# Run it in the Rust image turso.env names, with musl's development files
# added as upstream's build adds them, and without the network:
#
#   . ./turso.env
#   printf 'FROM %s\nRUN apk add --no-cache musl-dev\n' "$RUST_IMAGE" | podman build -t turso-build -
#   podman run --rm --network=none -v "$PWD:/turso" -w /turso turso-build sh build.sh
#
# OUT (default: ./out), a folder that must not exist yet, receives the
# library and the list of crates compiled into it. TURSO_TARGET builds for
# another Rust target, such as aarch64-unknown-linux-musl natively on an
# ARM machine; garner ships only the default.
set -eu

here=$(cd "$(dirname "$0")" && pwd -P)
# shellcheck source=tools/turso/turso.env
. "$here/turso.env"
target=${TURSO_TARGET:-$RUST_TARGET}
out=${OUT:-$here/out}

[ ! -e "$out" ] || {
	echo "✗ $out exists already; set OUT to a folder that does not exist" >&2
	exit 1
}
# The build's own folder, removed afterwards; the source stays packed.
work=$(mktemp -d "${TMPDIR:-/tmp}/turso-build.XXXXXX")
trap 'rm -rf "$work"' EXIT
tar -xzf "$here/turso-$TURSO_COMMIT.tar.gz" -C "$work"
cd "$work/turso-$TURSO_COMMIT"

# Cargo takes every crate from vendor/ instead of crates.io; --frozen makes
# it fail rather than change Cargo.lock or reach the network.
mkdir -p .cargo
cat >.cargo/config.toml <<EOF
[source.crates-io]
replace-with = "vendored-sources"

[source.vendored-sources]
directory = "$here/vendor"
EOF

export RUSTFLAGS CARGO_HOME="$work/cargo-home"
# Overrides rust-toolchain.toml's channel 1.88, which rustup would
# otherwise try to download.
export RUSTUP_TOOLCHAIN="$RUST_VERSION"
rustc --version
cargo build --frozen --profile "$CARGO_PROFILE" --target "$target" --package "$CARGO_PACKAGE"

lib="lib$CARGO_PACKAGE.so"
mkdir -p "$out"
cp "target/$target/$CARGO_PROFILE/$lib" "$out/$lib"
# The crates compiled for the target, for comparing with the notices.
find "target/$target/$CARGO_PROFILE/deps" -name '*.rlib' -exec basename {} \; |
	sed 's/^lib//; s/-[0-9a-f]*\.rlib$//' | sort -u >"$out/crates.txt"
echo "built:    $(sha256sum "$out/$lib" | cut -d' ' -f1)  $out/$lib"
[ "$target" != "$RUST_TARGET" ] || echo "embedded: $LIBRARY_SHA256  $LIBRARY"
