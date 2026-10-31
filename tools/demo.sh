#!/usr/bin/env bash
# Serves garner on the demo world of internal/simulation/demo_test.go and
# drives the terminal interface with VHS through a tape, to record it.
# Nothing reaches a real service or library.
#
# Needs VHS (https://github.com/charmbracelet/vhs) and the Geist Mono
# font; VHS falls back to another font without telling.
#
# Usage: mise run demo <tape> [demo flags]; the flags go to the demo, such
# as -demo-tick 1s for slower downloads. The tape runs in the repository,
# with GARNER_CONNECTION set for bin/garner.
set -euo pipefail

tape=$(realpath "${1:?usage: mise run demo <tape> [demo flags]}")
shift
cd "$(dirname "$0")/.."
command -v vhs >/dev/null || { echo "demo: VHS is missing: https://github.com/charmbracelet/vhs" >&2; exit 1; }

go build -o bin/garner ./cmd/garner
go test -c -o bin/demo ./internal/simulation

# The interface shows this folder in the paths of the library's files, so
# it is short and the same on every machine. Creating it claims it: a
# second recording finds it and stops without touching it.
root=/tmp/garner-demo
if ! mkdir "$root" 2>/dev/null; then
	echo "demo: $root exists: another recording runs, or one ended without cleaning up; remove it after that one ended" >&2
	exit 1
fi
work=$(mktemp -d)
conn="$work/connection"
pid=
trap '[ -n "$pid" ] && { kill "$pid" 2>/dev/null; wait "$pid" 2>/dev/null; }; rm -rf "$root" "$work"' EXIT

bin/demo -test.run '^TestDemo$' -test.timeout 0 -demo -demo-root "$root" -demo-connection "$conn" "$@" &
pid=$!

for _ in $(seq 100); do
	[ -s "$conn" ] && break
	kill -0 "$pid" 2>/dev/null || { echo "demo: the demo stopped before it served" >&2; exit 1; }
	sleep 0.1
done
[ -s "$conn" ] || { echo "demo: the demo did not serve within 10 seconds" >&2; exit 1; }

GARNER_CONNECTION="$conn" vhs "$tape"
