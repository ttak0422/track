#!/usr/bin/env bash
# Keep exactly one PR comment per topic, updated in place.
#
# `gh pr comment --edit-last` picks the last comment the bot wrote, whichever workflow wrote it, so
# two commenting workflows silently overwrite each other. The marker is what tells them apart: it is
# an HTML comment, invisible in the rendered body, and it is what the next run looks itself up by.
#
# Usage: scripts/pr-comment.sh <marker> <pr-number> <body>
set -euo pipefail

marker="<!-- $1 -->"
pr=$2
body="$marker
$3"

found=$(gh api "repos/$GITHUB_REPOSITORY/issues/$pr/comments" --paginate \
  --jq "[.[] | select(.body | startswith(\"$marker\"))] | first | .id // empty")
id=$(printf '%s\n' "$found" | head -n1)

if [ -n "$id" ]; then
  gh api --silent -X PATCH "repos/$GITHUB_REPOSITORY/issues/comments/$id" -f body="$body"
else
  gh api --silent -X POST "repos/$GITHUB_REPOSITORY/issues/$pr/comments" -f body="$body"
fi
