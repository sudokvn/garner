#!/usr/bin/env bash
# Installs or updates `garner`, garner's terminal interface, on macOS or
# Linux: downloads the newest release of a channel for this machine from
# GitHub, checks it against the release's SHA256SUMS and puts it into
# ~/.local/bin. Packaged releases keep their licence files alongside the
# binary in garner-licenses/. It downloads no release files when that
# version is installed already.
#
#   curl -fsSL https://raw.githubusercontent.com/sudokvn/garner/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/sudokvn/garner/main/install.sh | bash -s -- --channel dev
#
# The stable channel has the stable releases; dev has the release
# candidates as well, such as v0.9.0-rc.1, and always the newest version.
# GARNER_CHANNEL=dev does what --channel dev does. GARNER_VERSION=v0.6.1
# installs that version instead of the newest, and GARNER_INSTALL_DIR
# another folder instead of ~/.local/bin.
set -euo pipefail

repo=https://github.com/sudokvn/garner
# What a failed or cut-short run leaves behind; removed on exit.
tmp=""
part=""
trap 'rm -rf "$tmp" "$part"' EXIT

fail() {
	printf '✗ %s\n' "$1" >&2
	exit 1
}

version_pattern='^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[1-9][0-9]*)?$'

# newest prints the newest of the versions on stdin, a release after its
# candidates; sort -V would put v1.0.0 before v1.0.0-rc.1. The release
# scripts sort the same way (tools/versions.sh).
newest() {
	local v base major minor patch rc
	while read -r v; do
		[[ "$v" =~ $version_pattern ]] || continue
		base=${v#v}
		base=${base%%-*}
		IFS=. read -r major minor patch <<<"$base"
		rc=0
		[[ "$v" != *-rc.* ]] || rc=$((${v##*-rc.} - 1000000))
		printf '%d %d %d %d %s\n' "$major" "$minor" "$patch" "$rc" "$v"
	done | sort -n -k1,1 -k2,2 -k3,3 -k4,4 | awk 'END { print $5 }'
}

# Everything runs from main, so a download cut short runs nothing.
main() {
	command -v curl >/dev/null || fail "garner's installer needs curl"

	local os arch
	case "$(uname -s)" in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) fail "garner runs on macOS and Linux, not on $(uname -s)" ;;
	esac
	case "$(uname -m)" in
	arm64 | aarch64) arch=arm64 ;;
	x86_64 | amd64) arch=amd64 ;;
	*) fail "garner runs on ARM and x86 machines, not on $(uname -m)" ;;
	esac
	# A terminal under Rosetta reports x86 on a Mac with Apple silicon.
	if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null)" = 1 ]; then
		arch=arm64
	fi

	local channel=${GARNER_CHANNEL:-stable}
	while [ $# -gt 0 ]; do
		case $1 in
		--channel)
			[ $# -ge 2 ] || fail "--channel needs stable or dev"
			channel=$2
			shift 2
			;;
		*) fail "unknown option $1; garner's installer takes --channel stable or --channel dev" ;;
		esac
	done

	local version=${GARNER_VERSION:-}
	if [ -z "$version" ]; then
		case $channel in
		stable)
			# GitHub redirects its latest release, the newest stable one, to
			# its tag's page.
			version=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$repo/releases/latest") ||
				fail "could not reach GitHub to find the newest release"
			version=${version##*/}
			;;
		dev)
			# Candidates are pre-releases, which GitHub's latest leaves out.
			version=$(curl -fsSL "https://api.github.com/repos/sudokvn/garner/releases?per_page=30" |
				sed -n 's/^ *"tag_name": *"\([^"]*\)".*/\1/p' | newest) ||
				fail "could not reach GitHub to find the newest release"
			;;
		*) fail "there is no channel $channel; choose stable or dev" ;;
		esac
	fi
	[[ "$version" =~ $version_pattern ]] || fail "no release version found: $version"

	local file="garner-$version-$os-$arch"
	local dir=${GARNER_INSTALL_DIR:-$HOME/.local/bin}
	if [ -x "$dir/garner" ] && [ "$("$dir/garner" --version 2>/dev/null | awk '{ print $2 }')" = "$version" ]; then
		printf '✓ garner %s is installed already in %s\n' "$version" "$dir"
		return
	fi
	local licences="$dir/garner-licenses"
	local packaged=false notice
	tmp=$(mktemp -d)
	curl -fsSL -o "$tmp/SHA256SUMS" "$repo/releases/download/$version/SHA256SUMS" ||
		fail "could not download the checksums of $version"
	# Old MIT releases shipped bare binaries. Choose from the checksum list,
	# never fall back to a bare binary after an archive download fails.
	if awk -v f="$file.tar.gz" '$2 == f { found = 1 } END { exit !found }' "$tmp/SHA256SUMS"; then
		file="$file.tar.gz"
		packaged=true
	fi

	local want got
	want=$(awk -v f="$file" '$2 == f { print $1 }' "$tmp/SHA256SUMS")
	[ -n "$want" ] || fail "SHA256SUMS of $version lists no $file"

	printf 'Downloading garner %s for %s on %s...\n' "$version" "$os" "$arch"
	curl -fsSL -o "$tmp/download" "$repo/releases/download/$version/$file" ||
		fail "could not download $file from $repo/releases/tag/$version"
	if command -v sha256sum >/dev/null; then
		got=$(sha256sum "$tmp/download" | awk '{ print $1 }')
	else
		got=$(shasum -a 256 "$tmp/download" | awk '{ print $1 }')
	fi
	[ "$got" = "$want" ] || fail "$file does not match its checksum; nothing was installed"

	if [ "$packaged" = true ]; then
		command -v tar >/dev/null || fail "garner's installer needs tar"
		# Read only the expected files to stdout. Archive paths and symlinks
		# are never extracted into the installation or temporary directories.
		for notice in garner LICENSE NOTICE THIRD-PARTY-NOTICES.txt; do
			tar -xOzf "$tmp/download" "$notice" >"$tmp/$notice" || fail "$file is missing $notice"
			[ -s "$tmp/$notice" ] || fail "$file has an empty $notice"
		done
		mkdir -p "$licences"
		for notice in LICENSE NOTICE THIRD-PARTY-NOTICES.txt; do
			cp "$tmp/$notice" "$licences/$notice"
			chmod 644 "$licences/$notice"
		done
	else
		mv "$tmp/download" "$tmp/garner"
	fi
	mkdir -p "$dir"
	# Next to its target, so the rename below replaces it in one step, even
	# while garner runs. Install notices before making a new binary visible.
	part="$dir/.garner-$version.$$"
	cp "$tmp/garner" "$part"
	chmod 755 "$part"
	mv -f "$part" "$dir/garner"
	part=""
	printf '✓ Installed %s to %s\n' "$("$dir/garner" --version)" "$dir/garner"

	# shellcheck disable=SC2016 # $PATH is for the user to type
	case ":$PATH:" in
	*":$dir:"*) printf '  Run garner to connect to your server.\n' ;;
	*) printf '  %s is not in your PATH; add it, for example in ~/.bashrc or ~/.zshrc:\n    export PATH="%s:$PATH"\n' "$dir" "$dir" ;;
	esac
}

main "$@"
