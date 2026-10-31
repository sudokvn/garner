#!/usr/bin/env bash
# Writes a version's release notes to stdout: the commits since the last
# published release that change garner itself, then what to know before
# upgrading. A commit counts when it changes a file other than the
# documentation, the tests, CI and the project's scripts in tools/, which
# GoReleaser cannot tell from commit messages. The build files, such as
# .goreleaser.yaml and the Dockerfile, count: they decide what users get. The upgrade notes say when
# the version changes the database, followed by the note given with
# `mise run release --note`, which the tag's message keeps.
#
# Usage: tools/release-notes.sh VERSION [COMMIT]
#
# COMMIT defaults to the tag VERSION; RELEASE_NOTE stands in for the tag's
# note before the tag exists. Needs the tags and the GitHub CLI, which
# lists the published releases.
set -euo pipefail

version=${1:?give the version, such as v0.9.0}
commit=${2:-$version}
note=${RELEASE_NOTE:-}
if [ -z "$note" ] && [ "$(git cat-file -t "refs/tags/$version" 2>/dev/null)" = tag ]; then
	note=$(git for-each-ref --format='%(contents:body)' "refs/tags/$version")
fi

# The last published release before this version. A failed release keeps
# its tag, so its commits go out with the next version.
published=$(gh release list --limit 100 --exclude-drafts --json tagName --jq '.[].tagName')
previous=$({
	printf '%s\n' "$published" | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' || true
	printf '%s\n' "$version"
} | sort -uV | awk -v v="$version" '$0 == v { print prev; exit } { prev = $0 }')
range=$commit
[ -z "$previous" ] || range="$previous..$commit"

# own reports whether a path is only the project's own business: users
# see none of it in garner.
own() {
	case $1 in
	README.md | CONTRIBUTING.md | SECURITY.md | AGENTS.md | .gitignore | mise.toml) return 0 ;;
	docs/* | .github/* | tools/* | */testdata/* | *_test.go) return 0 ;;
	esac
	return 1
}

changes=""
while read -r sha subject; do
	while read -r path; do
		if ! own "$path"; then
			changes+="* $subject"$'\n'
			break
		fi
	done < <(git diff-tree --no-commit-id --name-only -r --root "$sha")
done < <(git log --no-merges --format='%H %s' "$range")

printf '## Changelog\n\n'
if [ -n "$changes" ]; then
	printf '%s' "$changes"
else
	printf 'Nothing users see changed: only the documentation, the tests and the project'"'"'s scripts.\n'
fi

migrations=""
if [ -n "$previous" ]; then
	migrations=$(git diff --name-only --diff-filter=A "$previous" "$commit" -- internal/store/migrations)
fi
if [ -n "$migrations" ] || [ -n "$note" ]; then
	printf '\n## Upgrading\n'
	if [ -n "$migrations" ]; then
		# shellcheck disable=SC2016 # the backticks are Markdown
		printf '\nThis version changes the database. The first time it starts, garner backs the database up to `backups/` in its data folder, then changes it. Going back to %s needs that backup.\n' "$previous"
	fi
	if [ -n "$note" ]; then
		printf '\n%s\n' "$note"
	fi
fi
