#!/usr/bin/env bash
# Checks the version order the release scripts rely on (tools/versions.sh):
# a wrong order would tag the wrong release as latest, or refuse a new one.
# Also the names of dev builds, that the release scripts ignore the dev
# release and that pruning never deletes a release's image.
set -euo pipefail
cd "$(dirname "$0")"
# shellcheck source=tools/versions.sh
source versions.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failed=0
expect() {
	if [ "$2" != "$3" ]; then
		printf '✗ %s\n  got:  %s\n  want: %s\n' "$1" "$2" "$3" >&2
		failed=1
	fi
}

got=$(printf '%s\n' v1.0.0 v0.10.0 v1.0.0-rc.10 v0.9.1 v1.0.0-rc.2 v0.9.0 v1.0.0-rc.1 v2.0.0-rc.1 nightly v1.0 | sort_versions | paste -sd ' ' -)
expect "order" "$got" "v0.9.0 v0.9.1 v0.10.0 v1.0.0-rc.1 v1.0.0-rc.2 v1.0.0-rc.10 v1.0.0 v2.0.0-rc.1"

expect "before a release" "$(printf '%s\n' v0.8.2 v0.9.0-rc.1 v0.9.0-rc.2 | before v0.9.0)" "v0.9.0-rc.2"
expect "before, stable only" "$(printf '%s\n' v0.8.2 v0.9.0-rc.1 | grep -E "$STABLE_PATTERN" | before v0.9.0-rc.2)" "v0.8.2"
expect "before a version in the list" "$(printf '%s\n' v0.8.1 v0.8.2 v0.9.0 | before v0.8.2)" "v0.8.1"
expect "before the first" "$(printf '%s\n' v0.8.2 | before v0.1.0)" ""

is_candidate v1.0.0-rc.3 || expect "candidate" "release" "candidate"
! is_candidate v1.0.0 || expect "release" "candidate" "release"

same_minor v0.9.0 v0.9.2 || expect "same minor" "different" "same"
same_minor v0.9.0 v0.9.1-rc.1 || expect "same minor, candidate" "different" "same"
! same_minor v0.9.0 v0.10.0 || expect "next minor" "same" "different"
! same_minor v0.9.0 v1.9.0 || expect "next major" "same" "different"

# The release scripts list the published releases; the dev release and
# a dev version must count as none, or release.sh would see no candidate
# and release-notes.sh no previous release.
gh() { printf '%s\n' dev v0.10.0 v0.11.0-rc.1-dev.2 v0.11.0-rc.1 v0.9.0; }
expect "published releases" "$(published_versions | paste -sd ' ' -)" "v0.9.0 v0.10.0 v0.11.0-rc.1"
unset -f gh

# Dev builds: the newest version tag merged into the commit, a stable
# release over its candidate on the same commit, and the dev tag ignored.
repo="$work/repo"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.invalid GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.invalid
git init -q -b main "$repo"
commit() { git -C "$repo" commit -q --allow-empty -m "$1"; }
commit one
git -C "$repo" tag v0.9.0
commit two
git -C "$repo" tag -a -m candidate v0.10.0-rc.1
commit three
commit four
git -C "$repo" tag dev
dev_of() { (cd "$repo" && dev_version "${1:-HEAD}"); }
expect "dev after a candidate" "$(dev_of)" "v0.10.0-rc.1-dev.2"
expect "dev of a tagged commit" "$(dev_of HEAD~2)" "v0.10.0-rc.1-dev.0"
git -C "$repo" tag v0.10.0 v0.10.0-rc.1^{}
expect "dev after a stable release" "$(dev_of)" "v0.10.0-dev.2"
[[ "$(dev_of)" =~ $DEV_PATTERN ]] || expect "dev pattern" "$(dev_of)" "a match"
[[ "$(dev_of)" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || expect "image tag" "$(dev_of)" "a valid image tag"
expect "dev builds are no versions" "$(printf '%s\n' v0.10.0-dev.2 v0.10.0 | sort_versions)" "v0.10.0"

# Pruning keeps the newest dev images and never selects a release's.
images=$(
	for i in 1 2 3 4; do printf '%s\t2026-01-0%sT00:00:00Z\tv0.10.0-dev.%s\n' "d$i" "$i" "$i"; done
	printf 'r1\t2026-01-01T12:00:00Z\tv0.10.0\n'
	printf 'r2\t2026-01-02T12:00:00Z\tv0.10.1-rc.1,latest\n'
	printf 'r3\t2025-12-01T12:00:00Z\tv0.9.0-rc.1\n'
	printf 'x1\t2025-01-01T00:00:00Z\t\n'
	printf 'x2\t2025-01-01T00:00:00Z\tv0.10.0-dev.0,dev\n'
)
expect "old dev images" "$(printf '%s\n' "$images" | old_images dev 2 | paste -sd ' ' -)" "d2 d1"
expect "old release images" "$(printf '%s\n' "$images" | old_images release 1 | paste -sd ' ' -)" "r3"

[ "$failed" = 0 ] && printf '✓ version order, dev builds and image pruning\n'
exit "$failed"
