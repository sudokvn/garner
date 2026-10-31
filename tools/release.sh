#!/usr/bin/env bash
# Releases a commit of main as a version: tags it, pushes the tag, and
# follows the GitHub workflow that builds the server image, the terminal
# interface and the release. The commit is main's newest unless named, for
# a stable release the newest candidate's; it must be on main on GitHub and
# newer than the last published release.
# Every push passed `mise run check`, so what is on main is checked.
#
# Usage: mise run release VERSION [revision] [--note TEXT] [--hotfix]
#
# A candidate, vX.Y.Z-rc.N, goes to the dev channel. A stable release,
# vX.Y.Z, releases the commit of the newest candidate once it proved
# itself (CONTRIBUTING.md, Releases): the nightly passed on that commit,
# and the candidate ran for a day, or for a new minor version for a week,
# the newest candidate for three days. --hotfix skips the waiting, for
# data loss, a security problem or imports broken for everyone.
#
# --note adds what users must know before upgrading, such as a setting
# that is no longer accepted, to the release notes (tools/release-notes.sh).
# The tag's message keeps it for the workflow.
set -euo pipefail
# shellcheck source=tools/versions.sh
source "$(dirname "$0")/versions.sh"

fail() {
	printf '✗ %s\n' "$1" >&2
	exit 1
}

note=""
hotfix=false
args=()
while [ $# -gt 0 ]; do
	case $1 in
	--note)
		[ $# -ge 2 ] && [ -n "$2" ] || fail "--note needs the text"
		note=$2
		shift 2
		;;
	--hotfix)
		hotfix=true
		shift
		;;
	*)
		args+=("$1")
		shift
		;;
	esac
