# Fork port 15: Y11S1 replay corpus from forks (local, uncommitted) + match-level acceptance test

> **Issue:** #29 · prestonwillis10/r6-dissect · fork survey: `docs/research/2026-10-21-fork-survey.md`

> **For agentic workers:** REQUIRED SUB-SKILL: use superpowers:executing-plans (or subagent-driven-development) to implement this plan task by task. Steps use checkbox (`- [ ]`) syntax.

Verified against: `origin/main` = `bd6ccbeb7b46af99638df5d650187069e849f925` (and `origin/fork/health-dbno` = `c608cf8` for the acceptance-test base).

## Summary

We have no Y11S1 replays in the repo. Main has fixtures for Y8S1, Y8S2, Y9S1 and Y11S2 (build 9838928 only), and `track/rec-timeline` adds Y11S3. Two forks commit real Y11S1 replays: nicktstewart (21 rounds, build 9540659, with defuser labels in a test) and ArnaudCabello (25 rounds, build 9578674). We extracted all 46 rounds and parsed them with our parser at bd6ccbe. **Nothing crashes, and every map and operator is known. But the corpus exposes three real bugs** that our committed fixtures cannot show: a `matchFeedback: null` that makes R6Stats reject the whole upload, a scoreboard mapping that shifts by one after a player abandons, and nondeterministic kill attribution caused by that shift.

We do **not** commit these replays. They are third-party players' data with no licence for the data, and they name about 60 real players (usernames and Ubisoft profile UUIDs). Instead:

1. we commit a **manifest** (git blob SHA, build, source ref, path, target folder; no usernames) and a **fetch script** that rebuilds the corpus outside the repo from any git object store holding those blobs;
2. we add a **match-level acceptance test**, `TestAcceptance_Matches`, gated on `R6_DISSECT_CORPUS`. It is the match-level sibling of PR #3's round-level `TestAcceptance_Replays`. Its invariants are red on this corpus today, one per bug above;
3. we ask the user, without blocking anything, whether they have **their own** Y11S1 replays. Those could become committed fixtures under the existing policy.

## Source

- **nicktstewart/r6-dissect `main` (`031e1872`)**: the replays were added in `0b5f2231` "[Add] Testing for newly added features" (2026-03-11), under `dissect/test/data/replays/defuser_detection/match{2..7}/` (21 `.rec` files, 82 MB). `dissect/test/defuser_detection_test.go` asserts the planter and disabler per round. Its "ground truth" comes from `planter-investigation.md` (commit `c6e5151`). For match2, that document says "Ground-truth expectation from current parsed output", so the labels are partly the fork's own parser output, not manual review. Build `Y11S1_Alpha03/9540659`. Recording players: player38, player39 (1v1 CustomGameOnline), player46, player42 (Ranked). Match folders as committed: match2 and match3 are the same 1v1 custom match from the two players' perspectives (0.03–1.1 MB per round); match4 and match5 are full ranked matches (5.5–9.1 MB per round); match6 and match7 are single rounds.
- **ArnaudCabello/r6-dissect `main` (`3cd02085`) = `reinforcements`** (identical blobs): `Matchreplay/` (25 `.rec`, 169 MB), added in `dfdc96d` (2026-04-01). Build `Y11S1_Alpha03/9578674`. **The survey's "5 recording players" is wrong.** There are 2 (player86, player87). The folders mix rounds of different matches: `Matchreplay/Match-2026-03-29_17-32-21-6900/` holds files from 7 matches. Regrouped by match ID, there are 10 matches: 8 Ranked match folders (one full 8-round match, one 5-round match, one 4-round match, and 5 single rounds) and 5 one-player CustomGameLocal rounds.
- **Flaws in the sources:** no licence covers the replay data. Both repos carry upstream's MIT `LICENSE`, which covers code. The data contains third-party usernames and profile UUIDs. nick's labels are not independent ground truth: 2 of 24 assertions are contradicted by the replays (see Current behaviour, D).
- **Other early Y11S2 builds (9718747, 9734089, 9785623): none exist in the mirror.** We grepped every tree at every ref tip (170 refs, 1082 `.rec` paths, 66 distinct blobs) and every reachable commit (`git log --all --raw -- '*.rec'`, 28 extra history-only blobs). Builds found:

  | build | source |
  |---|---|
  | Y11S1 9540659 | nicktstewart/main `dissect/test/data/replays/defuser_detection/` (21) |
  | Y11S1 9578674 | ArnaudCabello/main and /reinforcements `Matchreplay/` (25) |
  | Y11S2 9838928 | ours: `valid/Y11S2/custom_{early_plant,solid_snake}.rec` (mirrored by every fork of ours) |
  | Y11S3 9950954 | ours: `origin/track/rec-timeline` `valid/Y11S3/*` |
  | Y8S1–Y9S1 | upstream fixtures |
  | Y8S4, 2024 matches (history only) | upstream `33cf9a2`/`4dbcf5b` (Y8S4); "harry hayes" `99af979`/`69839b6` (2024-05 test-data, 28 blobs) |

  9734089 is only *named* in code: aa-sasha `Y11S2Alpha3 int = 9734089` and cliffside-git `if r.Header.CodeVersion >= 9734089`. No fork ships a replay of it. Builds 9718747 and 9785623 do not appear even in code.

## Current behaviour (bd6ccbe)

Setup we ran:

