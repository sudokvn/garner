#!/usr/bin/env bash
# Exercise the installer against local release fixtures, never GitHub or
# an installed garner. Archives must deliver notices before replacing a
# binary; failed verification must leave the old installation intact.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail() { printf '✗ installer: %s\n' "$1" >&2; exit 1; }
mkdir -p "$work/bin" "$work/fixtures" "$work/package" "$work/home"

cat >"$work/bin/curl" <<'SH'
#!/usr/bin/env bash
set -eu
out= url=
while [ $# -gt 0 ]; do
	case $1 in
	-o) out=$2; shift 2 ;;
	-fsSL) shift ;;
	https://github.com/sudokvn/garner/releases/download/*) url=$1; shift ;;
	*) exit 2 ;;
	esac
done
[ -n "$out" ] && [ -n "$url" ] || exit 2
printf '%s\n' "$url" >>"$FIXTURES/requests"
cp "$FIXTURES/${url##*/}" "$out"
SH
cat >"$work/bin/uname" <<'SH'
#!/usr/bin/env bash
case $1 in
-s) echo "${UNAME_S:-Linux}" ;;
-m) echo x86_64 ;;
*) exit 2 ;;
esac
SH
chmod 755 "$work/bin/curl" "$work/bin/uname"
export PATH="$work/bin:$PATH" FIXTURES="$work/fixtures" HOME="$work/home"
export GARNER_VERSION=v9.9.9 GARNER_INSTALL_DIR="$work/install"
# This is a fixture, not garner: it never reads a connection or config.
printf '#!/usr/bin/env bash\necho "garner %s"\n' "$GARNER_VERSION" >"$work/package/garner"
printf 'fixture licence\n' >"$work/package/LICENSE"
printf 'fixture copyright\n' >"$work/package/NOTICE"
printf 'fixture third-party notices\n' >"$work/package/THIRD-PARTY-NOTICES.txt"
file="garner-$GARNER_VERSION-linux-amd64"
checksum() {
	(cd "$FIXTURES" && if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi) >"$FIXTURES/SHA256SUMS"
}
archive() {
	tar -czf "$FIXTURES/$file.tar.gz" -C "$work/package" "$@"
	checksum "$file.tar.gz"
}
install_fixture() { bash "$root/install.sh" >"$work/output" 2>&1; }

archive garner LICENSE NOTICE THIRD-PARTY-NOTICES.txt
install_fixture || fail "packaged install failed"
[ "$("$GARNER_INSTALL_DIR/garner" --version)" = "garner $GARNER_VERSION" ] || fail "wrong installed binary"
notices="$GARNER_INSTALL_DIR/garner-licenses"
for notice in LICENSE NOTICE THIRD-PARTY-NOTICES.txt; do
	cmp "$work/package/$notice" "$notices/$notice" || fail "$notice was not installed"
done
# A repeated install downloads no release files.
mv "$FIXTURES/SHA256SUMS" "$FIXTURES/saved-sums"
install_fixture || fail "an installed version downloaded release files again"
mv "$FIXTURES/saved-sums" "$FIXTURES/SHA256SUMS"

# None of these failures may replace an older binary.
printf '#!/usr/bin/env bash\necho "garner v1.0.0"\n' >"$GARNER_INSTALL_DIR/garner"
cp "$GARNER_INSTALL_DIR/garner" "$work/old"
printf 'corruption\n' >>"$FIXTURES/$file.tar.gz"
if install_fixture; then fail "accepted a corrupt archive"; fi
cmp "$work/old" "$GARNER_INSTALL_DIR/garner" || fail "corrupt archive replaced the binary"
archive garner LICENSE THIRD-PARTY-NOTICES.txt
if install_fixture; then fail "accepted an archive without NOTICE"; fi
cmp "$work/old" "$GARNER_INSTALL_DIR/garner" || fail "incomplete archive replaced the binary"
# An advertised archive that fails to download must not fall back to a
# bare binary, even if that binary is also listed in the checksums.
cp "$work/package/garner" "$FIXTURES/$file"
checksum "$file"
legacy_sum=$(<"$FIXTURES/SHA256SUMS")
archive garner LICENSE NOTICE THIRD-PARTY-NOTICES.txt
printf '%s\n' "$legacy_sum" >>"$FIXTURES/SHA256SUMS"
rm "$FIXTURES/$file.tar.gz"
if install_fixture; then fail "fell back after a failed archive download"; fi
cmp "$work/old" "$GARNER_INSTALL_DIR/garner" || fail "failed download replaced the binary"

# Existing MIT releases still install from their checked bare binaries.
checksum "$file"
install_fixture || fail "legacy install failed"
[ "$("$GARNER_INSTALL_DIR/garner" --version)" = "garner $GARNER_VERSION" ] || fail "wrong legacy binary"

# The dev channel installs the build the rolling release dev holds, named
# by its checksums, and nothing when that build is installed already.
rm -f "$FIXTURES"/*
unset GARNER_VERSION
dev=v9.9.9-rc.1-dev.3
file="garner-$dev-linux-amd64"
printf '#!/usr/bin/env bash\necho "garner %s (0123456)"\n' "$dev" >"$work/package/garner"
tar -czf "$FIXTURES/$file.tar.gz" -C "$work/package" garner LICENSE NOTICE THIRD-PARTY-NOTICES.txt
cp "$FIXTURES/$file.tar.gz" "$FIXTURES/garner-$dev-linux-arm64.tar.gz"
printf 'source\n' >"$FIXTURES/garner-$dev-source.tar.gz"
(cd "$FIXTURES" && if command -v sha256sum >/dev/null; then sha256sum garner-*; else shasum -a 256 garner-*; fi) >"$work/sums"
mv "$work/sums" "$FIXTURES/SHA256SUMS"
install_dev() { bash "$root/install.sh" --channel dev >"$work/output" 2>&1; }
install_dev || fail "dev install failed: $(cat "$work/output")"
[ "$("$GARNER_INSTALL_DIR/garner" --version)" = "garner $dev (0123456)" ] || fail "wrong dev binary"
! grep -v -q '/releases/download/dev/' "$FIXTURES/requests" || fail "dev install downloaded from another release"
rm "$FIXTURES/$file.tar.gz"
install_dev || fail "an installed dev build was downloaded again"
grep -q "is installed already" "$work/output" || fail "an installed dev build was not recognised"
if UNAME_S=Darwin install_dev; then fail "installed a dev build on macOS"; fi
grep -q 'garner@dev' "$work/output" || fail "macOS dev install does not name the cask garner@dev"
printf '✓ Installer passed\n'
