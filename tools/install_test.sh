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
cp "$FIXTURES/${url##*/}" "$out"
SH
cat >"$work/bin/uname" <<'SH'
#!/usr/bin/env bash
case $1 in
-s) echo Linux ;;
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
printf '✓ Installer passed\n'
