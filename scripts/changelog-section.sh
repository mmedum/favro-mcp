#!/usr/bin/env bash
#
# Print one version's section of CHANGELOG.md, for use as the release-notes
# header. The published release then leads with the hand-written summary and
# keeps GitHub's generated PR list underneath.
#
# Inputs:
#   $1                   tag or version, with or without the leading "v"
#                        (v1.1.1 and 1.1.1 both resolve to the [1.1.1] heading).
#   CHANGELOG.md         Keep a Changelog format: `## [x.y.z] - YYYY-MM-DD`
#                        headings, link references at the bottom.
#
# Output: the section body on stdout, without its own `## [x.y.z]` heading —
# goreleaser renders it above the changelog it generates, so a second heading
# there would compete with the release title.
#
# Exits non-zero if the version has no section. That is deliberate: a release
# whose changelog entry was never written should fail loudly at tag time
# rather than publish with empty notes.

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ $# -ne 1 ]]; then
	echo "usage: $0 <version>" >&2
	exit 2
fi

version="${1#v}"

section="$(
	awk -v want="## [${version}]" '
		index($0, want) == 1 { collecting = 1; next }
		collecting && /^## / { exit }
		# Drop the link-reference block at the bottom of the file.
		collecting && /^\[.*\]: http/ { exit }
		collecting { print }
	' CHANGELOG.md
)"

# Strip leading and trailing blank lines.
section="$(printf '%s\n' "$section" | sed -e '/./,$!d' -e ':a' -e '/^\n*$/{$d;N;ba' -e '}')"

if [[ -z "$section" ]]; then
	echo "$0: no CHANGELOG.md section found for version ${version}" >&2
	exit 1
fi

printf '%s\n' "$section"
