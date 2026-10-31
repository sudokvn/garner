# shellcheck shell=bash
# Versions for the release scripts, sourced by them. A stable release is
# vX.Y.Z; a candidate for it is vX.Y.Z-rc.N. install.sh keeps its own copy
# of sort_versions, since users run it on its own.

# shellcheck disable=SC2034 # used by the scripts that source this file
VERSION_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[1-9][0-9]*)?$'
STABLE_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+$'

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

# is_candidate reports whether $1 is a candidate, such as v1.0.0-rc.2.
is_candidate() {
	[[ "$1" == *-rc.* ]]
}
