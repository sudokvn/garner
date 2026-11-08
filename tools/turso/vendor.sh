#!/usr/bin/env bash
# Collects the source of the Turso library that garnerd embeds, as
# tools/turso/turso.env pins it, into a new folder:
#
#   turso-COMMIT.tar.gz      GitHub's archive of the Turso commit, as is
#   vendor/                  every crate its Cargo.lock pins (cargo vendor)
#   turso-go-platform-libs/  the platform module's build scripts
#
# Checks the archive, Cargo.lock and the embedded library against the pins.
# Cargo runs in the pinned Rust image with Podman, or with Docker where
# Podman is missing, and needs the network.
#
# Usage: bash tools/turso/vendor.sh OUTDIR
set -euo pipefail

[ $# -eq 1 ] || {
	echo "usage: bash tools/turso/vendor.sh OUTDIR" >&2
	exit 2
}
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
# shellcheck source=tools/turso/turso.env
source "$here/turso.env"

fail() {
	printf '✗ %s\n' "$1" >&2
	exit 1
}
sha256() {
	if command -v sha256sum >/dev/null; then
		sha256sum "$1" | awk '{ print $1 }'
	else
		shasum -a 256 "$1" | awk '{ print $1 }'
	fi
}

engine=$(command -v podman || command -v docker) || fail "cargo runs in a container; install Podman or Docker"

# A new folder only, so nothing of the caller's is overwritten or removed.
[ ! -e "$1" ] || fail "$1 exists already; give a folder that does not exist"
mkdir -p "$1"
# The physical path: Podman on macOS shares /private/tmp, not /tmp.
out=$(cd "$1" && pwd -P)

# The platform module as go.mod pins it, from the module cache, which the
# go tool checks against go.sum.
want=$(cd "$root" && go list -m -f '{{.Version}}' github.com/tursodatabase/turso-go-platform-libs)
[ "$want" = "$PLATFORM_LIBS_VERSION" ] ||
	fail "go.mod pins turso-go-platform-libs $want, tools/turso/turso.env $PLATFORM_LIBS_VERSION; update the pins"
(cd "$root" && go mod download github.com/tursodatabase/turso-go-platform-libs)
module=$(cd "$root" && go list -m -f '{{.Dir}}' github.com/tursodatabase/turso-go-platform-libs)
[ "$(sha256 "$module/$LIBRARY")" = "$LIBRARY_SHA256" ] ||
	fail "$LIBRARY in the platform module does not match LIBRARY_SHA256"
for f in LICENSE README.md build_libs.sh hash_libs.sh .github/workflows/build.yml; do
	mkdir -p "$out/turso-go-platform-libs/$(dirname "$f")"
	cp "$module/$f" "$out/turso-go-platform-libs/$f"
	chmod u+w "$out/turso-go-platform-libs/$f"
done

archive="turso-$TURSO_COMMIT.tar.gz"
curl -fsSL -o "$out/$archive" "$TURSO_ARCHIVE_URL" || fail "could not download $TURSO_ARCHIVE_URL"
[ "$(sha256 "$out/$archive")" = "$TURSO_ARCHIVE_SHA256" ] ||
	fail "$TURSO_ARCHIVE_URL does not match TURSO_ARCHIVE_SHA256"

# cargo vendor needs the unpacked workspace; the archive keeps it packed.
# The work folder is this run's own, inside the new folder, where the
# container sees it.
work=$(mktemp -d "$out/.work.XXXXXX")
trap 'rm -rf "$work"' EXIT
tar -xzf "$out/$archive" -C "$work"
[ "$(sha256 "$work/turso-$TURSO_COMMIT/Cargo.lock")" = "$TURSO_CARGO_LOCK_SHA256" ] ||
	fail "Turso's Cargo.lock does not match TURSO_CARGO_LOCK_SHA256"

# RUSTUP_TOOLCHAIN overrides the rust-toolchain.toml channel 1.88, which
# rustup would otherwise try to download as a toolchain of its own.
# shellcheck disable=SC2016 # the container's shell expands them
"$engine" run --rm --security-opt label=disable \
	-v "$out:/out" -w "/out/${work##*/}/turso-$TURSO_COMMIT" \
	-e CARGO_HOME="/out/${work##*/}/cargo-home" -e RUSTUP_TOOLCHAIN="$RUST_VERSION" \
	-e OWNER="$(id -u):$(id -g)" \
	"$RUST_IMAGE" sh -euc '
		cargo vendor --locked --versioned-dirs /out/vendor >/dev/null
		chown -R "$OWNER" /out/vendor
	'
printf '✓ Turso %s source and %s vendored crates in %s\n' "$TURSO_VERSION" "$(find "$out/vendor" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')" "$out"
