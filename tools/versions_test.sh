#!/usr/bin/env bash
# Checks the version order the release scripts rely on (tools/versions.sh):
# a wrong order would tag the wrong release as latest, or refuse a new one.
set -euo pipefail
cd "$(dirname "$0")"
# shellcheck source=tools/versions.sh
source versions.sh

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

[ "$failed" = 0 ] && printf '✓ version order\n'
exit "$failed"
