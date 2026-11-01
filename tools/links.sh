#!/usr/bin/env bash
# Checks the links between the repository's Markdown files with lychee,
# pinned in mise.toml: every link to a file must name one that exists, and
# every #anchor a heading in it. --offline leaves links to the web alone.
# It checks the files in the working folder that Git does not ignore,
# committed or not, so it works the same with Git and with jj.
#
# Usage: mise run links (mise run check runs it too)
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
files=()
while IFS= read -r -d '' f; do
	# A deleted file stays in Git's index until the deletion is committed.
	[ -e "$f" ] && files+=("$f")
done < <(git ls-files -z --cached --others --exclude-standard '*.md')
lychee --offline --include-fragments --no-progress "${files[@]}"