```
git clone -q --shared /home/prest/projects/R6Stats/r6-dissect $S/wt-15 && git -C $S/wt-15 checkout -q bd6ccbe
go build -o $S/item15-r6d .
git -C $MIRROR show <ref>:<path> > $S/item15-data/...        # all 46 files
# regrouped into 16 match folders by the Match-<date>-<id> prefix (symlinks)
for m in bymatch/*; do item15-r6d -f json -o out/$m.json $m; done
```

**A. Parse health: clean.** All 16 folders and all 46 single rounds exit 0. There are no panics and no `ERR`/`FTL` logs. No `Map(N)` or `Operator(N)` name appears. In every 10-player round the number of `Kill` events equals the number of `died: true` rows. The only warnings are `scoreboard_kill`, which `dissect/scoreboard.go` emits at WARN for every scoreboard kill update (`log.Warn().` … `Msg("scoreboard_kill")`). That is log noise, not a fault. PR #3's `TestAcceptance_Replays` (`origin/fork/health-dbno` c608cf8) **passes** on the corpus: `R6_DISSECT_REPLAY_DIRS=<16 folders> go test ./dissect/test -run TestAcceptance_Replays` gives `rounds 46, DBNO 61 (57 credited), Revive 4 (3 credited)`, PASS.

**B. Bug 1: a round with no events is written as `"matchFeedback": null`, and R6Stats rejects the match.** The prep-forfeit round R05 in match2/match3 (29 KB and 60 KB) has no events. `MatchReader.Data()` copies the nil slice (`dissect/match.go`: `MatchFeedback: r.MatchFeedback,`), and the JSON is `null`. R6Stats' contract (`api/r6stats/services/parser_contract.py`, `_event(e, i) for i, e in enumerate(_req_list(raw, "matchFeedback", "matchFeedback"))`, where `_required` raises when `value is None`) then refuses the **whole match**:

```
FAIL nick-Match-2026-03-08_19-55-32-21916.json ParserContractError r6-dissect output: 'matchFeedback' must be a list
FAIL nick-Match-2026-03-08_19-55-32-26676.json ParserContractError r6-dissect output: 'matchFeedback' must be a list
(the other 14 folders: OK)
```

The bug does not depend on the game version. Any round with zero events triggers it.

**C. Bug 2: the scoreboard sbID→player map shifts by one after a player abandons, and the reconcile then rewrites correct kills.** In match4 (`Match-2026-03-09_17-13-54-47572`), player48 leaves after round 1. From round 2 on, the scoreboard still has 10 rows, but only 9 player packets exist. `ensureScoreboardMapping` (`dissect/scoreboard.go`) pairs the two lists by position: `r.scoreboardIDToPlayer[r.pendingSBIDs[i]] = r.readPlayerOrder[i]`. The leaver's row is `pendingIdx=4 sbID=70492cf0`, which has no DissectID after it, so every later row is credited to the next player. The debug log for R03 shows `sbID=4c492cf0` resolved as `player27`, but the packet that follows it is `DissectID=50492cf0 … username=player50`. `sbID=21492cf0` (player07, DissectID `25492cf0`) comes out as `scoreboard_kill_unknown_player`. Every DissectID is sbID+4. That is exactly the existing fallback (`candidateID := []byte{sbID[0] + 4, …}`), which would map all 9 correctly, but it only runs for sbIDs beyond the zip. Visible effects:
- player57 has a cumulative score of 420 → **0** in rounds 2–5 (`stats[].score`).
- `scoreboard_reconcile_kill` rewrites kill-feed killers to the wrong player. In R03 it credits player46's death (1:10) to player27, who had died at 1:24. The kill feed said player50.

