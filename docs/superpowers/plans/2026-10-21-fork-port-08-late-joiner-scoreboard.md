# Fork port 08: late joiner gets no scoreboard row — Design + Implementation Plan

> **Issue:** #18 · prestonwillis10/r6-dissect · fork survey: `docs/research/2026-10-21-fork-survey.md`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A player first read after `deriveTeamRoles` ran (late joiner, substitute, or a header-listed player whose player packet comes after the 10th one) keeps the score and assists the scoreboard packets credit to them.

**Architecture:** Scoreboard rows stop being created only once, in `deriveTeamRoles`. One accessor, `(*Reader).scoreboardRow(idx)`, grows `Scoreboard.Players` on demand so it stays parallel to `Header.Players`; the four score/assist write sites use it, and `deriveTeamRoles` filters existing rows in lockstep with the players it drops.

**Tech Stack:** Go (module `github.com/redraskal/r6-dissect`), `go test`.

**Spec:** this document (design sections below are the spec).

Verified against `origin/main` = `bd6ccbeb7b46af99638df5d650187069e849f925`.

## Global Constraints

- Fork code (Animuxs) is reference only; nothing is copied verbatim, nothing from it is built or run.
- No JSON schema change: `rounds[].stats[].score` / `assists` keep their names and types.
- `Reader.Scoreboard` / `ScoreboardPlayer` stay exported with the same fields (Go API compatibility).
- Golden replay JSON (`dissect/test/data/replays/valid/**.rec.json`) must not change (it does not serialize stats).
- New test file name: `dissect/scoreboard_latejoiner_test.go` (PR #3 adds `dissect/stats_test.go`, PR #12 adds `dissect/scoreboard_test.go`; a third name avoids add/add conflicts).

## Review Focus

1. Scoreboard packet for a player *before* `deriveTeamRoles` runs, followed by `deriveTeamRoles` dropping an operator-0 header player listed earlier → the score must stay with the right username (Task 2, `TestLateJoiner_DeriveKeepsRowsAligned`).
2. Round where `deriveTeamRoles` never runs (fewer than 10 `readPlayer` calls) → scores must still be kept, not all zero (covered by the first half of the same test: rows are created while `Scoreboard.Players` is nil).
3. Reconnect: same username, new dissect id → still one row, score on it (Task 1, `TestLateJoiner_ReconnectKeepsOneRow`, green today; a regression guard).
4. Y10S4+ path (`scoreboardIDToPlayer` lookup) as well as the pre-Y10S4 dissect-id path (Task 1, `TestLateJoiner_ScoreKeptY10S4`).
5. Assists path, which also bumps `AssistsFromRound` (Task 1, `TestLateJoiner_AssistKept`).

---

## Summary

`Scoreboard.Players` is built exactly once per round, inside `deriveTeamRoles`, which `readPlayer` calls when its call counter reaches 10. Any player appended to `Header.Players` after that has no row. Every score/assist write is guarded with `idx < len(r.Scoreboard.Players)`, so their score and assists are silently dropped and `PlayerStats` reports 0 for them. Our main already has the crash guard Animuxs added (`PlayerStats` no longer panics); what is missing is the row. Fix: create rows on demand.

## Source

- Fork: `Animuxs/main`, commit `e8b0eb5f463f059be999ef814d554f63dbd36fd0` ("Fixes from testing 1.7.0 beta 2"), files `dissect/player.go`, `dissect/scoreboard.go`, `dissect/stats.go`, `dissect/stats_test.go` (the commit also touches unrelated `scripts/*.py`).
- What they did: (1) in `readPlayer`, when a new username is appended, also append `ScoreboardPlayer{ID: p.DissectID}` if `len(r.Scoreboard.Players) == len(r.Header.Players) && len(r.Scoreboard.Players) > 0`; (2) `idx < len(r.Scoreboard.Players)` guards in `readScoreboardAssists` / `readScoreboardScore`; (3) the same guard in `Reader.PlayerStats`; (4) `TestPlayerStatsWithALatePlayer`. They hit `index out of range [10] with length 10` on a real 11-player match.
- Flaws for us:
  - (2) and (3) are already on our main (commit `1ee558e`), so their test passes on bd6ccbe; it asserts the late player's score is **0**, i.e. it pins the data loss rather than fixing it.
  - (1) couples the two slices through a length-equality check at one append site. It does nothing when `deriveTeamRoles` has not run yet (rows empty) and nothing if any other path ever appends to `Header.Players`; it does not cover the Y10S4 write functions specifically (they share the guard but not the append).
  - No test drives a scoreboard packet to the late player.

## Current behaviour (bd6ccbe)

How players and rows are built, per round (each `.rec` is its own `Reader`; `MatchReader.read` calls `NewReader` + `Read` per file):

1. `readHeader` seeds `Header.Players` from the file header (`playerid`/`playername`/`team` props). In the corpus this is often 9 or even 5 players, not 10.
2. `dissect/player.go` `readPlayer` runs per player packet. It increments `r.playersRead` on **every call** (including early returns for empty slots / `swap[0] == 0x9D` duplicates), and `defer func() { if r.playersRead == 10 { r.deriveTeamRoles() } }()`. An existing player is matched by `existing.Username == p.Username || ... bytes.Equal(existing.DissectID, p.DissectID)` and updated in place (including `r.Header.Players[i].DissectID = p.DissectID`); otherwise `r.Header.Players = append(r.Header.Players, p)` with `teamIndex := 0; if r.playersRead > 5 { teamIndex = 1 }`.
3. `dissect/header.go` `deriveTeamRoles` drops players with `p.Operator == 0` (header-listed, not yet read) and, for each kept player, `r.Scoreboard.Players = append(r.Scoreboard.Players, ScoreboardPlayer{ ID: p.DissectID, })`. This is the only place rows are created.
4. `dissect/scoreboard.go` writes: `readScoreboardScore`, `readScoreboardScoreY10S4`, `readScoreboardAssists`, `readScoreboardAssistsY10S4` all resolve `idx` into `Header.Players` and then `if idx < len(r.Scoreboard.Players) { r.Scoreboard.Players[idx].Score = score }` (assists alike). A late player resolves to `idx == 10` and the write is skipped.
5. `dissect/stats.go` `Reader.PlayerStats`: `var scorePlayer ScoreboardPlayer; if i < len(r.Scoreboard.Players) { scorePlayer = r.Scoreboard.Players[i] }` → late player gets `Score: 0, Assists: 0`.
6. Across the match, `MatchReader.PlayerStats` aggregates `r.PlayerStats()` by `p.Username` (`index[p.Username]`); it sums kills/deaths/assists/headshots and does not carry score. A reconnect under the same username therefore aggregates into one match row already; a late joiner gets a match row counting only the rounds they appear in.

Misattribution: none found. `deriveTeamRoles` filters players and rows together, and nothing else reorders `Header.Players`, so rows never shift onto the wrong player; the failure is loss only.

Reproduction (ran at bd6ccbe in a scratch clone, test file from Task 1):

```
$ go test ./dissect -run TestLateJoiner -count=1 -v
{"level":"debug","score":1234,"username":"late",...,"message":"scoreboard_score"}
    scoreboard_latejoiner_test.go:49: late player's score: got 0, want 1234
--- FAIL: TestLateJoiner_ScoreKept
    scoreboard_latejoiner_test.go:61: late player's assists: got 0, want 1
--- FAIL: TestLateJoiner_AssistKept
    scoreboard_latejoiner_test.go:77: late player's score: got 0, want 777
--- FAIL: TestLateJoiner_ScoreKeptY10S4
    scoreboard_latejoiner_test.go:102: a2: score 0, want 500
--- FAIL: TestLateJoiner_DeriveKeepsRowsAligned
--- PASS: TestLateJoiner_ReconnectKeepsOneRow
```

The debug line shows the packet *was* attributed to `late` and then thrown away.

Corpus survey (instrumented scratch build counting every skipped write; 17 match folders, 105 rounds: user Y11S2 ×2, user Y11S3 ×3, main fixtures Y8S1/Y8S2/Y9S1/Y11S2, rec-timeline Y11S3, ArnaudCabello `Matchreplay/`, nicktstewart `defuser_detection/`, Animuxs-lab `LIQ_MAG`):

- No round ends with more than 10 players; every round has `len(Header.Players) == len(Scoreboard.Players)`; **zero** skipped score/assist writes. The bug does not occur in our corpus; it needs a real mid-round join (Animuxs had one).
- Distinct usernames per match: 10 in every real match folder (user matches: 10/10/10/10/10). Folders that showed more (ArnaudCabello `Match-2026-03-29_17-32-21-6900` 27, Y8S1 23, Y8S2 29, Y11S3 15) contain several different matches in one directory, not late joiners.
- 4v5 rounds exist (nicktstewart match4 R02–R05, match5 R03: 9 players, 9 rows) and are handled.
- Re-reads exist: nicktstewart match5 R02 has 12 `readPlayer` completions for 10 players (`player23`, `player72` read again after `deriveTeamRoles`); both are matched by username, no new row needed. Y8S1 `ranked_2` has 11 calls for 10 players.
- Players missing from the file header are common (`player82`, `player48`, `player54`, five `*.SOOS` in `ranked_2`); in every case their packet arrived before the 10th call, which is the only reason they got a row.

## Approaches considered

A. **Port Animuxs: append a row in `readPlayer`** when a new player is appended and the lengths are equal. Smallest diff. Keeps a hidden invariant enforced at one call site; no-op when rows do not exist yet; does not help a round in which `deriveTeamRoles` never runs.

B. **On-demand rows (`scoreboardRow(idx)`)**, plus `deriveTeamRoles` keeping any rows already written, filtered in lockstep. Every write path is covered by construction, independent of when or how a player got into `Header.Players`; also fixes "deriveTeamRoles never ran → all scores lost" for free. Slightly larger: touches `deriveTeamRoles`.

C. **Key scoreboard data by player identity** (move score/assists into unexported `Player` fields or a `map[string]*ScoreboardPlayer`). Removes the parallel-slice invariant entirely. Changes the exported `Scoreboard` shape or makes it a derived view; larger diff that collides with PR #3 (`healthState map[int]`) and item 09, which also key by player index.

## Decision and rationale

**B.** It fixes the loss at the write, not at one producer of players, and costs ~30 lines. A keeps the fragile invariant; C is the right long-term direction but is a refactor across index-keyed maps (`lastPlayerScores`, `scoreboardFinalKills`, `scoreboardIDToPlayer`, PR #3 `healthState`) that should be done once, deliberately, not inside a bug fix.

Decisions (brainstorming questions answered autonomously):

- *Should the late player appear with 0 when no packet arrives?* Yes, unchanged: `PlayerStats` keeps its guard, so a player without a row still gets a stats entry.
- *Reconnect (same username, new id): one row or two?* One. `readPlayer` already matches by username and updates `DissectID` in place, so per round there is one player and one row; across rounds `MatchReader.PlayerStats` aggregates by username. Pinned by `TestLateJoiner_ReconnectKeepsOneRow`. A reconnect under a *different* username is a different player to the parser; R6Stats aggregates by `profileID`, which is the right key there. No change.
- *Fix the late joiner's team index in the same change?* No (see Risks, "08b"). It is a different mechanism (positional `playersRead > 5` team inference) and needs its own test seam.
- *Change the `playersRead == 10` trigger?* No. Out of scope; with on-demand rows the trigger no longer decides who gets a row.
- *Port Animuxs' test?* No; it passes on main and asserts a 0 score. Our tests assert the score is kept.

## Interactions

- **PR #3 `origin/fork/health-dbno`:** adds a `+50` revive block inside `readScoreboardScoreY10S4` right after the score write and adds `dissect/stats_test.go`; `player.go` is identical to main. The prototype diff applies cleanly on it with `git apply --3way` and `go test ./...` passes. Either order works; if PR #3 lands first, rebase is trivial.
- **PR #10 `origin/track/rec-timeline`:** touches `scoreboard.go` (one `derived = true` line in the defuser block) and `player.go` (`addUpdate`). Prototype applies cleanly; `go test ./...` passes (including `cmd/dissect-lab`, `dissect/wire`).
- **PR #12 `ElyasYassin/fix/deterministic-scoreboard-reconcile`:** sorts in `reconcileKillsFromScoreboard` only; no overlap with the write sites; prototype applies cleanly and passes. Independent evidence for PR #12 from this work: the bd6ccbe CLI produced three different JSON outputs in three runs on nicktstewart `match4` (kills moved between players by reconcile), so before/after comparisons on that folder are noise until #12 lands.
- **Item 09 (scoreboard entity link EB219B38 + LCS alignment):** changes *who* `idx` is on Y10S4+ (`scoreboardIDToPlayer`); this item changes *where* the value is stored. On Y10S4+ a late joiner's sbID must also be mapped, which today relies on `ensureScoreboardMapping`'s `sbID[0] + 4` fallback; if it is not mapped the packet is dropped as `scoreboard_kill_unknown_player`/`N/A` before reaching `scoreboardRow`. So 08 is necessary for Y10S4+ late joiners and 09 may be needed too. Land 08 first (small, independent); 09 then gets on-demand rows for free. Expect a textual conflict in `readScoreboard*Y10S4` only.
- **Item 01 (incomplete match):** unrelated code path.
- **JSON / R6Stats:** no schema change. `rounds[].stats[]` for a late joiner gets a real `score` / `assists` instead of 0. R6Stats `services/facts.py` reads `stat.score` / `stat.assists` keyed by username; it starts getting correct values with no change. Match-level `stats[]` has no score field; assists there become correct too.
- **Golden churn:** none. Golden JSON serializes only `Header` and `MatchFeedback`. Verified: `go test ./...` passes unchanged with the prototype, and the CLI JSON (which includes stats) is byte-identical before/after on 16 of 17 corpus folders; the 17th (`match4`) differs run-to-run on the unmodified binary too (PR #12), and none of its differences are in `score` or `assists`.

## Implementation plan

Work in a branch off `main` (e.g. `fix/late-joiner-scoreboard`). `export PATH=$HOME/.local/go/bin:$PATH`. Tasks 1 and 2 ship in **one PR**: Task 1 alone would misalign rows in any round where a scoreboard packet precedes the 10th `readPlayer` call.

### Task 1: Failing tests for a late joiner's score and assists

**Files:**
- Create: `dissect/scoreboard_latejoiner_test.go`

**Interfaces:**
- Produces: `lateJoinerReader(codeVersion int) *Reader` (10 players `a0..a4`, `d0..d4` with `DissectID []byte{i+1,0,0,0}`, `Operator: Recruit`, 10 rows, then `late` with `DissectID {0x0B,0,0,0}` appended to `Header.Players` only; maps `scoreboardIDToPlayer`, `scoreboardInitialKills`, `scoreboardFinalKills`, `lastPlayerScores` made; `pendingDefuserPlantIdx: -1`, `lastDefuserPlayerIndex: -1`; `Header.Teams[0].Won = true`) and `u32(v uint32) []byte` (returns `0x04` + 4 bytes little-endian, the way `Reader.Uint32` reads: it skips a size byte first).

- [ ] **Step 1: Write the tests**

Byte layouts (`Reader.Skip` returns EOF when the offset reaches `len(r.b)`, so every buffer ends with one extra `0` byte):
- pre-Y10S4 score: `r.b = u32(1234) + 13 zero bytes + {0x0B,0,0,0} + {0}`, offset 0, `CodeVersion: Y9S1`.
- pre-Y10S4 assist: `r.b = u32(1) + 30 zero bytes + {0x0B,0,0,0} + {0}`.
- Y10S4 score: `r.b = {0x23} + sbID{0xAA,0xBB,0xCC,0xDD} + 8 zero bytes + u32(777) + {0}`, `r.offset = 13`, `r.scoreboardIDToPlayer[sbID] = 10`, `CodeVersion: Y10S4` (marker is read at end-of-score − 18, sbID at end-of-score − 17).

Tests:
- `TestLateJoiner_ScoreKept`: after `readScoreboardScore(r)`, `stats := r.PlayerStats()`; `len(stats) == 11`, `stats[10].Username == "late"`, `stats[10].Score == 1234`.
- `TestLateJoiner_AssistKept`: after `readScoreboardAssists(r)`, `r.PlayerStats()[10].Assists == 1`.
- `TestLateJoiner_ScoreKeptY10S4`: after `readScoreboardScore(r)`, `r.PlayerStats()[10].Score == 777`.
- `TestLateJoiner_ReconnectKeepsOneRow`: `r.Header.Players = r.Header.Players[:10]`; `r.Header.Players[3].DissectID = {0x44,0,0,0}`; score packet `u32(900)` + 13 zero + `{0x44,0,0,0}` + `{0}`; expect `len(stats) == 10`, `stats[3].Username == "a3"`, `stats[3].Score == 900`.

A working version of this file (all five tests, including Task 2's) is kept at `docs/research/fork-port-prototypes/08/scoreboard_latejoiner_test.go.txt` while the scratchpad lives; the layouts above are the source of truth.

- [ ] **Step 2: Run and confirm red**

Run: `go test ./dissect -run TestLateJoiner -count=1 -v`
Expected: `TestLateJoiner_ScoreKept` FAIL `late player's score: got 0, want 1234`; `TestLateJoiner_AssistKept` FAIL `got 0, want 1`; `TestLateJoiner_ScoreKeptY10S4` FAIL `got 0, want 777`; `TestLateJoiner_ReconnectKeepsOneRow` PASS.

- [ ] **Step 3: Implement `func (r *Reader) scoreboardRow(idx int) *ScoreboardPlayer` in `dissect/scoreboard.go`**

Appends `ScoreboardPlayer{ID: r.Header.Players[n].DissectID}` for each missing `n` until `len(r.Scoreboard.Players) > idx`, returns `&r.Scoreboard.Players[idx]`. Precondition (doc comment): `0 <= idx < len(r.Header.Players)`; all four callers already check it. Replace the four `if idx < len(r.Scoreboard.Players) { ... }` blocks in `readScoreboardAssists`, `readScoreboardAssistsY10S4`, `readScoreboardScore`, `readScoreboardScoreY10S4` with `row := r.scoreboardRow(idx); row.Assists = assists; row.AssistsFromRound++` and `r.scoreboardRow(idx).Score = score`. Leave the `PlayerStats` guard in `dissect/stats.go`, but change its comment `// the scoreboard is only populated once all players were read` to `// a player no scoreboard packet named has no row`.

- [ ] **Step 4: Run and confirm green**

Run: `go test ./dissect -run TestLateJoiner -count=1 -v`
Expected: all four PASS.

- [ ] **Step 5: Commit**

```bash
git add dissect/scoreboard.go dissect/stats.go dissect/scoreboard_latejoiner_test.go
git commit -m "fix(scoreboard): give a late joiner a scoreboard row so their score and assists are kept"
```

### Task 2: Keep rows aligned when deriveTeamRoles runs after rows exist

With on-demand rows, a scoreboard packet before the 10th `readPlayer` call creates rows; `deriveTeamRoles` today would then *append* another set (misaligning everything) and does not drop rows of the operator-0 players it removes.

**Files:**
- Modify: `dissect/header.go` (`func (r *Reader) deriveTeamRoles()`)
- Test: `dissect/scoreboard_latejoiner_test.go`

**Interfaces:**
- Consumes: `scoreboardRow`, `lateJoinerReader`, `u32` from Task 1.

- [ ] **Step 1: Write `TestLateJoiner_DeriveKeepsRowsAligned`**

`r := lateJoinerReader(Y9S1)`; `r.Scoreboard.Players = nil`; `r.Header.Players = append([]Player{{Username: "ghost", DissectID: []byte{0x0C,0,0,0}}}, r.Header.Players[:10]...)` (ghost has `Operator` 0); score packet `u32(500)` + 13 zero + `{0x03,0,0,0}` (a2) + `{0}`; `readScoreboardScore(r)`; `r.deriveTeamRoles()`. Assert `len(r.Scoreboard.Players) == len(r.Header.Players)` (10) and, over `r.PlayerStats()`, `a2` has score 500 and every other player 0.

- [ ] **Step 2: Run and confirm red**

Run: `go test ./dissect -run TestLateJoiner_DeriveKeepsRowsAligned -count=1 -v`
Expected: FAIL (with Task 1 in place: `14 rows for 10 players`; on bd6ccbe: `a2: score 0, want 500`).

- [ ] **Step 3: Change `deriveTeamRoles`**

Build a fresh `rows := make([]ScoreboardPlayer, 0, len(r.Header.Players))` alongside the existing in-place `players` filter; for each kept player at original index `i`, take `r.Scoreboard.Players[i]` if `i < len(r.Scoreboard.Players)`, else `ScoreboardPlayer{ID: p.DissectID}`; after the loop set `r.Scoreboard.Players = rows` next to `r.Header.Players = players`. Remove the old `append(r.Scoreboard.Players, ...)`.

- [ ] **Step 4: Run the whole suite**

Run: `go test ./... -count=1`
Expected: `ok` for `dissect` and `dissect/test` (golden JSON unchanged; do not pass `-update`). `gofmt -l dissect` prints nothing; `go vet ./dissect` clean.

- [ ] **Step 5: Corpus check (CLI JSON, which includes stats)**

Build `go build -o /tmp/r6d-new .` on the branch and `go build -o /tmp/r6d-old .` on main; for each match folder in the user's Y11S2/Y11S3 replays, run `<bin> <folder> -f json -o <out>.json` with both and `cmp` the outputs.
Expected: identical (no corpus round has a late joiner). If a folder differs, diff the JSON: differences only in kill attribution that also vary between two runs of the old binary are reconcile nondeterminism (PR #12), not this change; any `score`/`assists` difference must be explained before merging.

- [ ] **Step 6: Commit**

```bash
git add dissect/header.go dissect/scoreboard_latejoiner_test.go
git commit -m "fix(scoreboard): keep scoreboard rows aligned when deriveTeamRoles drops players"
```

## Acceptance

- `go test ./dissect -run TestLateJoiner -count=1` — red on bd6ccbe (4 of 5 fail), green after Tasks 1–2.
- `go test ./... -count=1` green with no golden file changes.
- CLI JSON byte-identical before/after on the user's five real match folders.

## Risks / open questions

- **No real replay with a late joiner** in our corpus; the fix is proven by unit tests on the exact state `readPlayer` produces. If a real 11-player match turns up (Animuxs had one; ask a CollegiateR6 org for a sub/reconnect replay), add it as a fixture and check the late player's `score`.
- **08b, separate mechanism, not in this issue:** a new player read after the 5th `readPlayer` call is given `teamIndex = 1` (`teamIndex := 0; if r.playersRead > 5 { teamIndex = 1 }` in `readPlayer`). A late joiner on team 0, or a header-listed team-0 player dropped by `deriveTeamRoles` (operator 0) and read again later, lands on team 1; R6Stats would then count their round on the wrong side. Fix shape: after roles are derived, take the team whose `Role` matches `p.Operator.Role()`. Needs a seam to unit-test `readPlayer`'s registration step; recommend filing at /triage once a replay or a byte-level test shows it.
- **Index-keyed state shifts when `deriveTeamRoles` drops a player:** `readPlayerOrder`, `scoreboardIDToPlayer`, `lastPlayerScores`, `scoreboardFinalKills` (and PR #3 `healthState`) hold `Header.Players` indices taken before the filter. Pre-existing, item 09 territory; this plan only makes `Scoreboard.Players` follow the filter.
- **`playersRead == 10` counts calls, not players.** With on-demand rows it no longer decides who gets a row, but it still decides when team roles are derived and which header players are dropped.
- **Y10S4+ late joiner may still be unmapped** (see Interactions, item 09).
