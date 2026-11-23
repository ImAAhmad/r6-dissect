# Fork port 09: link scoreboard entities to players explicitly — Design and Implementation Plan

> **Issue:** #26 · prestonwillis10/r6-dissect · fork survey: `docs/research/2026-10-21-fork-survey.md`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On Y10S4+ replays, each scoreboard entity ID is linked to its player through the reference the replay already contains (`1B <player obj> EB219B38 <scoreboard entity> 18A591A1`), not by zipping first-seen order with `readPlayerOrder`. Kills, assists, score and the +100 defuser bonus then land on the right player when a player has left the match or joined late.

**Architecture:** A new Y10S4+ listener, `readScoreboardEntity`, records `scoreboard entity -> player object handle`. `ensureScoreboardMapping` resolves those handles through the existing `playerIndexByObject` (the loadout code's player-object table), and uses them before anything else. The old zip runs only when the round has no link tokens at all. The kill-feed reconciliation (`reconcileKillsFromScoreboard`) stays as it is. The JSON shape does not change.

**Tech Stack:** Go (`export PATH=$HOME/.local/go/bin:$PATH`), `github.com/go-test/deep` golden tests in `dissect/test`.

**Spec:** this document (design sections first, then the plan).

**Base:** `origin/main` at `bd6ccbeb7b46af99638df5d650187069e849f925`.

## Global Constraints

- Gate on `r.Header.CodeVersion >= Y10S4`, the same gate as the code path that uses the mapping. Do not register the listener on older versions. Y8S1 has tens of thousands of `EB219B38` hits per round, and the pre-Y10S4 scoreboard path reads the player ID directly.
- Parse the token by structure, the same way `readInventoryOwner` and `readDefuserPawn` do: `0x1B` op byte, an 8-byte owner handle, the field, an 8-byte target handle whose high 4 bytes are zero, and class `18 A5 91 A1`. Do not search forward for a username (that is the fork's method; see D2).
- Use `r.playerIndexByObject(owner)` to resolve owners. Do not add a second player-object table.
- No new exported field and no JSON change.
- No dependency on PR #10 (`dissect/wire`). The code must apply on `main` alone.

## Review Focus

1. **A player who left keeps a scoreboard entity.** Their entity has a link token, but its owner object has no player packet. It must stay unmapped, and it must not be zipped onto the next player. Pinned in Task 2 (`TestEnsureScoreboardMapping_LinkBeatsZip`).
2. **The owner is resolved after the first `ensureScoreboardMapping` call** (a late joiner's player packet arrives mid-round). Resolution must be lazy: a linked entity left unresolved now is resolved on a later call. Pinned in Task 2 (`TestEnsureScoreboardMapping_LateOwner`).
3. **A replay or build with no link tokens** (an unknown Y10 build, a truncated round). The legacy zip and the `+4` rule must behave exactly as at bd6ccbe. Pinned in Task 2 (`TestEnsureScoreboardMapping_NoLinksKeepsZip`).
4. **A token look-alike** (an unnamed `0x1A` ref, another class, a non-zero high half). It must be ignored. Pinned in Task 1 (`TestReadScoreboardEntity`).
5. **Two scoreboard entities resolving to the same player.** This is not seen in the corpus, but a reconnect could produce it. The first mapping wins, and no player is mapped twice. Pinned in Task 2 (`TestEnsureScoreboardMapping_NoDoubleMapping`).

---

## Summary

On Y10S4+ replays, our scoreboard code does not know which player a scoreboard entity belongs to. It pairs the entity IDs, in first-seen order, with the order in which `readPlayer` ran. A fallback guesses `sbID[0]+4 == DissectID[0]`. When one entity has no player packet, the pairing shifts by one for every later player. This happens when a player left the match and still has a scoreboard entity. From then on, the kill reconciliation reassigns kills to the wrong players, and score and assists are written to the wrong rows. The replay states the link explicitly: each player object holds a reference to its scoreboard entity. The fork Animuxs/r6-stats-app reads this link. We port the link, read our own way (through the player-object handle, not a username search). We do not port the fork's LCS kill-feed alignment: with a correct link it produced exactly the same attributions as our existing reconciliation on every round of the corpus.

## Source

- Fork: **Animuxs/r6-stats-app**, commit `bf687ca24dddb0f7ecf1eb02d6b6a740384e51aa` ("Pending changes exported from your codespace", 2026-09-25). atrejo26/r6-dissect-stats has the same `dissect/scoreboard.go` (`8dc0307` carries `bf687ca` forward unchanged; `Animuxs/main` `e8b0eb5` only adds bounds checks).
- Files: `dissect/scoreboard.go` (`scoreboardEntityIndicator`, `readScoreboardEntity`, `scoreboardEntityAt`, `scoreboardValue`, `fixKillersFromScoreboard`, `reassignKiller`), `dissect/reader.go`, `dissect/time.go`.
- What it does:
  - `readScoreboardEntity` listens on `EB 21 9B 38` and reads 4 bytes as the entity ID. It then looks for the player packet `22 07 94 9B DC` within the next 256 bytes and takes that username.
  - `scoreboardEntityAt` walks back from a scoreboard field to `<entity id> 00000000` and treats a kills field that is followed by `22 CD9C5D72` (deaths) as the round-start snapshot. Otherwise the field is an update.
  - `fixKillersFromScoreboard` builds the list of feed killers and the ordered list of scoreboard kill increments, then aligns them by longest common subsequence. Inside each unmatched gap, it reassigns the k-th unmatched feed kill to the k-th unmatched scoreboard killer, never across teams (`reassignKiller`).
- Flaws:
  - The username search depends on the player packet following the reference within 256 bytes. It does today (+165 bytes in every round we probed), but this is layout, not structure. The reference's owner handle *is* the player object, which we already track.
  - It reads 4 bytes after the field without checking the `0x1B` op or the class, so any `EB219B38` field use matches. On Y8S1 that is more than 50,000 hits per round.
  - The LCS replaces the whole reconciliation, and it is written on a 2025-09 upstream base that does not have our `reconcileKillsFromScoreboard`, `TeamKill` flag or `scoreboardKiller` timing.
  - We read the fork's code but did not build or run it (safety rule).

## Current behaviour (bd6ccbe)

`dissect/scoreboard.go`, `ensureScoreboardMapping`:

```go
	for i := 0; i < maxZip; i++ {
		if _, exists := r.scoreboardIDToPlayer[r.pendingSBIDs[i]]; !exists {
			r.scoreboardIDToPlayer[r.pendingSBIDs[i]] = r.readPlayerOrder[i]
		}
	}
	...
		candidateID := []byte{sbID[0] + 4, sbID[1], sbID[2], sbID[3]}
```

`pendingSBIDs` is filled by `readScoreboardKillsY10S4` (`r.pendingSBIDs = append(r.pendingSBIDs, sbID)` the first time an sbID is seen), and `readPlayerOrder` by `readPlayer` (`r.readPlayerOrder = append(r.readPlayerOrder, foundIndex)`). The same map is used by `readScoreboardAssistsY10S4`, by `readScoreboardScoreY10S4` (including the +100 planter/disabler bonus) and by `reconcileKillsFromScoreboard`.

**Wire layout** (Y11S3, user match `Match-2026-10-18_21-07-35-14012-R01`, offset 36,882):

```
1B 30bb2bf000000000 EB219B38 39bb2bf000000000 18A591A1      ref: player obj 30bb2bf0 -> scoreboard entity 39bb2bf0
23 39bb2bf000000000 1CD2B19D 04 00000000 22 CD9C5D72 ...    round-start snapshot of entity 39bb2bf0
...
1B 30bb2bf000000000 27C08DCA ...  22 07949BDC 07 "player41"  player object 30bb2bf0 (our playerObjectField) and username
```

The `sbID` our code reads (`scoreboardEntryPlayerID`, `r.offset - 17`) is the first 4 bytes of the entity handle.

### Reproduction (run 2026-10-21, scratch clone at bd6ccbe)

A probe (an internal test with a hook before `reconcileKillsFromScoreboard`) ran OUR parser on 101 unique rounds: our fixtures, the PR #10 Y11S3 fixtures, the user's Y11S2 and Y11S3 matches, and 64 replays committed in forks. It resolved the link two ways (owner handle through `playerIndexByObject`, and the fork's 256-byte username search) and compared it with our zip:

| season | rounds | header players | linked (owner) | owner vs search disagree | zip-wrong rounds / entries |
|---|---|---|---|---|---|
| Y8S1 | 7 | 70 | 70 | 0 | n/a (pre-Y10S4 path) |
| Y8S2 | 5 | 50 | 50 | 0 | n/a |
| Y9S1 | 1 | 10 | 10 | 0 | n/a |
| Y11S1 (forks) | 46 | 330 | 330 | 0 | **4 / 16** |
| Y11S2 | 19 | 190 | 190 | 0 | 0 |
| Y11S3 | 23 | 230 | 230 | 0 | 0 |

There are no Y10 replays in the corpus. The `+4` rule was never wrong, but it found no player for all 190 Y11S2 entities and for 58 of 230 Y11S3 entities, so it cannot be the primary rule.

The zip fails in nicktstewart `Y11S1-changes`, `dissect/test/data/replays/defuser_detection/match4/Match-2026-03-09_17-13-54-47572-R0{2..5}.rec`. Player `player48` left after R01. In R02–R05 the header has 9 players, but there are 10 scoreboard entities:

```
pending 4 70492cf0 zip=player57       owner=(no player)  search=player48
pending 5 5d492cf0 zip=player50   owner=player57
pending 6 4c492cf0 zip=player27   owner=player50
pending 7 43492cf0 zip=player33  owner=player27
pending 8 32492cf0 zip=player07    owner=player33
pending 9 21492cf0 zip=              owner=player07
```

The CLI output for the match, bd6ccbe vs the prototype (Task 1 + 2):

```
R2 player50 -> player68  | proto: player57         (also R2 x2, R3 x1, R4 x3: 7 kills)
stats player27 base kills 5  proto 1
stats player57     base kills 1  proto 4
R2 roundstats base ('player57', 0, 0)  proto ('player57', 970, 0)   (score; every round R2-R5)
R2 roundstats base ('player50', 970, 0)  proto ('player50', 710, 0)
```

The base output on R02 is also **nondeterministic**: 6 runs of the bd6ccbe CLI on that single round gave 5 different kill lists. A wrong mapping creates several over-credited and under-credited players at once, and the reassign loop iterates a Go map. That is PR #12's bug, made worse by this one. With the prototype, the same 6 runs gave one result.

**Kill attribution, ours vs an LCS prototype** (written in the probe from the fork's description, using link-based usernames): there are 8 rounds where the feed killers and the scoreboard increments disagree. Seven are Y10S4+ rounds; the eighth, fork Y8S1 `quick_1`, is pre-Y10S4, and neither method touches it. Each method's final kill attribution was compared with the link-based scoreboard:

| | bd6ccbe | prototype (link + our reconcile) | LCS (link) |
|---|---|---|---|
| rounds where ours ≠ LCS | 3 (all match4) | **0** | – |
| kills reassigned | 13 (7 wrong) | 7 | 7 |
| Y10S4+ rounds whose per-player kills differ from the scoreboard | 3 | **0** | 0 |

A second full probe run with the prototype gave byte-identical results on all 101 rounds. All six Task 1/2 unit tests below were written and run against the prototype (green), and `LinkBeatsZip` was confirmed red with the link branch disabled. On the 6 user rounds with a disagreement (Y11S2 and Y11S3), bd6ccbe, the prototype and LCS all make the same single reassignment. That is the expected kind: the feed credits the finisher, and the scoreboard credits the player who scored the kill.

## Approaches considered

1. **Port the fork as written** (256-byte username search, snapshot detection by the next field, LCS replacing our reconciliation). It fixes match4, but it reads by layout, not structure, discards working timing logic, and churns every Y10S4+ golden that has a reassignment. The LCS showed no measured benefit over our reconciliation once the link was correct.
2. **Link through the owner handle, keep our reconciliation (chosen).** A structural parse of the ref token, resolved through the player-object table we already maintain for loadouts. The change is small and deterministic. It produced zero golden churn on main fixtures and agrees 100% with the username search on all 101 rounds.
3. **Make the `+4` rule primary.** Never wrong in the corpus, but no match for any Y11S2 entity. Rejected.
4. **Use PR #10's object graph (`wire.Graph.Parent(entity)`).** This is the most principled option: the edge is exactly an `OpRefNamed` token. But PR #10 is not on main and is a large branch. Deferred. Once PR #10 lands, `readScoreboardEntity` can be replaced by a graph lookup with no behaviour change.

## Decision and rationale

- **D1: Link source = the owner handle of `1B <owner> EB219B38 <entity> 18A591A1`, resolved by `playerIndexByObject`.** Evidence: 880 of 880 header players linked across Y8S1–Y11S3, with 0 disagreements against the fork's username search. Alternative: the username search (see D2).
- **D2: No username search.** Fallback is the legacy zip/`+4` only. Reason: the owner handle is structural, and the username search adds a second, layout-dependent truth that never differed in the corpus.
- **D3: Precedence.** If the round has any link token: linked entities map only through their link (unresolved means unmapped, never zipped). Unlinked entities use only the `+4` rule. If the round has no link token: bd6ccbe behaviour, byte for byte. Reason: a positional zip is meaningless once any entity is known to be out of order, and the no-link path keeps the risk on unseen builds at zero.
- **D4: Lazy resolution.** Store `entity -> owner handle` at listen time and resolve in `ensureScoreboardMapping`. The token precedes the player packet (by about 165 bytes in every round probed), so resolving at listen time would always fail.
- **D5: Do not port the LCS alignment.** It matched our reconciliation on 101 of 101 rounds once the link was correct. Revisit only with a round where the two disagree under a correct link. One candidate to watch: user `Match-2026-10-18_21-07-35-14012-R02`, where both methods move `player61 -> player35` to `player76`. The byte order (`SB+ player76`, `SB+ player61`, feed `player61 -> Mako`, feed `player61 -> player41`) makes the second kill the more likely player76 kill. No ground truth exists for it.
- **D6: Do not port snapshot detection by the following deaths field.** Our first-seen rule agrees: 0 rounds where a first-seen kills field was not a snapshot.
- **D7: PR #12 is not superseded.** It sorts the over/under lists in `reconcileKillsFromScoreboard`, while this change fixes the input of that function. The two are orthogonal. Its patch applies over the prototype with fuzz on the import block only (`"bytes"` here, `"sort"` there). Recommendation: merge PR #12 first or in either order. It would only be superseded if D5 were reversed and the LCS replaced `reconcileKillsFromScoreboard`.
- **D8: Regression fixture.** Add match4 R02 (6.5 MB, comparable to existing fixtures) as `dissect/test/data/replays/valid/Y11S1/left_player_r02.rec`. If item 15 (Y11S1 fixtures from forks) lands first and already includes this file, reuse its path.

## Interactions

- **PR #12** (`ElyasYassin/fix/deterministic-scoreboard-reconcile`, `ee8b078`): see D7. It is complementary, and the match4 R02 nondeterminism shows that it matters: 5 different outputs in 6 runs at bd6ccbe.
- **PR #3** (`origin/fork/health-dbno`): it adds `+50` revive matching in `readScoreboardScoreY10S4`, keyed by the mapped player index, so this fix makes it correct on rounds with a departed player. It also realigns the `Reader` struct field block, which conflicts textually with the new field (whitespace only; rerun `gofmt`). It runs `resolveHealthEvents` after `roundEnd` "so that kills corrected from the scoreboard credit the downs", which is unaffected.
- **PR #10** (`origin/track/rec-timeline`): there is no code overlap in `ensureScoreboardMapping`. Approach 4 is a possible follow-up. Its Y11S3 fixtures had a correct zip in the probe, so no churn is expected there.
- **Item 08** (late joiner gets no scoreboard row): different mechanism (row creation). With this change, a late joiner's entity resolves once their player packet is read, which item 08's fix can rely on.
- **Item 02** (Y11S2 build 9734089 player-id markers): if `readPlayer` misses a player there, the link cannot resolve them either, but it no longer shifts everyone else.
- **Item 14** (per-player trade stats) and every kill-derived stat consume the corrected attributions.
- **Item 15** (Y11S1 fixtures): see D8.
- **JSON / R6Stats:** no schema change. The values change only on rounds where the zip was wrong: kill `username`, per-round `score` and `assists`, and the defuser planter/disabler from the +100 bonus. R6Stats (`api/r6stats/worker/jobs.py`) needs no change.
- **Golden churn:** none on main fixtures (`go test ./...` green with the prototype). The new Y11S1 fixture adds one `.rec.json`.

## Implementation plan

### Task 1: Read the scoreboard entity reference

**Files:**
- Modify: `dissect/scoreboard.go` (new vars and listener, next to `readScoreboardKills`)
- Modify: `dissect/reader.go` (`Reader` field, `NewReader` init and listener registration)
- Create: `dissect/scoreboard_link_test.go`

**Interfaces:**
- Produces: `var scoreboardEntityField = []byte{0xEB, 0x21, 0x9B, 0x38}`, `var scoreboardEntityClass = []byte{0x18, 0xA5, 0x91, 0xA1}`, `func readScoreboardEntity(r *Reader) error`, and the field `Reader.scoreboardEntityOwner map[[4]byte]uint64 // Y10S4+: scoreboard entity ID -> owning player object handle` (keep the comment so `gofmt` does not realign the struct block).
- Test helpers (in the test file): `func (b *inventoryBuilder) scoreboardLink(op byte, playerObj, entity uint64, class []byte)` writes `op, handle(playerObj), scoreboardEntityField, handle(entity), class`; `func sbID(entity uint64) [4]byte` returns the low 4 bytes, little-endian.

- [ ] **Step 1: Write the failing test** `TestReadScoreboardEntity`, table-driven, with `obj = 0xF02BBB30` and `ent = 0xF02BBB39`. Each case builds one token, sets `r := &Reader{b: b.Bytes(), scoreboardEntityOwner: map[[4]byte]uint64{}}` and `r.offset = 1 + handleSize + len(scoreboardEntityField)`, then calls `readScoreboardEntity(r)`:
  - `{"named ref", 0x1B, scoreboardEntityClass}`: `r.scoreboardEntityOwner[sbID(ent)] == obj`
  - `{"unnamed ref", 0x1A, scoreboardEntityClass}`: no entry
  - `{"other class", 0x1B, []byte{1, 2, 3, 4}}`: no entry
  - `"non-zero high half"`: entity `0x00000001F02BBB39` with op `0x1B`: no entry
- [ ] **Step 2: Run** `go test ./dissect -run TestReadScoreboardEntity -count=1`. Expected: build FAIL (`undefined: readScoreboardEntity`, `unknown field scoreboardEntityOwner`).
- [ ] **Step 3: Implement.** `readScoreboardEntity` takes `start := r.offset - len(scoreboardEntityField)`. It requires `r.b[start-handleSize-1] == 0x1B`, `bytes.Equal(r.b[start+4+handleSize:start+slotTokenSize], scoreboardEntityClass)` and `isZero(r.b[start+8:start+12])`, with bounds checks as in `readInventoryOwner`. It reads `owner := readHandle(r.b[start-handleSize:start])`, ignores `owner == 0`, and stores `r.scoreboardEntityOwner[[4]byte(r.b[start+4:start+8])] = owner`. In `NewReader`, initialise the map with the other scoreboard maps and add `r.Listen(scoreboardEntityField, readScoreboardEntity)` inside the existing `if r.Header.CodeVersion >= Y10S4 {` block, after `readDefuserPawn`. Add `"bytes"` to the imports of `scoreboard.go`.
- [ ] **Step 4: Run** `go test ./dissect -run TestReadScoreboardEntity -count=1 && gofmt -l dissect`. Expected: `ok`, and no files listed.
- [ ] **Step 5: Commit** `feat(scoreboard): read the scoreboard entity reference of each player object`.

### Task 2: Map scoreboard entities through the link

**Files:**
- Modify: `dissect/scoreboard.go` (`ensureScoreboardMapping`)
- Test: `dissect/scoreboard_link_test.go`

**Interfaces:**
- Consumes: `Reader.scoreboardEntityOwner` (Task 1), `r.playerIndexByObject(handle uint64) int`, `r.trackPlayerObject(handle uint64, p Player)`, `newInventoryState()`.
- Produces: the unchanged signature `func (r *Reader) ensureScoreboardMapping()`, with the D3 precedence.

- [ ] **Step 1: Write the failing tests.** Every fixture sets `inventory: newInventoryState()` and `scoreboardIDToPlayer: map[[4]byte]int{}`, and registers player objects with `trackPlayerObject`.
  - `TestEnsureScoreboardMapping_LinkBeatsZip`: players `a`, `b`, `c` (DissectIDs `{0x11,0,0,0}`, `{0x22,0,0,0}`, `{0x33,0,0,0}`) with objects `0xF0000010`, `0xF0000020`, `0xF0000030`. `pendingSBIDs = {sbID(0xF00000A1), sbID(0xF00000B1), sbID(0xF00000E1), sbID(0xF00000C1)}`, `readPlayerOrder = {0, 1, 2}`. The links are A→obj a, B→obj b, E→`0xF0000099` (untracked: the player who left), C→obj c. Assert A→0, B→1, C→2, and E absent.
  - `TestEnsureScoreboardMapping_LateOwner`: same as above, but call `ensureScoreboardMapping()` before `trackPlayerObject` for `c`. Assert that C is unmapped and that nothing is zipped onto player 2. Then track `c`, call it again, and assert C→2.
  - `TestEnsureScoreboardMapping_NoLinksKeepsZip`: players `a` and `b`, `pendingSBIDs = {A, B}`, `readPlayerOrder = {1, 0}`, `scoreboardEntityOwner` empty. Assert A→1 and B→0.
  - `TestEnsureScoreboardMapping_NoDoubleMapping`: two entities both linked to obj a. Assert exactly one of them maps to 0 (the first in `pendingSBIDs`) and the other is absent.
- [ ] **Step 2: Run** `go test ./dissect -run TestEnsureScoreboardMapping -count=1`. Expected: `LinkBeatsZip` and `LateOwner` FAIL (C unmapped and E zipped to player 2, as at bd6ccbe); the other two PASS.
- [ ] **Step 3: Implement the D3 precedence in `ensureScoreboardMapping`.** First compute `mappedPlayers` from the existing map. If `len(r.scoreboardEntityOwner) > 0`:
  1. For each `sbID` in `pendingSBIDs` that is not yet mapped and has a link: map it to `playerIndexByObject(owner)` when that is `>= 0` and the player is not in `mappedPlayers`.
  2. For each `sbID` that is still unmapped and has **no** link entry: apply the existing `+4` rule.
  3. Return.

  Otherwise run the existing zip and `+4` code unchanged.
- [ ] **Step 4: Run** `go test ./dissect -count=1`. Expected: `ok`.
- [ ] **Step 5: Commit** `fix(scoreboard): map scoreboard entities to players through the player object link`.

### Task 3: Replay regression for a player who left

**Files:**
- Create: `dissect/test/data/replays/valid/Y11S1/left_player_r02.rec`. Get it with `git show cc0f7e04a23486291c920f5085f12f5ef7313de5 > ...` from a clone that has fetched `nicktstewart/r6-dissect` branch `Y11S1-changes`. The path there is `dissect/test/data/replays/defuser_detection/match4/Match-2026-03-09_17-13-54-47572-R02.rec`, 6,482,569 bytes. Skip this if item 15 already added it, and use its path.
- Create: `dissect/test/data/replays/valid/Y11S1/left_player_r02.rec.json` (generated)
- Create: `dissect/test/scoreboard_link_test.go`

**Interfaces:**
- Consumes: the exported `dissect.NewReader`, `Reader.Read`, `dissect.Ok`, `Reader.MatchFeedback`, `Reader.Header.Players` and `Reader.Scoreboard.Players`.

- [ ] **Step 1: Write the test** `TestReader_ScoreboardLinkLeftPlayer`. Open the fixture, `NewReader`, `Read` (accept `dissect.Ok(err)`). Then assert:
  - the `Kill` targets of `player57` are exactly `[player68 player46 player11]`
  - `player50` has no `Kill`
  - the `Kill` whose target is `player33` is credited to `player19` (the feed says `player68`; this is the one genuine feed/scoreboard correction in the round)
  - `Scoreboard.Players[idx("player57")].Score == 970`, `player50 == 710`, `player27 == 600`, `player33 == 725`, `player07 == 670` with `Assists == 3`
  - running `Read` on a fresh reader 10 times gives the same kill list (determinism)
- [ ] **Step 2: Run** `go test ./dissect/test -run TestReader_ScoreboardLinkLeftPlayer -count=1` against the Task 2 code with Task 2's precedence block temporarily disabled (or on a checkout of bd6ccbe plus the fixture and test). Expected: FAIL (`player57` score 0, and kills on `player50`, varying run to run).
- [ ] **Step 3: Generate the golden file:** `go test ./dissect/test -run TestReader_Read -update`, then `git status` shows only the new `left_player_r02.rec.json`.
- [ ] **Step 4: Run** `go test ./...`. Expected: all `ok`, with no change to any existing `.rec.json` (`git diff --stat -- dissect/test/data` lists nothing).
- [ ] **Step 5: Commit** `test(scoreboard): Y11S1 round with a departed player keeps scoreboard rows on the right players`.

## Acceptance

- `go test ./dissect -run 'TestReadScoreboardEntity|TestEnsureScoreboardMapping' -count=1`: red at bd6ccbe (build failure or `LinkBeatsZip`), green when done.
- `go test ./dissect/test -run TestReader_ScoreboardLinkLeftPlayer -count=1`: red at bd6ccbe (with the fixture added), green when done.
- `go test ./...` green, with no existing golden JSON changed.
- The CLI on the match4 folder (R01–R05) gives `player57` 4 kills and `player27` 1 kill in the `stats` block, and identical output over 5 runs.

## Risks / open questions

- **There are no Y10S4–Y10S? replays in the corpus.** The token is present and identical from Y8S1 to Y11S3, so Y10 is very likely the same, and if it is absent the code falls back to bd6ccbe behaviour (D3).
- **The 0x1B owner could be something other than the player object on some build.** Then `playerIndexByObject` returns -1, that entity stays unmapped, and only the `+4` rule could map it. This is a possible regression vs the zip on such a build. Mitigation: the probe showed 880 of 880 resolved; the implementer may log `scoreboard_entity_unresolved` at debug level for linked-but-unresolved entities at round end.
- **D5 stands until a counter-example appears.** The `player61`/`player76` round is the closest case. Both methods pick the same kill, so neither change would alter it.
- **PR #12 merge order.** Either order works. The import block needs a manual one-line merge if both land the same day.