done
version=${args[0]:-}
rev=${args[1]:-main@origin}
[[ "$version" =~ $VERSION_PATTERN ]] && [ ${#args[@]} -le 2 ] ||
	fail "give the version and, optionally, the commit: mise run release v0.9.0-rc.1 [revision] [--note TEXT] [--hotfix]"
! is_candidate "$version" || [ "$hotfix" = false ] || fail "--hotfix is for a stable release; a candidate never waits"
root=$(git rev-parse --show-toplevel)
cd "$root"

jj git fetch --quiet
git fetch --quiet --tags origin
git rev-parse --verify --quiet "refs/tags/$version" >/dev/null && fail "$version exists already"
latest=$(git tag --list 'v*' | sort_versions | tail -1)
[ -z "$latest" ] || [ "$(printf '%s\n%s\n' "$latest" "$version" | sort_versions | tail -1)" = "$version" ] ||
	fail "$version is not newer than $latest"
# A failed release keeps its tag, so its commit may go out again under the
# next version; only a published release counts.
published=$(gh release list --limit 100 --exclude-drafts --json tagName --jq '.[].tagName' | sort_versions) ||
	fail "could not list the releases on GitHub"
stable=$(printf '%s\n' "$published" | grep -E "$STABLE_PATTERN" | tail -1 || true)
# The candidates since the last stable release, oldest first: the list is
# sorted, so all that follows it.
candidates=$(printf '%s\n' "$published" | awk -v s="$stable" 's == "" || seen { print } $0 == s { seen = 1 }' |
	grep -v -E "$STABLE_PATTERN" || true)
candidate=$(printf '%s\n' "$candidates" | tail -1)
if ! is_candidate "$version"; then
	[ -n "$candidate" ] || fail "release a candidate first, then this one once it proved itself: mise run release $version-rc.1"
	# A release goes out under its candidate's version. Only when that
	# version's release failed, so its tag is taken, does the next patch
	# version release the same candidate.
	base=${candidate%-rc.*}
	[ "$base" = "$version" ] || git rev-parse --verify --quiet "refs/tags/$base" >/dev/null ||
		fail "the newest candidate, $candidate, is for $base; release it as $base, or a candidate for $version first"
	# What goes out is what the dev channel ran: the newest candidate.
	[ ${#args[@]} -ge 2 ] || rev=$(git rev-list -n 1 "refs/tags/$candidate")
fi

commits=$(jj log -r "$rev" --no-graph -T 'commit_id ++ "\n"' 2>/dev/null) || fail "$rev names no commit"
[ "$(printf '%s\n' "$commits" | grep -c .)" = 1 ] || fail "$rev is not exactly one commit"
commit=$commits
[ -n "$(jj log -r "$commit & ::main@origin" --no-graph -T commit_id)" ] ||
	fail "${commit:0:12} is not on main on GitHub; push it first"

if is_candidate "$version"; then
	# A new candidate brings something new to the dev channel.
	newest=$(printf '%s\n' "$published" | tail -1)
	[ -z "$newest" ] || ! git merge-base --is-ancestor "$commit" "$newest" ||
		fail "${commit:0:12} is already part of $newest"
else
	[ -z "$stable" ] || ! git merge-base --is-ancestor "$commit" "$stable" ||
		fail "${commit:0:12} is already part of $stable"
	[ "$(git rev-list -n 1 "refs/tags/$candidate")" = "$commit" ] ||
		fail "${commit:0:12} is not the commit of the newest candidate, $candidate; release that commit, or a new candidate first"
	[ "$(gh run list --workflow nightly.yml --commit "$commit" --status success --limit 1 --json databaseId --jq length)" != 0 ] ||
		fail "the nightly has not passed on ${commit:0:12}; run it with: gh workflow run nightly.yml --ref $candidate"
	hours() { gh release view "$1" --json publishedAt --jq '(now - (.publishedAt | fromdateiso8601)) / 3600 | floor'; }
	# The wait counts from the first candidate of this version; one of a
	# version given up for another does not count.
	first=$(printf '%s\n' "$candidates" | awk -v b="$base-rc." 'index($0, b) == 1' | head -1)
	newest_hours=$(hours "$candidate")
	first_hours=$(hours "$first")
	if [[ "$base" == *.0 ]]; then
		wait="$candidate ran $((newest_hours / 24)) days and $first $((first_hours / 24)); a new minor version waits for 3 and 7"
		[ "$first" != "$candidate" ] || wait="$candidate ran $((newest_hours / 24)) days; a new minor version waits for 7"
		ok=$((newest_hours >= 72 && first_hours >= 168))
	else
		wait="$candidate ran $newest_hours hours; a fix waits for 24"
		ok=$((newest_hours >= 24))
	fi
	if [ "$ok" = 1 ]; then
		printf '%s\n' "${wait%%;*}"
	elif [ "$hotfix" = true ]; then
		printf '! %s; released early as a hotfix\n' "$wait"
	else
		fail "$wait. For data loss, a security problem or imports broken for everyone: --hotfix"
	fi
fi

printf 'Releasing %s (%s) as %s\n' "${commit:0:12}" "$(jj log -r "$commit" --no-graph -T 'description.first_line()')" "$version"
# The notes the workflow will write, to read while it runs.
if notes=$(RELEASE_NOTE=$note bash tools/release-notes.sh "$version" "$commit"); then
	printf '\nRelease notes:\n\n%s\n\n' "$notes" | sed 's/^/  /'
else
	printf '! could not preview the release notes; the workflow writes them\n' >&2
fi
message=("-m" "garner $version")
[ -z "$note" ] || message+=("-m" "$note")
git tag -a "$version" "${message[@]}" "$commit"
git push --quiet origin "refs/tags/$version" || {
	git tag -d "$version" >/dev/null
	fail "could not push the tag $version; nothing was released"
}

# The tag push starts the release workflow; wait until GitHub lists it.
run=""
for _ in $(seq 1 30); do
	run=$(gh run list --workflow release.yml --limit 20 --json databaseId,headBranch \
		--jq ".[] | select(.headBranch == \"$version\") | .databaseId" | head -1)
	[ -n "$run" ] && break
	sleep 2
done
[ -n "$run" ] || fail "GitHub started no release workflow for $version; see the repository's Actions page"
# Version tags are never deleted, so a failed version stays taken: the
# next candidate, or the next patch version from the same candidate.
if is_candidate "$version"; then
	next="${version%-rc.*}-rc.$((${version##*-rc.} + 1))"
else
	next=$(printf '%s\n' "$version" | awk -F. '{ printf "%s.%s.%d", $1, $2, $3 + 1 }')
fi
gh run watch "$run" --exit-status --interval 10 >/dev/null ||
	fail "the release workflow failed: gh run view $run --log-failed. $version stays taken; fix the cause and release the next version: mise run release $next"

# shellcheck disable=SC2016 # the backticks are literal, as the notes write them
image=$(gh release view "$version" --json body --jq .body | sed -n 's/^Server image: `\(.*\)`$/\1/p')
printf '\n✓ Released %s\n  %s\n  Server image: %s\n' "$version" "$(gh release view "$version" --json url --jq .url)" "$image"
