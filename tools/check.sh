#!/usr/bin/env bash
# Checks the working copy in place: formatting, vet, the shell scripts,
# the links in the docs, the generated database code, the third-party
# notices and known vulnerabilities; the tests on macOS with the race
# detector, and again on Linux in the Go image the container is built
# with; and builds for Linux x86. It checks every file in the folder that
# Git does not ignore, committed or not, so it works the same with Git and
# with jj. Run it before every push.
#
# Usage: mise run check
set -euo pipefail

[ $# -eq 0 ] || {
	echo "mise run check takes no arguments; it checks the working copy" >&2
	exit 2
}
root=$(git rev-parse --show-toplevel)
cd "$root"

fail() {
	printf '✗ %s\n' "$1" >&2
	exit 1
}
step() { printf '\n· %s\n' "$1"; }

# Passes go test's output on without a line per passing package.
quiet() { grep -v -E '^(ok|\?) ' || true; }

# The folder's files as a Git tree, written through an index of its own so
# Git's index stays as it is: every file Git does not ignore, committed or
# not.
index=$(mktemp)
trap 'rm -f "$index"' EXIT
tree() {
	rm -f "$index"
	GIT_INDEX_FILE=$index git add -A
	GIT_INDEX_FILE=$index git write-tree
}

podman info >/dev/null 2>&1 || fail "Podman does not answer; start it with: podman machine start"
image=$(sed -n 's/^ARG GOLANG=//p' Dockerfile)
tags=$(sed -n 's/.*go build -tags \([^ ]*\) .*/\1/p' Dockerfile)
[ -n "$image" ] && [ -n "$tags" ] || fail "the Dockerfile names no Go image or build tags"

checked=$(tree)
printf 'Checking the working folder in %s\n' "$root"

export GOTOOLCHAIN=local

step "Formatting"
unformatted=$(gofmt -l cmd internal tools)
[ -z "$unformatted" ] || fail "not formatted: $unformatted"

step "Vet"
go vet ./... || fail "vet found problems"

step "Shell scripts"
shellcheck -x install.sh tools/*.sh tools/turso/*.sh || fail "shellcheck found problems"
bash tools/versions_test.sh || fail "the release scripts order versions wrongly"
bash tools/install_test.sh || fail "install.sh does not install releases as it should"

step "Links in the Markdown files"
bash tools/links.sh || fail "the docs link to a file or heading that does not exist; see above"

step "Generated database code"
(cd internal/store && sqlc diff) || fail "internal/store/query does not match the SQL; run mise run generate"

# The notices name the licenses of everything the released binaries
# compile in; go-licenses, pinned in mise.toml, classifies them.
step "Third-party notices"
go run ./tools/notices --check || fail "THIRD-PARTY-NOTICES.txt is not current; run mise run notices"

# govulncheck reads the code and what it calls; it needs nothing built.
step "Known vulnerabilities, on macOS and in the server as the container image builds it"
govulncheck ./... || fail "known vulnerabilities reach garner's code on macOS; see above"
GOOS=linux GOARCH=amd64 govulncheck -tags "$tags" ./... || fail "known vulnerabilities reach garner's code on Linux; see above"

# The race detector needs cgo, which the Linux run below builds without.
step "Tests on macOS, with the race detector"
go test -race ./... 2>&1 | quiet || fail "tests failed on macOS"

step "Tests on Linux ($(podman info --format '{{.Host.Arch}}'), tags $tags, as the container image builds)"
# The container is thrown away; it needs no SELinux separation, and
# disabling it leaves the labels of the mounted folders alone.
podman run --rm --security-opt label=disable \
	-v "$root:/src:ro" \
	-v garner-check-gomod:/go/pkg/mod -v garner-check-gocache:/root/.cache/go-build \
	-e CGO_ENABLED=0 -e GOFLAGS="-tags=$tags" -e GOTOOLCHAIN=local \
	-w /src "$image" \
	sh -c 'set -eo pipefail; apk add --no-cache -q libgcc; go vet ./...; go test ./... 2>&1 | { grep -v -E "^(ok|\?) " || true; }' ||
	fail "vet or tests failed on Linux"

step "Build for Linux x86: garnerd as the container image builds it, garner as a release"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags "$tags" -o /dev/null ./cmd/garnerd || fail "garnerd does not build for Linux x86"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/garner || fail "garner does not build for Linux x86"

[ "$(tree)" = "$checked" ] || fail "files changed while checking; run mise run check again"
printf '\n✓ Passed\n'
