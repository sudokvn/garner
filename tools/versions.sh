# shellcheck shell=bash
# Versions for the release scripts and the workflows, sourced by them. A
# stable release is vX.Y.Z; a candidate for it is vX.Y.Z-rc.N. A dev build
# of main is named after the newest version before it, such as
# v0.9.0-rc.1-dev.4; it is no release, and the functions that order
# releases leave it out, as they leave out the rolling release dev.
# install.sh checks versions in its own patterns, since users run it on
# its own.

# shellcheck disable=SC2034 # used by the scripts that source this file
VERSION_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[1-9][0-9]*)?$'
STABLE_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+$'
DEV_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[1-9][0-9]*)?-dev\.[0-9]+$'

# sort_versions reads versions, one per line, and prints them oldest first,
# each candidate before its release; sort -V puts v1.0.0 before
# v1.0.0-rc.1. Lines that are no version are left out.
sort_versions() {
	local v base major minor patch rc
	while read -r v; do
		[[ "$v" =~ $VERSION_PATTERN ]] || continue
		base=${v#v}
		base=${base%%-*}
		IFS=. read -r major minor patch <<<"$base"
		rc=0 # a release, after all its candidates
		[[ "$v" != *-rc.* ]] || rc=$((${v##*-rc.} - 1000000))
		printf '%d %d %d %d %s\n' "$major" "$minor" "$patch" "$rc" "$v"
	done | sort -n -k1,1 -k2,2 -k3,3 -k4,4 | awk '{ print $5 }'
}

# before prints the version just before $1 among the versions on stdin, or
# nothing when $1 comes first.
before() {
	{
		cat
		printf '%s\n' "$1"
	} | sort_versions | uniq | awk -v v="$1" '$0 == v { print prev; exit } { prev = $0 }'
}

# same_minor reports whether two versions share their major and minor
# version, such as v0.9.0 and v0.9.2-rc.1.
same_minor() {
	[ "$(printf '%s' "$1" | cut -d. -f1,2)" = "$(printf '%s' "$2" | cut -d. -f1,2)" ]
}

# is_candidate reports whether $1 is a candidate, such as v1.0.0-rc.2.
is_candidate() {
	[[ "$1" == *-rc.* ]]
}

# published_versions prints the versions of the releases published on
# GitHub, oldest first. The rolling release dev and drafts are left out.
published_versions() {
	gh release list --limit 100 --exclude-drafts --json tagName --jq '.[].tagName' | sort_versions
}

# dev_version prints the version of a dev build of commit $1: the newest
# version tag merged into it and the number of commits since that tag,
# such as v0.9.0-rc.1-dev.4, or v0.9.0-rc.1-dev.0 for the tagged commit
# itself. It is a valid image tag. It needs the commit's history and tags.
dev_version() {
	local tag
	tag=$(git tag --merged "$1" | sort_versions | tail -1)
	[ -n "$tag" ] || return 1
	printf '%s-dev.%s\n' "$tag" "$(git rev-list --count "refs/tags/$tag..$1")"
}

# old_images reads a container's image versions, one per line as id,
# creation time and the tags joined by commas, separated by tabs. It
# prints the ids of all but the $2 newest images, 20 by default, whose
# tags are all dev versions (kind dev) or all release versions (kind
# release). An untagged image, or one that also carries another tag such
# as latest or dev, is never printed.
old_images() {
	local pattern keep=${2:-20} n=0 id tags tag all list
	case $1 in
	dev) pattern=$DEV_PATTERN ;;
	release) pattern=$VERSION_PATTERN ;;
	*) return 2 ;;
	esac
	sort -t $'\t' -k2,2r | while IFS=$'\t' read -r id _ tags; do
		[ -n "$tags" ] || continue
		all=true
		IFS=, read -ra list <<<"$tags"
		for tag in "${list[@]}"; do
			[[ "$tag" =~ $pattern ]] || all=false
		done
		[ "$all" = true ] || continue
		n=$((n + 1))
		[ "$n" -le "$keep" ] || printf '%s\n' "$id"
	done
}