**D. Bug 3: kill attribution in that match is nondeterministic.** Running the same folder 10 times gave **10 different** kill-feed attributions and match kill totals (for example, one player's total ranged over 2, 3, 4, 5, 6, 7). Six JSON runs gave 6 distinct outputs. The other 15 folders gave 1 distinct output in 6 runs each. The cause is map iteration in the reconcile (the subject of PR #12, ElyasYassin `ee8b078`), and the misaligned map above amplifies it.

**E. Defuser attribution against nick's labels: 22 of 24 assertions agree (92%).**

| match (folder) | round idx | label | ours | verdict |
|---|---|---|---|---|
| match2 and match3 | 0–3 | 7 assertions each (no plant; plant player38 ×3; no disable; disable player39 ×2) | identical | 14/14 |
| match6 `13-44-52` | 0 (R#2) | plant player15, no disable | same | 2/2 |
| match7 `13-24-27` | 0 (R#2) | plant player51, disable player34 | same | 2/2 |
| match4 `47572` | 4 | plant player27, disable player46 | same | 2/2 |
| match4 `47572` | 1 | plant player46 ✓, **disable player57** | no DisableComplete | **label wrong (certain)**: the replay's own result is `Attack won=True winCondition=DefusedBomb`, and the scores agree (R1 Atk 1/Def 1, R2 Def 2). player57 starts the disable at 171 s, stops at 175 s to kill player11, and the bomb goes off. |
| match5 `28328` | 3 | plant player46 ✓, **disable player82** | player94 | **label likely wrong**: our disabler comes from the pawn→slot link (`defuser_timer pawn=b06924f0 playerIndex=4` = player94). The same mechanism gives plant-stop for player59 at 148 s, exactly when player94 kills player59, which confirms the link. player82 gets a kill at 181 s, the second the disable starts. We cannot confirm it from positions, because the timeline needs Y11S2+ (`unsupported version Y11S1_Alpha03`). |

nick's test also asserts `match2.NumRounds() == 4`. We return 5, because we keep the prep-forfeit R05. That round's result belongs to item 07: it is an invented `Time` win for the defense, a 1vX for player39, and one perspective even moves the score (21916: Def 2→3; 26676: Def 2→2).

**F. Version gating: no surprise.** `dissect/version.go` has `Y11S1 int = 9578674`, and nick's build 9540659 is lower. The constant is unused, as is `Y11S2`, so the code paths are gated only on `Y10S4`. Both Y11S1 builds take the same path as Y11S2.

**G. Not a Y11S1 issue:** one player per round has an empty `roleName`. This happens on the user's Y11S2 and Y11S3 matches too, so we leave it out here.

## Approaches considered

1. **Commit the replays as fixtures** (all of them, or match2+match3 at 3.6 MB).
   + Every contributor and CI gets them, with real Y11S1 coverage in the golden tests.
   − Third-party data with no licence, naming about 60 real players. This contradicts the user's policy (only their own small rounds are committed). Once pushed, the data is permanent in the git history.
2. **Commit anonymized replays.** Rewrite usernames and profile IDs.
   − `.rec` is a zstd-chunked binary. Usernames appear in the header, player packets, kill-feed strings and scoreboard. Same-length replacement means decompress, patch, then recompress, and we have no writer for that. Different-length names would shift offsets that the parser's pattern search depends on. The result would no longer be the real file, so it would not be a trustworthy regression input. Too much work for the value.
3. **Local, uncommitted corpus: committed manifest + fetch script + env-gated acceptance test** (chosen).
   + No data enters the repo. The corpus is reproducible from public git objects by blob SHA. It reuses PR #3's env-gated pattern, and it already found 3 bugs.
   − CI does not run it, and it depends on the forks staying up. Mitigation: once fetched, the blobs live in any local mirror; the manifest pins SHAs.
4. **Ask the user for their own Y11S1 replays** and commit 1–2 small rounds under the existing policy.
   + Clean licence. Real Y11S1 coverage in CI.
   − Depends on the user still having Y11S1 replays (the season ended in June 2026). It does not replace 3: the bugs above need a leaver match and a no-event round, which the user may not have.

## Decision and rationale

**Do 3 now. Offer 4 as a non-blocking request.**

- **D1. No fork replay data is committed.** Reason: licence and privacy (third-party usernames and UUIDs). Anonymizing is impractical (approach 2).
- **D2. The manifest is committed at `scripts/corpus/y11s1-forks.tsv`.** It holds blob SHAs, build, source ref and source path, plus the regrouped `<source>-<MatchID>/<file>` target. Only `Match-<date>-<id>` file names appear, no usernames.
- **D3. Fetch reads from a git object store, not the network.** `scripts/corpus/fetch.sh <git-dir> <out-dir>` uses `git cat-file blob <sha>` and verifies with `git hash-object`. It refuses an `<out-dir>` inside the repo. The README documents how to build that object store (`git init --bare` + `git fetch` of the two fork branches). Reason: the script never runs fork code and never touches GitHub during tests. Pinning by SHA makes force-pushes in the forks harmless.
- **D4. The match-level invariants go in a new test file, `dissect/test/acceptance_match_test.go`, gated on `R6_DISSECT_CORPUS`** (a root directory; each subdirectory holding `*.rec` is one match). Reason: PR #3's `TestAcceptance_Replays` covers round-level event invariants and takes a list of folders. Bugs B–D are match-level (JSON shape, cross-round score, determinism), so they need `MatchReader`. Keeping a separate file avoids a conflict with PR #3. A root directory suits a 16-folder corpus.
- **D5. Invariants (each with a reason):**
  - **I1:** `MatchReader.Read()` is `dissect.Ok`.
  - **I2:** in `json.Marshal(m.Data())`, every round's `matchFeedback` and `stats` is a JSON array, never `null`. This is the R6Stats contract. Red today: match2 and match3.
  - **I3:** two independent reads give byte-identical JSON. Red today: match4.
  - **I4:** no `Operator(`, `Map(` or `GameMode(` names appear in the output (catches missing IDs).
  - **I5:** each player's cumulative `stats[].score` never goes down from one round to the next round they appear in. Rounds where every score is 0 are skipped: no scoreboard was recorded (the prep-forfeit round; item 07 territory). Red today: match4 (player57 420 → 0). Also red on match3/21916 R05 (`player38 930 → 0`): in that forfeited round, the player who left has no scoreboard row in the other player's recording. That is item 07 territory, and it is a real consumer-visible zero, so we keep it. Clean on all of the user's Y11S2 and Y11S3 matches.
  - We **reject** a "killer still alive" invariant. It fires on the user's own Y11S2 and Y11S3 matches too (gadget or same-second kills), so it is too noisy.
- **D6. We do not encode nick's labels.** Committing them would put third-party usernames into our tests. Two of the 24 are wrong, and the 22 correct ones match our current output, so they add nothing over I1–I5. The comparison is recorded in this document instead.
- **D7. The test reports bugs; it does not fix them.** B, C and D are filed as their own issues. Until they land, `TestAcceptance_Matches` stays red on this corpus, by design, the way an acceptance check should. Each sub-test is named after its match folder, so a fix turns exactly its rows green.
- **D8. Request to the user (non-blocking):** "Do you still have any Y11S1 (Mar–Jun 2026) match replays of your own? One or two small rounds would become committed `valid/Y11S1/` fixtures."

## Interactions

- **PR #3 (`fork/health-dbno`)** adds `dissect/test/acceptance_test.go` (`TestAcceptance_Replays`, `R6_DISSECT_REPLAY_DIRS`). We reuse its pattern and add a new file, so there is no textual conflict. The README task documents both variables. PR #3's own output on this corpus is unchanged for B, C and D (we checked with a c608cf8 build: the same `null`, the same player57 0 scores, the same defuser results).
- **PR #12 (ElyasYassin, deterministic reconcile)** should turn I3 green for match4 by making the reconcile deterministic. I5 will stay red until the mapping is fixed, because a deterministic but wrong map is still wrong. Not built here: external contributor code.
- **Item 07 (abandoned round winner):** match2 and match3 R05 is a second real no-result round. Note for 07: one perspective (21916) *does* move the defense score 2→3 while the other (26676) does not, so 07's `headerWinner` will disagree between the two recordings of the same round.
- **Items 08 and 09 (late joiner row, scoreboard entity link):** bug C is the leaver mirror of 08's late joiner, and it is in 09's `scoreboardIDToPlayer` territory. Item 08's plan already notes that index-keyed state shifts. The fix (prefer the `sbID+4` DissectID link over the positional zip) should be owned by 09 or by a new issue. This item only supplies the failing match.
- **Item 02 (Y11S2 9734089 markers):** no replay of 9734089 exists in any fork, so item 02 cannot be validated with fork data. The user would have to supply one.
- **JSON / R6Stats consumer:** this item changes no output. Bug B's fix (`[]` instead of `null`) is a consumer-visible improvement. R6Stats could also accept `null` defensively in `parser_contract.py`; that would be a separate R6Stats issue.
- **Golden-test churn:** none. No fixtures change.

## Implementation plan

Base: `origin/main` (bd6ccbe), or main after PR #3 lands. Neither choice changes this plan. Work in a scratch clone. Go: `export PATH=$HOME/.local/go/bin:$PATH`.

### Task 1: Manifest

**Files:** create `scripts/corpus/y11s1-forks.tsv`.

- [ ] **Step 1:** Write the file. It is tab-separated with no header; the columns are `blob_sha  build  source_ref  source_path  target_path`. The content is the 46 lines in **Appendix A**, verbatim. To regenerate it from a mirror holding both refs:

  ```bash
  M=<mirror>; { for p in $(git -C $M ls-tree -r --name-only nicktstewart/main dissect/test/data/replays/defuser_detection); do echo "nicktstewart/main $p"; done
    git -C $M ls-tree -r --name-only ArnaudCabello/main Matchreplay | grep '\.rec$' | sed 's#^#ArnaudCabello/main #'; } |
  while read ref p; do b=$(git -C $M rev-parse "$ref:$p"); f=$(basename $p); id=${f%-R*.rec}
    src=$([[ $ref == nick* ]] && echo nick || echo arnaud); build=$([[ $src == nick ]] && echo 9540659 || echo 9578674)
    printf '%s\t%s\t%s\t%s\t%s\n' "$b" "$build" "$ref" "$p" "$src-$id/$f"; done
  ```
- [ ] **Step 2:** Check it: `wc -l scripts/corpus/y11s1-forks.tsv` should print `46`. `cut -f5 scripts/corpus/y11s1-forks.tsv | cut -d/ -f1 | sort -u | wc -l` should print `16`. `grep -c -i -E 'mori|oreo|littlest' scripts/corpus/y11s1-forks.tsv` should print `0` (no usernames).

### Task 2: Fetch script

**Files:** create `scripts/corpus/fetch.sh` (executable) and `scripts/corpus/README.md`.

- [ ] **Step 1:** Write `fetch.sh`:

  ```bash
  #!/usr/bin/env bash
  # Rebuild a local replay corpus from git blobs listed in a manifest.
  # usage: fetch.sh <git-dir> <out-dir> [manifest]
  # The corpus is third-party data: never write it inside this repository.
  set -euo pipefail
  gitdir=${1:?usage: fetch.sh <git-dir> <out-dir> [manifest]}
  out=${2:?usage: fetch.sh <git-dir> <out-dir> [manifest]}
  here=$(cd "$(dirname "$0")" && pwd)
  manifest=${3:-$here/y11s1-forks.tsv}
  repo=$(git -C "$here" rev-parse --show-toplevel)
  out=$(realpath -m "$out")
  case "$out/" in "$repo"/*) echo "fetch.sh: refusing to write inside the repository ($out)" >&2; exit 2;; esac
  mkdir -p "$out"
  n=0; missing=0
  while IFS=$'\t' read -r sha _build ref path target; do
    [ -z "$sha" ] && continue
    if ! git -C "$gitdir" cat-file -e "$sha^{blob}" 2>/dev/null; then
      echo "missing $sha ($ref:$path)" >&2; missing=$((missing+1)); continue
    fi
    mkdir -p "$out/$(dirname "$target")"
    git -C "$gitdir" cat-file blob "$sha" > "$out/$target"
    got=$(git hash-object "$out/$target")
    [ "$got" = "$sha" ] || { echo "hash mismatch $target: $got" >&2; exit 1; }
    n=$((n+1))
  done < "$manifest"
  echo "fetched $n files into $out ($missing missing)"
  [ "$missing" -eq 0 ]
  ```
- [ ] **Step 2:** Write `scripts/corpus/README.md` with these points: what the corpus is (Y11S1 builds 9540659 and 9578674, from nicktstewart/r6-dissect `0b5f223` and ArnaudCabello/r6-dissect `dfdc96d`); why it is not committed (third-party players' data, no data licence); how to build the object store:

  ```bash
  git init -q --bare ~/r6-corpus-objects.git
  git -C ~/r6-corpus-objects.git fetch -q https://github.com/nicktstewart/r6-dissect.git main:refs/remotes/nicktstewart/main
  git -C ~/r6-corpus-objects.git fetch -q https://github.com/ArnaudCabello/r6-dissect.git main:refs/remotes/ArnaudCabello/main
  scripts/corpus/fetch.sh ~/r6-corpus-objects.git ~/r6-corpus/y11s1
  R6_DISSECT_CORPUS=~/r6-corpus/y11s1 go test ./dissect/test -run TestAcceptance_Matches -v
  # with PR #3: R6_DISSECT_REPLAY_DIRS=$(ls -d ~/r6-corpus/y11s1/*/ | tr '\n' ':') go test ./dissect/test -run TestAcceptance_Replays
  ```
  Also note that the user's own matches (any folder of match subfolders) can be passed in the same way.
- [ ] **Step 3: Verify.** Run `scripts/corpus/fetch.sh <mirror> /tmp/.../r6-corpus-y11s1`. Expected: `fetched 46 files into … (0 missing)`, exit 0. Running `scripts/corpus/fetch.sh <mirror> ./inside` from the repo root should print `refusing to write inside the repository` and exit 2. No `inside/` directory is created. Run `shellcheck scripts/corpus/fetch.sh` if it is installed: no findings. *Prototype result: all three as stated.*

### Task 3: Match-level acceptance test (TDD: it must be red on the corpus first)

**Files:** create `dissect/test/acceptance_match_test.go`.

- [ ] **Step 1:** Write the test:

  ```go
  package test

  import (
  	"bytes"
  	"encoding/json"
  	"os"
  	"path/filepath"
  	"sort"
  	"strings"
  	"testing"

  	"github.com/redraskal/r6-dissect/dissect"
  )

  // TestAcceptance_Matches checks match-level invariants of the JSON output on real match folders
  // that are not committed (third-party data, or too large). Set R6_DISSECT_CORPUS to a directory
  // whose subdirectories each hold the .rec files of one match (see scripts/corpus/README.md).
  func TestAcceptance_Matches(t *testing.T) {
  	root := os.Getenv("R6_DISSECT_CORPUS")
  	if root == "" {
  		t.Skip("R6_DISSECT_CORPUS not set")
  	}
  	matches := corpusMatchDirs(t, root)
  	if len(matches) == 0 {
  		t.Fatalf("no match folders with .rec files under %s", root)
  	}
  	for _, dir := range matches {
  		dir := dir
  		t.Run(filepath.Base(dir), func(t *testing.T) {
  			first := matchJSON(t, dir)
  			checkMatchJSON(t, first)
  			if second := matchJSON(t, dir); !bytes.Equal(first, second) {
  				t.Errorf("output differs between two reads of the same match (nondeterministic)")
  			}
  		})
  	}
  }

  func corpusMatchDirs(t *testing.T, root string) []string {
  	t.Helper()
  	entries, err := os.ReadDir(root)
  	if err != nil {
  		t.Fatal(err)
  	}
  	var dirs []string
  	for _, e := range entries {
  		if !e.IsDir() {
  			continue
  		}
  		d := filepath.Join(root, e.Name())
  		if recs, _ := filepath.Glob(filepath.Join(d, "*.rec")); len(recs) > 0 {
  			dirs = append(dirs, d)
  		}
  	}
  	sort.Strings(dirs)
  	return dirs
  }

  func matchJSON(t *testing.T, dir string) []byte {
  	t.Helper()
  	f, err := os.Open(dir)
  	if err != nil {
  		t.Fatal(err)
  	}
  	defer f.Close()
  	m, err := dissect.NewMatchReader(f)
  	if err != nil {
  		t.Fatalf("NewMatchReader: %v", err)
  	}
  	if err := m.Read(); !dissect.Ok(err) {
  		t.Fatalf("Read: %v", err)
  	}
  	b, err := json.Marshal(m.Data())
  	if err != nil {
  		t.Fatal(err)
  	}
  	return b
  }

  func checkMatchJSON(t *testing.T, b []byte) {
  	t.Helper()
  	for _, unknown := range []string{`"Operator(`, `"Map(`, `"GameMode(`} {
  		if bytes.Contains(b, []byte(unknown)) {
  			t.Errorf("output names an unknown ID: %s...", unknown)
  		}
  	}
  	var out struct {
  		Rounds []map[string]json.RawMessage `json:"rounds"`
  	}
  	if err := json.Unmarshal(b, &out); err != nil {
  		t.Fatal(err)
  	}
  	prev := map[string]int{}
  	for i, r := range out.Rounds {
  		for _, key := range []string{"matchFeedback", "stats"} {
  			if v := strings.TrimSpace(string(r[key])); !strings.HasPrefix(v, "[") {
  				t.Errorf("round %d: %q is %s, want a JSON array", i, key, v)
  			}
  		}
  		var stats []struct {
  			Username string `json:"username"`
  			Score    int    `json:"score"`
  		}
  		_ = json.Unmarshal(r["stats"], &stats)
  		allZero := true
  		for _, s := range stats {
  			if s.Score != 0 {
  				allZero = false
  			}
  		}
  		if allZero { // no scoreboard recorded (e.g. a round forfeited in prep)
  			continue
  		}
  		cur := map[string]int{}
  		for _, s := range stats {
  			cur[s.Username] = s.Score
  			if p, ok := prev[s.Username]; ok && s.Score < p {
  				t.Errorf("round %d: %s cumulative score went down %d -> %d", i, s.Username, p, s.Score)
  			}
  		}
  		prev = cur
  	}
  }
  ```
- [ ] **Step 2: Skip path.** `go test ./dissect/test -run TestAcceptance_Matches -v` should print `--- SKIP: TestAcceptance_Matches … R6_DISSECT_CORPUS not set`. The plain `go test ./...` must stay green, so CI is unaffected.
- [ ] **Step 3: Red on the Y11S1 corpus (this is the point).** Run `R6_DISSECT_CORPUS=<out-dir from Task 2> go test ./dissect/test -run TestAcceptance_Matches -count=1 2>&1 | grep -E '^\s+(--- FAIL|acceptance_match_test)'`. Expected failures, and only these:
  - `nick-Match-2026-03-08_19-55-32-21916` and `nick-Match-2026-03-08_19-55-32-26676`: `round 4: "matchFeedback" is null, want a JSON array` (bug B). 21916 also shows `round 4: player38 cumulative score went down 930 -> 0` (forfeited round, item 07; see D5).
  - `nick-Match-2026-03-09_17-13-54-47572`: `round 1: player57 cumulative score went down 420 -> 0`, plus later rounds (bug C); and `output differs between two reads` (bug D; nondeterministic, so it is very likely but not certain in any one run)
  - The other 13 subtests PASS.
  - *Prototype result (we ran this exact test at bd6ccbe on 2026-10-21):* exactly these 3 FAIL, with exactly these messages; 13 PASS; 81 s.
- [ ] **Step 4: Green on the user's own matches.** Run `R6_DISSECT_CORPUS=<scratch>/replays go test ./dissect/test -run TestAcceptance_Matches -count=1`, then the same with `.../replays-new`. Expected: PASS. (The `.zip` files next to the folders are ignored because they are not directories.) If something here is red, record it as a new finding. Do not loosen the invariant. *Prototype result:* PASS on all 5 (2 Y11S2 + 3 Y11S3), 20 s + 43 s.
- [ ] **Step 5:** `gofmt -l dissect/test` and `go vet ./dissect/test` should both be clean.

### Task 4: Docs

**Files:** modify `README.md` (testing section if one exists, otherwise append "Acceptance on real replays") and `scripts/README.md`.

- [ ] **Step 1:** In `README.md`, add 3 lines naming `R6_DISSECT_CORPUS` (match-level, this test) and `R6_DISSECT_REPLAY_DIRS` (round-level, PR #3, if it has landed), pointing to `scripts/corpus/README.md`.
- [ ] **Step 2:** In `scripts/README.md`, add a one-line entry for `corpus/fetch.sh`.

### Task 5: Hand-off record

- [ ] In the PR body, list the three expected red rows from Task 3 Step 3, each linked to its own issue (bugs B, C, D; see the triage candidates). State that `TestAcceptance_Matches` is expected to stay red on this corpus until those land.

## Acceptance

- `go test ./...` with no env set: green, and `TestAcceptance_Matches` is SKIPped.
- `scripts/corpus/fetch.sh <mirror> <dir>` prints `fetched 46 files … (0 missing)`, and every file's `git hash-object` equals its manifest SHA.
- `R6_DISSECT_CORPUS=<dir> go test ./dissect/test -run TestAcceptance_Matches`: exactly the 3 match folders listed in Task 3 Step 3 fail, with those messages. All the user's Y11S2 and Y11S3 match folders pass.
- `git grep -i -E 'Mori\.CU|player46|player86'` on the branch returns nothing. No replay data and no third-party usernames are committed.

## Risks / open questions

- **Fork availability.** If nicktstewart or ArnaudCabello delete or force-push their repos, a fresh object store cannot be built from GitHub. Existing local mirrors keep working (pinned SHAs). Mitigation: keep one bare mirror on the user's machine; it is not in the repo.
- **I3 is probabilistic.** Two reads can agree by chance. Over 10 runs on match4, all 10 differed, so the false-pass risk is negligible. If needed, read 3 times.
- **I5 could fire on legitimate score drops.** We have not seen one in 46 Y11S1 rounds or in any of the user's Y11S2 and Y11S3 matches. Possible sources: score penalties for teamkills, or an overtime reset. If one appears, narrow the check to a drop to exactly 0 and document why.
- **The match5 R4 disabler (player94 vs nick's player82) is not settled** without positions. The timeline needs Y11S2+. This does not affect this plan (D6), but it would matter if someone later turns nick's labels into fixtures.
- **Open question for the user (D8):** do you have your own Y11S1 replays? If so, a follow-up issue adds `valid/Y11S1/` golden fixtures.

## Appendix A: `scripts/corpus/y11s1-forks.tsv` (tab-separated)

```
90c5ad765337bf6826f8d3e0d8c2af234dc85e44	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match2/Match-2026-03-08_19-55-32-26676-R01.rec	nick-Match-2026-03-08_19-55-32-26676/Match-2026-03-08_19-55-32-26676-R01.rec
00d4ae609cce9d79f9ada48a6077770fdf6f0a81	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match2/Match-2026-03-08_19-55-32-26676-R02.rec	nick-Match-2026-03-08_19-55-32-26676/Match-2026-03-08_19-55-32-26676-R02.rec
646a9c4a699cd4f7c80f009390e43aff1a8b1bba	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match2/Match-2026-03-08_19-55-32-26676-R03.rec	nick-Match-2026-03-08_19-55-32-26676/Match-2026-03-08_19-55-32-26676-R03.rec
388008996b519f6383733a7e564f02d096beb65f	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match2/Match-2026-03-08_19-55-32-26676-R04.rec	nick-Match-2026-03-08_19-55-32-26676/Match-2026-03-08_19-55-32-26676-R04.rec
e1ee37ca6efff89b86b7e35a18b2ef832a08c464	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match2/Match-2026-03-08_19-55-32-26676-R05.rec	nick-Match-2026-03-08_19-55-32-26676/Match-2026-03-08_19-55-32-26676-R05.rec
167512964fae07a28123e53c069498b16c00d547	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match3/Match-2026-03-08_19-55-32-21916-R01.rec	nick-Match-2026-03-08_19-55-32-21916/Match-2026-03-08_19-55-32-21916-R01.rec
6214b6c49a0a70f60efc4914f9892ed43682c390	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match3/Match-2026-03-08_19-55-32-21916-R02.rec	nick-Match-2026-03-08_19-55-32-21916/Match-2026-03-08_19-55-32-21916-R02.rec
426411c29d47966cb134604c7e2add33d8af525b	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match3/Match-2026-03-08_19-55-32-21916-R03.rec	nick-Match-2026-03-08_19-55-32-21916/Match-2026-03-08_19-55-32-21916-R03.rec
28949bec75cf7dc5278bad57938549bdfb11a909	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match3/Match-2026-03-08_19-55-32-21916-R04.rec	nick-Match-2026-03-08_19-55-32-21916/Match-2026-03-08_19-55-32-21916-R04.rec
aa9644d0b7158a5d34880c5e0d71fde42281e794	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match3/Match-2026-03-08_19-55-32-21916-R05.rec	nick-Match-2026-03-08_19-55-32-21916/Match-2026-03-08_19-55-32-21916-R05.rec
0b1ea8cdf00021e9bd461a09ec2274d89c63dcfc	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match4/Match-2026-03-09_17-13-54-47572-R01.rec	nick-Match-2026-03-09_17-13-54-47572/Match-2026-03-09_17-13-54-47572-R01.rec
cc0f7e04a23486291c920f5085f12f5ef7313de5	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match4/Match-2026-03-09_17-13-54-47572-R02.rec	nick-Match-2026-03-09_17-13-54-47572/Match-2026-03-09_17-13-54-47572-R02.rec
51b8f63c4d7069bb7747ee6553358f47a762cc57	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match4/Match-2026-03-09_17-13-54-47572-R03.rec	nick-Match-2026-03-09_17-13-54-47572/Match-2026-03-09_17-13-54-47572-R03.rec
5bb60ef3f7f338e92edca0a032d7df7bce67a566	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match4/Match-2026-03-09_17-13-54-47572-R04.rec	nick-Match-2026-03-09_17-13-54-47572/Match-2026-03-09_17-13-54-47572-R04.rec
fef5472bd29588329e8efc89910f52882129774a	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match4/Match-2026-03-09_17-13-54-47572-R05.rec	nick-Match-2026-03-09_17-13-54-47572/Match-2026-03-09_17-13-54-47572-R05.rec
585da0b163b9d825c7bee1e957ca8f7fa31e5229	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match5/Match-2026-03-07_17-53-32-28328-R01.rec	nick-Match-2026-03-07_17-53-32-28328/Match-2026-03-07_17-53-32-28328-R01.rec
29319cc656a336424bcde8b0c27ee36517413fc1	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match5/Match-2026-03-07_17-53-32-28328-R02.rec	nick-Match-2026-03-07_17-53-32-28328/Match-2026-03-07_17-53-32-28328-R02.rec
8fd99e1024d785a887d69b06fe2a4ccdf4ed8a62	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match5/Match-2026-03-07_17-53-32-28328-R03.rec	nick-Match-2026-03-07_17-53-32-28328/Match-2026-03-07_17-53-32-28328-R03.rec
b493bb15d5762916ffabfca2c441a956ecad5407	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match5/Match-2026-03-07_17-53-32-28328-R04.rec	nick-Match-2026-03-07_17-53-32-28328/Match-2026-03-07_17-53-32-28328-R04.rec
47b47d8ca1bc5bc65ffb590a75cc6b063b3c197f	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match6/Match-2026-03-03_13-44-52-35568-R03.rec	nick-Match-2026-03-03_13-44-52-35568/Match-2026-03-03_13-44-52-35568-R03.rec
87229b8cac236a76e602c85c7c6ed02bf8b87c23	9540659	nicktstewart/main	dissect/test/data/replays/defuser_detection/match7/Match-2026-03-03_13-24-27-35568-R03.rec	nick-Match-2026-03-03_13-24-27-35568/Match-2026-03-03_13-24-27-35568-R03.rec
f98e047fb24b7539c3de638cddec6c9fc5997b06	9578674	ArnaudCabello/main	Matchreplay/Clubhouse/Match-2026-03-29_15-42-18-22800/Match-2026-03-29_15-42-18-22800-R01.rec	arnaud-Match-2026-03-29_15-42-18-22800/Match-2026-03-29_15-42-18-22800-R01.rec
866a927084e6e47db9c344b9a529cc568207deb1	9578674	ArnaudCabello/main	Matchreplay/Clubhouse/Match-2026-03-29_15-42-18-22800/Match-2026-03-29_15-42-18-22800-R02.rec	arnaud-Match-2026-03-29_15-42-18-22800/Match-2026-03-29_15-42-18-22800-R02.rec
6b6e61ffb22de3114f2986bb2fed5677f83e1740	9578674	ArnaudCabello/main	Matchreplay/Clubhouse/Match-2026-03-29_15-42-18-22800/Match-2026-03-29_15-42-18-22800-R03.rec	arnaud-Match-2026-03-29_15-42-18-22800/Match-2026-03-29_15-42-18-22800-R03.rec
197bc20811891c476b654884dabcceca576becc7	9578674	ArnaudCabello/main	Matchreplay/Clubhouse/Match-2026-03-29_15-42-18-22800/Match-2026-03-29_15-42-18-22800-R04.rec	arnaud-Match-2026-03-29_15-42-18-22800/Match-2026-03-29_15-42-18-22800-R04.rec
1e76d1974a1381113d22af5ea1e1b9c01894f492	9578674	ArnaudCabello/main	Matchreplay/Clubhouse/Match-2026-03-29_15-42-18-22800/Match-2026-03-29_15-42-18-22800-R05.rec	arnaud-Match-2026-03-29_15-42-18-22800/Match-2026-03-29_15-42-18-22800-R05.rec
9df837cbd29193ed5fb5c2d2815eb50df2ba91ec	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R01.rec	arnaud-Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R01.rec
b9efd1d6ffd76e772781677a171bc66c70d55d90	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R02.rec	arnaud-Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R02.rec
ef726f78bbc2c04728d79eb0bb6ce4542a9361c1	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R03.rec	arnaud-Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R03.rec
d2ce1e533f2cf4ff58587b083a333d3fa7708aba	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R04.rec	arnaud-Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R04.rec
aa0f35b056b39cd40051d8fe42ebc386e29796ae	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R05.rec	arnaud-Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R05.rec
310b3dfea621001e5402e4af1d64aa5b1c298097	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R06.rec	arnaud-Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R06.rec
cf15d505d4b746567eba1451869475328a785e95	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R07.rec	arnaud-Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R07.rec
2da87a9cd60e7b5412d43b290546afbd461acd68	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R08.rec	arnaud-Match-2026-03-28_22-27-37-16316/Match-2026-03-28_22-27-37-16316-R08.rec
7ed107ea66bbd10ac572ee7a2d0b70c7cb859260	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-29_09-31-02-16316-R05.rec	arnaud-Match-2026-03-29_09-31-02-16316/Match-2026-03-29_09-31-02-16316-R05.rec
c366f15121e305a2861b2b4f7eb8a4a48b070b1d	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-29_16-04-35-6900-R04.rec	arnaud-Match-2026-03-29_16-04-35-6900/Match-2026-03-29_16-04-35-6900-R04.rec
1a3950155e5271b341d81cb97b7799aacf5434f0	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-29_16-24-26-6900-R05.rec	arnaud-Match-2026-03-29_16-24-26-6900/Match-2026-03-29_16-24-26-6900-R05.rec
d96976e7b9968fcc7aa9a05a79c43688fdd67369	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-29_17-32-21-6900-R01.rec	arnaud-Match-2026-03-29_17-32-21-6900/Match-2026-03-29_17-32-21-6900-R01.rec
1c8b2bf4e56028d670109ea646400ce88573913a	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-29_17-32-21-6900-R02.rec	arnaud-Match-2026-03-29_17-32-21-6900/Match-2026-03-29_17-32-21-6900-R02.rec
0856d6294a710cd91b1fc3de47635b0305f3ad19	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-29_17-32-21-6900-R03.rec	arnaud-Match-2026-03-29_17-32-21-6900/Match-2026-03-29_17-32-21-6900-R03.rec
cd78174f7acd41247797353c8608355cdd937e2c	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-29_17-32-21-6900-R04.rec	arnaud-Match-2026-03-29_17-32-21-6900/Match-2026-03-29_17-32-21-6900-R04.rec
104e9fd5d34d2d497a7a22e376caa13240f0f232	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-30_11-46-24-20744-R01.rec	arnaud-Match-2026-03-30_11-46-24-20744/Match-2026-03-30_11-46-24-20744-R01.rec
5555b7fa452451ca58356e7808b900f8b83c2978	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-30_12-50-35-20744-R01.rec	arnaud-Match-2026-03-30_12-50-35-20744/Match-2026-03-30_12-50-35-20744-R01.rec
a888e05f84389f3ac1bdc9e903bedfc3e587c5f9	9578674	ArnaudCabello/main	Matchreplay/Match-2026-03-29_17-32-21-6900/Match-2026-03-30_13-07-17-20744-R01.rec	arnaud-Match-2026-03-30_13-07-17-20744/Match-2026-03-30_13-07-17-20744-R01.rec
883c7abe9ce229ff10b15435f73d7abd14c44b60	9578674	ArnaudCabello/main	Matchreplay/Traces/Club/Match-2026-03-30_16-31-20-33512/Match-2026-03-30_16-31-20-33512-R01.rec	arnaud-Match-2026-03-30_16-31-20-33512/Match-2026-03-30_16-31-20-33512-R01.rec
c9d7784c65795311a7ca21d16a699c48b95e1eda	9578674	ArnaudCabello/main	Matchreplay/Traces/Club/Match-2026-03-30_16-31-20-33512/Match-2026-03-30_16-31-20-33512-R02.rec	arnaud-Match-2026-03-30_16-31-20-33512/Match-2026-03-30_16-31-20-33512-R02.rec
```
