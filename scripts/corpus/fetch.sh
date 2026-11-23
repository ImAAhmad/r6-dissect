#!/usr/bin/env bash
# Rebuild the local Y11S1 replay corpus from the pinned git blobs in a manifest.
#
# usage: fetch.sh <out-dir> [manifest]
#
# The replays are third-party data and are never committed. This script
#   1. fetches the fork branches named in the manifest into a scratch BARE repo
#      (<out-dir>.objects.git, next to <out-dir>, outside this repository),
#   2. reads each pinned blob with `git cat-file blob <sha>` and verifies it with
#      `git hash-object`.
# It never checks out, builds or runs fork code.
#
# Environment:
#   R6_CORPUS_MIRROR  a local git dir holding refs/remotes/<owner>/<branch> for every
#                     fork in the manifest; fetched from instead of GitHub.
set -euo pipefail

usage="usage: fetch.sh <out-dir> [manifest]"
out=${1:?$usage}
here=$(cd "$(dirname "$0")" && pwd)
manifest=${2:-$here/y11s1-forks.tsv}
repo=$(git -C "$here" rev-parse --show-toplevel)
out=$(realpath -m "$out")
case "$out/" in
  "$repo"/*) echo "fetch.sh: refusing to write inside the repository ($out)" >&2; exit 2 ;;
esac
[ -f "$manifest" ] || { echo "fetch.sh: no manifest at $manifest" >&2; exit 2; }

objects="$out.objects.git"
mkdir -p "$out"
[ -d "$objects" ] || git init -q --bare "$objects"

# Columns: blob_sha build source_ref source_path target_path
refs=$(cut -f3 "$manifest" | sort -u)
for ref in $refs; do
  owner=${ref%%/*}
  branch=${ref#*/}
  if [ -n "${R6_CORPUS_MIRROR:-}" ]; then
    src="file://$(realpath "$R6_CORPUS_MIRROR")"
    spec="refs/remotes/$ref"
  else
    src="https://github.com/$owner/r6-dissect.git"
    spec="refs/heads/$branch"
  fi
  git -C "$objects" fetch -q --no-tags --depth=1 "$src" "+$spec:refs/fetched/$ref"
done

n=0
missing=0
while IFS=$'\t' read -r sha _build ref path target; do
  [ -z "$sha" ] && continue
  case "$target" in
    /*|*..*) echo "fetch.sh: unsafe target path $target" >&2; exit 2 ;;
  esac
  if ! git -C "$objects" cat-file -e "$sha^{blob}" 2>/dev/null; then
    echo "missing $sha ($ref:$path)" >&2
    missing=$((missing + 1))
    continue
  fi
  mkdir -p "$out/$(dirname "$target")"
  git -C "$objects" cat-file blob "$sha" > "$out/$target"
  got=$(git hash-object "$out/$target")
  if [ "$got" != "$sha" ]; then
    rm -f "$out/$target"
    echo "hash mismatch for $target: want $sha got $got" >&2
    exit 1
  fi
  n=$((n + 1))
done < "$manifest"
echo "fetched $n files into $out ($missing missing)"
[ "$missing" -eq 0 ]
