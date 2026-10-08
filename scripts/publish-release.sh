#!/bin/sh
# Create the GitHub release v<VERSION> of a published dist build, its tag on
# the dist commit and its notes from release-notes.sh. VERSION must be a
# Semantic Versioning 2.0.0 version, and one with a pre-release part is
# published as a pre-release. An existing release is left as it is, so a
# version's tag keeps naming the first build published for it. Runs inside the
# repository, with gh signed in.
#
#   scripts/publish-release.sh <dist-sha> <source-sha>
set -eu

die() {
  echo "publish-release: $*" >&2
  exit 2
}

# The regular expression semver.org suggests, in POSIX ERE.
number='(0|[1-9][0-9]*)'
identifier='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
semver="^$number\\.$number\\.$number(-$identifier(\\.$identifier)*)?(\\+[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?\$"

[ $# -eq 2 ] || die "usage: publish-release.sh <dist-sha> <source-sha>"
dist=$1
source=$2
version=$(git show "$source:VERSION")
# grep matches line by line, so a second line would pass unchecked.
case $version in
*"
"*) die "VERSION has more than one line, so it is not a semantic version" ;;
esac
printf '%s\n' "$version" | grep -Eq "$semver" || die "VERSION $version is not a semantic version"
tag=v$version
case ${version%%+*} in
*-*) set -- --prerelease ;;
*) set -- ;;
esac

if gh release view "$tag" >/dev/null 2>&1; then
  echo "publish-release: release $tag exists; nothing to publish"
  exit 0
fi

notes=$(mktemp)
trap 'rm -f "$notes"' EXIT
sh "$(dirname "$0")/release-notes.sh" "$source" >"$notes"
gh release create "$tag" --target "$dist" --title "$tag" --notes-file "$notes" "$@"
