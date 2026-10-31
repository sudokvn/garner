#!/usr/bin/env bash
# Installs or updates `garner`, garner's terminal interface, on macOS or
# Linux: downloads the newest release of a channel for this machine from
# GitHub, checks it against the release's SHA256SUMS and puts it into
# ~/.local/bin. It downloads nothing when that version is installed already.
#
#   curl -fsSL https://raw.githubusercontent.com/trucke/garner/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/trucke/garner/main/install.sh | bash -s -- --channel dev
#
# The stable channel has the stable releases; dev has the release
# candidates as well, such as v0.9.0-rc.1, and always the newest version.
# GARNER_CHANNEL=dev does what --channel dev does. GARNER_VERSION=v0.6.1
# installs that version instead of the newest, and GARNER_INSTALL_DIR
# another folder instead of ~/.local/bin.
set -euo pipefail

repo=https://github.com/trucke/garner
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
			version=$(curl -fsSL "https://api.github.com/repos/trucke/garner/releases?per_page=30" |
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
	mkdir -p "$dir"
	tmp=$(mktemp -d)
	# Next to its target, so the rename below replaces it in one step, even
	# while garner runs.
	part="$dir/.garner-$version.$$"

	printf 'Downloading garner %s for %s on %s...\n' "$version" "$os" "$arch"
	curl -fsSL -o "$part" "$repo/releases/download/$version/$file" ||
		fail "could not download $file from $repo/releases/tag/$version"
	curl -fsSL -o "$tmp/SHA256SUMS" "$repo/releases/download/$version/SHA256SUMS" ||
		fail "could not download the checksums of $version"

	local want got
	want=$(awk -v f="$file" '$2 == f { print $1 }' "$tmp/SHA256SUMS")
	[ -n "$want" ] || fail "SHA256SUMS of $version lists no $file"
	if command -v sha256sum >/dev/null; then
		got=$(sha256sum "$part" | awk '{ print $1 }')
	else
		got=$(shasum -a 256 "$part" | awk '{ print $1 }')
	fi
	[ "$got" = "$want" ] || fail "$file does not match its checksum; nothing was installed"

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
