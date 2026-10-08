#!/bin/sh
# Print the release notes of a source commit's build: a line naming the
# commit, then the subjects of the commits since the previous version, oldest
# first, grouped as Features (feat), Fixes (fix) and Other, without the
# `chore: release` commits. Runs inside the repository.
#
#   scripts/release-notes.sh <source-sha>
#
# The previous version's commit is the newest commit that changed VERSION and
# whose VERSION differs from the source commit's; with none, every commit up
# to the source commit is listed.
set -eu

die() {
  echo "release-notes: $*" >&2
  exit 2
}

[ $# -eq 1 ] || die "usage: release-notes.sh <source-sha>"
source=$(git rev-parse --verify --quiet "$1^{commit}") || die "$1 is not a commit"
version=$(git show "$source:VERSION")

previous=
for commit in $(git log --format=%H "$source" -- VERSION); do
  if [ "$(git show "$commit:VERSION")" != "$version" ]; then
    previous=$commit
    break
  fi
done
range=$source
[ -z "$previous" ] || range=$previous..$source

printf 'Build of %s.\n' "$source"
git log --reverse --no-merges --no-color --format='%s (%h)' "$range" | awk '
  function section(title, items, n,   i) {
    if (n == 0) return
    printf "\n### %s\n", title
    for (i = 1; i <= n; i++) printf "- %s\n", items[i]
  }
  /^chore: release / { next }
  /^feat(\([^)]*\))?!?:/ { features[++nf] = $0; next }
  /^fix(\([^)]*\))?!?:/ { fixes[++nx] = $0; next }
  { other[++no] = $0 }
  END {
    section("Features", features, nf)
    section("Fixes", fixes, nx)
    section("Other", other, no)
  }
'
