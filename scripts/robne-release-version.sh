#!/usr/bin/env bash
# robne release version, computed from the current checkout (#877):
#   0.<phase>.YYYYMMDD-HHMM-<sha7>     e.g. 0.17.20261118-0215-3d35cc8
#
# - <phase>   from the branch name (pgarciaq-rosocp-superpowers-phase<N>)
# - stamp     the HEAD commit's committer date in UTC — deterministic per
#             commit, so rebuilding the same commit yields the same version
# - <sha7>    FIRST 7 of HEAD, matching git %h and ${GITHUB_SHA::7} in
#             publish-quay.yml, so a robne release joins 1:1 with the server
#             image tag built from the same commit (parity skill step 0)
#
# Deliberately NOT v-prefixed: the Go module proxy ignores these tags, so the
# root module never gets accidental releases from them.
#
# Fails loudly on a non-phase branch (main carries no phase number — the
# post-fork-transition rule is still TBD) and on a dirty tree (the tag would
# not describe the worktree being released).
set -euo pipefail

branch="$(git rev-parse --abbrev-ref HEAD)"
if [[ ! "$branch" =~ ^pgarciaq-rosocp-superpowers-phase([0-9]+)$ ]]; then
	echo "robne-release-version: branch '${branch}' is not a pgarciaq-rosocp-superpowers-phase<N> branch" >&2
	exit 1
fi
phase="${BASH_REMATCH[1]}"

if ! git diff-index --quiet HEAD --; then
	echo "robne-release-version: working tree is dirty; commit or stash first (the tag must describe what is released)" >&2
	exit 1
fi

stamp="$(TZ=UTC git log -1 --format=%cd --date=format:%Y%m%d-%H%M)"
sha="$(git rev-parse HEAD | cut -c1-7)"
echo "0.${phase}.${stamp}-${sha}"
