#!/usr/bin/env bash
# Builds a release's corresponding source, garner-VERSION-source.tar.gz,
# into a new folder, next to the unpacked tree it is made of:
#
#   garner/   the committed HEAD, with the Go modules in vendor/
#   ffmpeg/   the FFmpeg release the Dockerfile builds FFprobe from
#   turso/    the source of the Turso library garnerd embeds, its vendored
#             crates and the script that builds it offline
#
# The release workflow builds the server image from garner/, so the image
# is built from what the archive holds. docs/source.md describes how to
# build each part from the archive. It needs the network, Go and Podman,
# or Docker where Podman is missing.
#
# Usage: bash tools/source.sh VERSION OUTDIR
set -euo pipefail

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

[ $# -eq 2 ] || {
	echo "usage: bash tools/source.sh VERSION OUTDIR" >&2
	exit 2
}
version=$1
[[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || fail "$version is not a version such as v1.2.3"
# A new folder only, so nothing of the caller's is overwritten or removed.
[ ! -e "$2" ] || fail "$2 exists already; give a folder that does not exist"
root=$(git rev-parse --show-toplevel)
cd "$root"

# Archive the committed source only; local edits and untracked files stay out.
commit=$(git rev-parse HEAD)

name="garner-$version-source"
mkdir -p "$2"
out=$(cd "$2" && pwd -P)
dir="$out/$name"
mkdir -p "$dir/garner" "$dir/ffmpeg"

git archive --format=tar "$commit" | tar -x -C "$dir/garner"

# The Go modules, so garner builds without the network. The go tool
# checks them against go.sum. vendor/ holds every package the module
# needs on any platform, so it covers both programs on every release
# platform.
printf 'Vendoring the Go modules...\n'
(cd "$dir/garner" && GOFLAGS=-mod=mod GOTOOLCHAIN=local go mod vendor)

# FFmpeg as the Dockerfile downloads it, checked against its checksum.
read -r ffmpeg_sum ffmpeg_url < <(awk '
	/^ADD --checksum=sha256:/ { sub(/^ADD --checksum=sha256:/, ""); sum = $1; getline; print sum, $1; exit }
' "$dir/garner/Dockerfile")
[[ "$ffmpeg_url" == https://ffmpeg.org/releases/* ]] || fail "the Dockerfile names no FFmpeg release"
printf 'Downloading %s...\n' "$ffmpeg_url"
curl -fsSL -o "$dir/ffmpeg/${ffmpeg_url##*/}" "$ffmpeg_url" || fail "could not download $ffmpeg_url"
[ "$(sha256 "$dir/ffmpeg/${ffmpeg_url##*/}")" = "$ffmpeg_sum" ] || fail "$ffmpeg_url does not match the Dockerfile's checksum"

printf 'Vendoring Turso...\n'
# Use the frozen source, not scripts or pins that may change during this
# download. Resolve Go module-cache paths rather than the new vendor/ tree.
GOFLAGS=-mod=mod bash "$dir/garner/tools/turso/vendor.sh" "$dir/turso"
cp "$dir/garner/tools/turso/build.sh" "$dir/garner/tools/turso/turso.env" "$dir/turso/"
cat >"$dir/README.md" <<EOF
# garner $version: corresponding source

This archive holds the corresponding source of the programs garner
$version ships: the terminal interface \`garner\`, the server \`garnerd\`
with the Turso library it embeds, and FFprobe in the server's container
image. It does not hold the source of the compilers, of Rust's standard
library or of the Alpine Linux packages the image is based on and built
with; \`garner/docs/source.md\` lists what each build needs.

| Folder | Holds |
| --- | --- |
| \`garner/\` | garner's source from commit $commit, with the Go modules it builds with in \`vendor/\` |
| \`ffmpeg/\` | The FFmpeg release the image's FFprobe is built from; \`garner/Dockerfile\` holds its build options |
| \`turso/\` | The source of the Turso library \`garnerd\` embeds, the crates it builds with in \`vendor/\` and \`build.sh\`, which builds it without the network |

\`garner/docs/source.md\` describes how to build each part from this
archive.

garner is licensed under the GNU Affero General Public License version 3
only: see \`garner/LICENSE\` and \`garner/NOTICE\`. Third-party code keeps
its own licence. \`garner/THIRD-PARTY-NOTICES.txt\` lists the code compiled
into the programs with its licences; FFmpeg is under the LGPL 2.1 or later.
EOF

# Owner and macOS metadata stay out, so the archive does not depend on
# who built it.
printf 'Packing %s.tar.gz...\n' "$name"
if tar --version | grep -q GNU; then
	owner=(--owner=0 --group=0 --numeric-owner)
else
	owner=(--uid 0 --gid 0 --uname root --gname root --no-xattrs --no-mac-metadata)
fi
(cd "$out" && COPYFILE_DISABLE=1 tar -cf - "${owner[@]}" "$name" | gzip -n -9 >"$name.tar.gz")
printf '✓ %s/%s.tar.gz (%s)\n' "$out" "$name" "$(du -h "$out/$name.tar.gz" | awk '{ print $1 }')"
