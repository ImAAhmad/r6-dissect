# Fork port 02: detect the player-record tags of Y11S2 build 9734089 (design + implementation plan)

> **Issue:** #34 · prestonwillis10/r6-dissect · fork survey: `docs/research/2026-10-21-fork-survey.md` · prototype: `docs/research/fork-port-prototypes/02/`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `readPlayer` reads every player on Y11S2 build 9734089 (Y11S2_Alpha03 mid-season patch), where the player-id tag is `8C 61 1A 75 23` and the uiID tag is `70 FA AF 28`, and keeps reading every other build exactly as today.

**Architecture:** A pure function `detectPlayerMarkers(b, codeVersion)` picks, for each round, which known tag set the decompressed round contains. `(*Reader).playerMarkers()` runs it on the first `readPlayer` call and caches the result on the `Reader`. `readPlayer` seeks the detected tags in place of its two hard-coded literals. No build number gates the choice.

**Tech Stack:** Go (module `github.com/redraskal/r6-dissect`), zerolog. No new dependencies.

**Spec:** this document. The design sections come before the plan.

Verified against: `origin/main` bd6ccbeb7b46af99638df5d650187069e849f925.

## Global Constraints

- Base branch: `main` at bd6ccbe or later. Go: `export PATH=$HOME/.local/go/bin:$PATH`.
- Tag bytes, exactly: classic id `33 D8 3D 4F 23`, classic uiID `38 DF EE 88`, 9734089 id `8C 61 1A 75 23`, 9734089 uiID `70 FA AF 28`, Y7S2-and-earlier id `E6 F9 7D 86`.
- Detection order: the classic set first, then the 9734089 set.
- The `CodeVersion <= Y7S2` and `CodeVersion >= Y9S3` gates in `readPlayer` stay as they are. No new version gate is added, and `Y11S2` in `dissect/version.go` keeps its value (9838928).
- No golden `.rec.json` file may change. `go test ./dissect/test -run TestReader_Read` must pass without `-update`.
- No new binary fixture. The acceptance test rewrites the existing `dissect/test/data/replays/valid/Y11S2/*.rec` fixtures in memory.
- Commit labels: a commit that changes code is `fix(...)`, never `docs(...)`.

## Review Focus

1. **A 9734089 round that also contains a stray 4-byte classic uiID** (`38 DF EE 88` appears by chance about once per 4 GB, so about 1% of 50 MB rounds): the 9734089 uiID tag must win, because the id tag already chose that set. Pinned by the case "the uiID tag of the set the id tag picked wins" in `TestDetectPlayerMarkers` (Task 1).
2. **A future build that moves only one of the two tags**: the other tag must still be read. Pinned by "the uiID tag is found on its own when only it moved" (Task 1).
3. **A build with neither known id tag**: behaviour must equal today's (seek the classic tag, fail the same way), plus one warning per round, not one per player. Pinned by the two "falls back" cases (Task 1) and the cache in Task 2.
4. **Old seasons** (Y8S1 to Y9S1 contain `38 DF EE 88` tens of thousands of times, and their uiID is not read): output must not change. Pinned by the "before Y9S3" case (Task 1) and the unchanged goldens (Task 2, Step 6).
5. **Detection run after the buffer changes**: the cache is filled on the first `readPlayer` call, not in `NewReader`, so a test (or any caller) that edits `r.b` before `Read` gets tags for the edited buffer. Pinned by `TestReadPlayer_Y11S2Alpha03Tags` (Task 2), which only passes with lazy detection.

---

## Summary

Ubisoft's Y11S2 mid-season patch build 9734089 (`Y11S2_Alpha03`, live about 2026-06-11 to 2026-06-21) re-tagged two properties inside the player record: the tag before the 4-byte DissectID moved from `33 D8 3D 4F 23` to `8C 61 1A 75 23`, and the tag 13 bytes before the uiID moved from `38 DF EE 88` to `70 FA AF 28`. The next builds (Y11S2_Alpha04: 9751808, 9785623, 9838928) changed both back. Our `readPlayer` seeks only the classic tags and its seeks are fatal, so on that build the first player record seeks to the end of the round, `Reader.Read` returns `io.EOF`, and the round loses all operators, profile IDs, spawns and every later event. In a match folder, `MatchReader.Read` stops after round 1 and the CLI panics on the nil rounds.

The fix detects the tags by looking for them in the round, one `bytes.Contains` per tag per round, instead of gating on the build number. Two forks gated on build numbers and broke the next builds when the tags flipped back. Detection cannot go stale that way.

**Validation limit:** no replay from build 9734089 exists in the fork mirror (95 `.rec` blobs across all refs and history) or in the user's corpus (206 rounds). The reproduction and the acceptance test use real Y11S2_Alpha04 rounds rewritten with the 9734089 tags. The tag bytes come from two independent forks that tested on real 9734089 replays (defiancer6s-hub: 4 matches, 43 rounds). The issue therefore carries the `research` label: one real 9734089 match should be run when one turns up.

## Source

| Fork / commit | What it does | Flaws |
|---|---|---|
| **cliffside-git `ca5406d`** (Alec Wang, 2026-06-12, "fix: parse Y11S2 mid-season build (codeVer 9734089)") | Gates the new id tag on `r.Header.CodeVersion >= 9734089`. Makes the uiID and profileID seeks non-fatal (save offset, restore on failure). | The `>=` gate broke every build from 9785623 on (their next commit says about 100% of parses failed for 11 weeks). Non-fatal uiID drops the uiID on 9734089 instead of reading it. It says the profileID tag "also moved", but no other fork changed it, and defiancer's fatal profileID seek worked on 43 real rounds, so that claim is likely wrong. |
| **cliffside-git `4fc9465`** (kaishi3, 2026-09-09, "fix: detect the player-id marker instead of gating it on a build number") | `(*Reader).playerIDIndicator()`: classic if `bytes.Contains(r.b, classic)`, else the 9734089 tag if present, else warn and use classic; cached in a `Reader.idIndicator` field. Has a build survey: 9718747 classic, 9734089 new, 9785623 classic, 9879602 classic. Tests in `dissect/player_internal_test.go`. | Detects only the id tag. The uiID still relies on `ca5406d`'s non-fatal seek, so on 9734089 the uiID is lost and atk op swaps fall back to the player-object match. **Best design; we take it and extend it to the uiID.** |
| **aa-sasha `cec8ad7`** (2026-08-27, "fix(y11s2): decode Alpha03 player markers"), branch `aa-sasha/codex/y11s2-alpha03-player-markers` | `playerIndicators(codeVersion)` returns both tags; new set only when `codeVersion == Y11S2Alpha3` (9734089). Also makes listener errors non-fatal and adds `SeekBounded` (item 03 territory). | Exact-build gate: correct for the one build seen, wrong for any sibling build with the same tags. |
| **aa-sasha `bfcff23`** (2026-06-17) | Flushes the 10th header player when the header loop ends on `teamscore1`; guards `PlayerStats`. | Separate mechanism (header parsing). Ours is already fixed on `origin/fork/health-dbno` by `a17b9f4` ("fix: read gmSettings and the last header player on Y11S2+"), not yet on main. Not part of this item. |
| **defiancer6s-hub `26213be`** (2026-07-05) then **`6186a87`** (2026-07-11) | Both tags, first gated `>= Y11S2` (their `Y11S2 = 9734089`), then narrowed to `[9734089, 9751808)` after Alpha04 9751808 reverted. Verified on 4 real 9734089 matches, 43 rounds, and on 9718747 and 9679781. | Range gate: needs a new constant every time Ubisoft flips. Also their constant names collide with ours (`Y11S2` means 9734089 there, 9838928 here). |
| **nicktstewart `dd6fbc1`** (2026-06-14, "[Fix] Header location") | New id tag gated `>= Y11S2_1` (9734089); uiID and profileID seeks made non-fatal. | Same `>=` trap as `ca5406d`. |

Consensus across the four forks: exactly two tags changed (id and uiID), only on 9734089 among the builds anyone saw; the spawn, operator and profileID seeks did not change.

## Current behaviour

`dissect/player.go` at bd6ccbe:

- `readPlayer` hard-codes the id tag: `idIndicator := []byte{0x33, 0xD8, 0x3D, 0x4F, 0x23}` / `if r.Header.CodeVersion <= Y7S2 { idIndicator = []byte{0xE6, 0xF9, 0x7D, 0x86} }`, then `if err := r.Seek(idIndicator); err != nil { return err }`.
- The uiID seek is hard-coded too: `if r.Header.CodeVersion >= Y9S3 { if err = r.Seek([]byte{0x38, 0xDF, 0xEE, 0x88}); err != nil { return err }`.
- `dissect/reader.go` `Read`: `if err = listener(r); err != nil { return }`, so one failed seek ends the round before `roundEnd`.
- `dissect/match.go` `MatchReader.Read`: `if err := m.read(i); err != nil { return err }`, and `main.go` treats `io.EOF` as success (`if err := m.Read(); !dissect.Ok(err)`), so the CLI goes on to write a match whose later rounds are nil.
- `dissect/version.go`: `Y11S2 int = 9838928` is a late Y11S2_Alpha04 build, not the first Y11S2 build. It is not used by any code on main; `origin/track/rec-timeline` uses it in `dissect/timeline.go` (`if r.Header.CodeVersion < Y11S2 {` refuses the timeline).

### Search for a real 9734089 replay

- Fork mirror: every `.rec` blob in every ref and in history (`git rev-list --all --objects | grep '\.rec$'`, 94 objects), read with our parser: builds 7422506 to 8248131 (Y8S1 to Y9S1), 9540659 and 9578674 (Y11S1), 9838928 (Y11S2_Alpha04), 9950954 (Y11S3). No 9718747, 9734089, 9751808 or 9785623.
- `git grep` of all fork tips for `9734089`: only Go constants and comments; no fixtures or goldens.
- User corpus (`.../a0d4e668-.../scratchpad`, 206 `.rec` files): 9838928 and 9950954 for Y11S2/Y11S3.
- Production uploads volume (`r6stats-deploy_uploads`): not searched; reading production data was denied in this session. The deployment started 2026-10-20, so June replays are unlikely to be there.

### Tag counts on real rounds (147 rounds, Y8S1 to Y11S3)

The 9734089 tags (`8C 61 1A 75 23`, `70 FA AF 28`) occur **0 times** in all 147 rounds. The classic id tag occurs in every round (10 to 13 times; 1 to 2 in a few short Y11S1 rounds). The classic uiID tag occurs 9 to 18 times per round on Y11S1 to Y11S3, and 29,780 to 73,540 times per round on Y8S1 to Y9S1, where it is not read (the `>= Y9S3` gate).

### Reproduction (ran at bd6ccbe)

A scratch internal test read each fixture, then read it again after rewriting the decompressed round (`bytes.ReplaceAll` of both classic tags with the 9734089 tags, `CodeVersion = 9734089`) before `Read`:

```
custom_early_plant.rec code=9838928 classicID=10 alphaID=0 classicUI=13 alphaUI=0
ORIGINAL err=<nil> players=10 (Rook, Warden, Smoke, ...) feedback=13 kills=8
MUTATED  err=EOF   players=9, every operator "Operator(0)", every profileID "", feedback=0
```

Over 44 real Y11S2/Y11S3 rounds (2 fixtures + the user's 2026-08-25 and 2026-10-18 matches), all 44 rewritten rounds failed the same way: `errB=EOF players=10/9 fb=N/0`.

End to end, the rounds of `Match-2026-08-25_20-53-53-41172` (9 rounds) were rewritten to disk with the 9734089 tags and header (`codeVersion` `9734089`, `version` `Y11S2_Alpha03`, re-compressed as one zstd frame after the original header):

```
$ r6-dissect-bd6ccbe <synthetic match folder> -f json -o out.json
panic: runtime error: invalid memory address or nil pointer dereference
github.com/redraskal/r6-dissect/dissect.(*Reader).PlayerStats(0x0)  dissect/stats.go:95
github.com/redraskal/r6-dissect/dissect.(*MatchReader).Data          dissect/match.go:293
exit=2
```

With the prototype of this plan, the same folder exits 0 and its JSON equals the original match's JSON except `codeVersion` and `gameVersion` (9 rounds, 10 stats rows).

## Approaches considered

**A. Version gate** (`ca5406d`, `dd6fbc1`: `>= 9734089`; aa-sasha: `== 9734089`; defiancer: `[9734089, 9751808)`).
- Pro: no scan; one line.
- Con: the tags flip without regard to build order. `>=` broke every later build for 11 weeks in the wild. `==` and the range both depend on knowing every build that used the tags; we have seen only one, and builds between 9734089 and 9751808 are unknown. Every flip needs a new constant and a release.

**B. Detect per round, id tag only** (`4fc9465`).
- Pro: cannot go stale; one `bytes.Contains` per round.
- Con: leaves the uiID seek fatal (in our code) or lossy (in theirs, via `ca5406d`'s non-fatal seek). On our code it would still fail at the uiID seek on 9734089.

**C. Detect per round, both tags, uiID chosen in the id tag's set first** (chosen).
- Pro: B's robustness for both tags. Preferring the uiID tag of the set the 5-byte id tag picked guards against a stray 4-byte match of the other set's uiID tag (about 1% of 50 MB rounds by chance). Falling back to any other known uiID tag covers a build that moves only one tag.
- Con: two to four `bytes.Contains` passes on the first `readPlayer` of each round (about 10 ms on a 50 MB round). A little more code than B.

**D. Per player, seek the nearest known tag** (no cache).
- Pro: handles a round that mixes tags (none seen).
- Con: for an absent tag, each player scans the rest of the round, so 10 extra scans per round; the same coincidence risk as C without the set preference. No evidence that mixing happens.

**E. Make the uiID (and id) seeks non-fatal** (`ca5406d`, `dd6fbc1`).
- Pro: also survives an unknown future tag.
- Con: loses the data instead of reading it; and generic listener resilience is item 03. Rejected for this item.

## Decision and rationale

Approach C. Decisions made autonomously (the brainstorming questions, answered):

1. **Detection, not gating.** The forks' history is the evidence: every gate was wrong for some build. Alternatives A/B/D/E above.
2. **Both tags, one cached set per round.** The forks with real 9734089 data (defiancer, aa-sasha) moved both tags together. Cached on the `Reader` (one `Reader` per round) in a new field `markers *playerMarkers`.
3. **Lazy detection on the first `readPlayer` call**, not in `NewReader`. It costs nothing on rounds that never reach a player record, and it lets the acceptance test rewrite `r.b` after `NewReader`. Alternative (eager in `NewReader`): simpler field, but the in-memory acceptance test would then be impossible without a test hook.
4. **Order: classic first.** Every build we have except 9734089 uses the classic tags, and the 9734089 tags never occur in 147 real rounds of other builds.
5. **Unknown tags fall back to classic** and log one `log.Warn()` per round with `gameVersion` and `codeVersion`. Behaviour is then exactly today's (the seek fails), and the log line tells the operator a new build moved a tag. Alternative: return an error at once; rejected, because it changes behaviour for builds that today partially parse.
6. **Version constants.** `dissect/version.go` names the first build of a season `YxSy` (for example `Y11S1 int = 9578674`) and later builds of that season `YxSy_n` (`Y10S2_1` to `Y10S2_5`) or `Y9S1Update3`; code uses them as lower bounds (`CodeVersion >= Y10S4`). Our `Y11S2 int = 9838928` is a late Alpha04 build: builds 9718747, 9734089, 9751808 and 9785623 are Y11S2 but fall below it. This item adds **no** constant and **does not** change `Y11S2`, because detection needs no gate and `Y11S2` is unused on main. It adds a comment above `Y11S2` that lists the known Y11S2 builds and says it is not the first one. Moving `Y11S2` to 9718747 is a separate decision for `origin/track/rec-timeline` (its `CodeVersion < Y11S2` timeline gate would then accept early Y11S2 builds whose wire format nobody has checked); noted under Risks.
7. **Acceptance without a real replay**: rewrite the two committed Y11S2 fixtures in memory and require output identical to the unmodified read. No synthetic `.rec` is committed (it would be 3.5 MB or more and would look like real data). Alternative: commit a one-round synthetic fixture; rejected for size and because it proves nothing the in-memory test does not.
8. **Scope**: the two tags only. Non-fatal seeks (item 03), the header 10th-player flush (`a17b9f4`, health-dbno), nil rounds in `MatchReader` (item 01) and `Seek`'s overlap miss (item 05) stay out.

## Interactions

- **Item 03 (listener EOF resilience).** Independent; either order. With 03 alone, a 9734089 round finishes with a `warnings` entry for `readPlayer`, but every player still lacks operator, DissectID, spawn and profileID, so kills cannot be attributed by DissectID and op swaps fall back. 02 restores the data; 03 limits the damage of the next tag move. Both touch `dissect/reader.go` (03 the `Read` loop and new fields, 02 one struct field): a trivial textual merge at most.
- **Item 01 (incomplete-match crash).** On bd6ccbe the synthetic 9734089 match panics through 01's mechanism (round 1 returns `io.EOF`, rounds 2 to 9 stay nil, `PlayerStats(nil)`). 02 removes this trigger; 01 removes the panic in general.
- **Item 05 (pattern search).** 02 does not use `Seek` or the worker scanner for detection (`bytes.Contains` only), so 05 does not change its results. `Seek`'s naive walk (on a mismatch it does not re-test the current byte against `pattern[0]`) applies equally to both tag sets; fixing it is 05's.
- **Item 13 (operator fallback from header roleName).** Complementary: 13 helps when an operator ID is unknown; 02 is about reading the ID at all.
- **`origin/fork/health-dbno` (PR #3).** Does not touch `dissect/player.go`. The plan's diff applies except one context hunk in `dissect/reader.go` (health-dbno re-aligned the struct comments); after adding the field by hand, `go test ./dissect/...` passes there with no golden change (checked). Its `a17b9f4` covers aa-sasha's `bfcff23` header flush.
- **`origin/track/rec-timeline` (PR #10).** Changes only `addUpdate` calls in `dissect/player.go`. The plan's diff applies cleanly and `go test ./dissect/...` (including `dissect/wire`) passes (checked). Its timeline refuses `CodeVersion < Y11S2` (9838928), so a 9734089 replay will parse but get no timeline; unchanged by this item.
- **`ElyasYassin/fix/deterministic-scoreboard-reconcile` (PR #12).** No change to these files.
- **JSON output / R6Stats.** No schema change. Output for every build we have is byte-identical (goldens unchanged; 42 real rounds identical). 9734089 matches go from a failed job (CLI panic, exit 2) to a normal result.
- **Golden-test churn: none.** `go test ./...` passes at bd6ccbe with the prototype, without `-update`.

## Implementation plan

### File structure

- Create `dissect/player_markers.go`: the tag sets, `detectPlayerMarkers`, `(*Reader).playerMarkers`.
- Create `dissect/player_markers_test.go`: the test tag bytes and `TestDetectPlayerMarkers` (pure function table).
- Create `dissect/player_retag_test.go`: `TestReadPlayer_Y11S2Alpha03Tags` (fixture rewrite).
- Modify `dissect/player.go` (`readPlayer`: the two literals), `dissect/reader.go` (one `Reader` field), `dissect/version.go` (comment only).

All tests are internal (`package dissect`) because they read `r.b` and the unexported function.

### Task 1: tag detection

**Files:**
- Create: `dissect/player_markers_test.go`
- Create: `dissect/player_markers.go`

**Interfaces:**
- Produces:
  - `type playerMarkers struct { id []byte; uiID []byte }`
  - `var playerMarkersClassic, playerMarkersY11S2Alpha03 playerMarkers`, `var playerIDMarkerY7S2 []byte`, `var knownPlayerMarkers = []playerMarkers{playerMarkersClassic, playerMarkersY11S2Alpha03}`
  - `func detectPlayerMarkers(b []byte, codeVersion int) (m playerMarkers, ok bool)`
  - test helpers in `dissect/player_markers_test.go`: `testIDClassic`, `testUIClassic`, `testIDAlpha03`, `testUIAlpha03`, `testIDY7S2`, `testRound(parts ...[]byte) []byte`

- [ ] **Step 1: Write the test tag bytes** at the top of `dissect/player_markers_test.go` (package `dissect`, imports `bytes`, `testing`). Task 2's test file uses them too.

```go
// The bytes readPlayer seeks, spelled out so the tests do not depend on the
// variables under test.
var (
	testIDClassic = []byte{0x33, 0xD8, 0x3D, 0x4F, 0x23}
	testUIClassic = []byte{0x38, 0xDF, 0xEE, 0x88}
	testIDAlpha03 = []byte{0x8C, 0x61, 0x1A, 0x75, 0x23}
	testUIAlpha03 = []byte{0x70, 0xFA, 0xAF, 0x28}
	testIDY7S2    = []byte{0xE6, 0xF9, 0x7D, 0x86}
)
```

- [ ] **Step 2: Write the failing table test** in `dissect/player_markers_test.go`:

```go
var testMarkerSlack = bytes.Repeat([]byte{0x11}, 64)

// testRound joins parts with filler bytes, like tags inside a round.
func testRound(parts ...[]byte) []byte { /* slack, part, slack, part, ..., slack */ }

func TestDetectPlayerMarkers(t *testing.T) {
	cases := []struct {
		name        string
		codeVersion int
		round       []byte
		wantID      []byte
		wantUI      []byte
		wantOK      bool
	}{
		{"Y11S2_Alpha03 9718747 uses the classic tags", 9718747, testRound(testIDClassic, testUIClassic), testIDClassic, testUIClassic, true},
		{"Y11S2_Alpha03 9734089 uses its own tags", 9734089, testRound(testIDAlpha03, testUIAlpha03), testIDAlpha03, testUIAlpha03, true},
		{"Y11S2_Alpha04 9785623 reverted to the classic tags", 9785623, testRound(testIDClassic, testUIClassic), testIDClassic, testUIClassic, true},
		{"Y11S3 9950954 uses the classic tags", 9950954, testRound(testIDClassic, testUIClassic), testIDClassic, testUIClassic, true},
		{"the bytes decide, not the build number", 9950954, testRound(testIDAlpha03, testUIAlpha03), testIDAlpha03, testUIAlpha03, true},
		{"the uiID tag is found on its own when only it moved", 9950954, testRound(testIDClassic, testUIAlpha03), testIDClassic, testUIAlpha03, true},
		{"the uiID tag of the set the id tag picked wins", 9734089, testRound(testIDAlpha03, testUIClassic, testUIAlpha03), testIDAlpha03, testUIAlpha03, true},
		{"before Y9S3 there is no uiID to find", Y8S1, testRound(testIDClassic), testIDClassic, testUIClassic, true},
		{"Y7S2 and earlier keep their own id tag", Y7S2, testRound(testIDClassic), testIDY7S2, testUIClassic, true},
		{"an unknown id tag falls back to the classic tags", 9950954, testRound(testUIClassic), testIDClassic, testUIClassic, false},
		{"an unknown uiID tag falls back to the classic tags", 9950954, testRound(testIDClassic), testIDClassic, testUIClassic, false},
	}
	// for each case: got, ok := detectPlayerMarkers(c.round, c.codeVersion);
	// t.Errorf on !bytes.Equal(got.id, c.wantID), !bytes.Equal(got.uiID, c.wantUI), ok != c.wantOK
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `go test ./dissect -run TestDetectPlayerMarkers`
Expected: build failure, `undefined: detectPlayerMarkers`.

- [ ] **Step 4: Implement `dissect/player_markers.go`**

Tag sets as in Global Constraints, each with a comment: classic "used by every known build after Y7S2 except 9734089"; `playerMarkersY11S2Alpha03` "used by build 9734089, a Y11S2_Alpha03 mid-season patch (June 2026); 9718747 before it and 9751808, 9785623 and 9838928 (Y11S2_Alpha04) after it use the classic tags". Struct field comments: `id // followed by the 4-byte DissectID`, `uiID // Y9S3+: the uiID follows 13 bytes after it`.

`detectPlayerMarkers` (the doc comment must say why it is detection, not a gate, and what `ok` means):

```go
func detectPlayerMarkers(b []byte, codeVersion int) (m playerMarkers, ok bool) {
	if codeVersion <= Y7S2 {
		return playerMarkers{id: playerIDMarkerY7S2, uiID: playerMarkersClassic.uiID}, true
	}
	m, ok = playerMarkersClassic, true
	order := knownPlayerMarkers
	found := false
	for i, s := range knownPlayerMarkers {
		if bytes.Contains(b, s.id) {
			m.id, found = s.id, true
			// try this set's uiID tag first, then the others in order
			order = append([]playerMarkers{s}, knownPlayerMarkers[:i]...)
			order = append(order, knownPlayerMarkers[i+1:]...)
			break
		}
	}
	if !found {
		ok = false
	}
	if codeVersion < Y9S3 {
		return m, ok
	}
	for _, s := range order {
		if bytes.Contains(b, s.uiID) {
			return playerMarkers{id: m.id, uiID: s.uiID}, ok
		}
	}
	return m, false
}
```

(`append([]playerMarkers{s}, knownPlayerMarkers[:i]...)` allocates a new slice, so `knownPlayerMarkers` is never modified.)

- [ ] **Step 5: Run it to verify it passes**

Run: `gofmt -l dissect; go vet ./dissect && go test ./dissect -run TestDetectPlayerMarkers -v`
Expected: no gofmt output; 11 subtests PASS.

- [ ] **Step 6: Commit**

```bash
git add dissect/player_markers.go dissect/player_markers_test.go
git commit -m "fix(player): detect the player record tags of a round"
```

### Task 2: read players with the detected tags

**Files:**
- Modify: `dissect/reader.go` (`Reader` struct)
- Modify: `dissect/player.go` (`readPlayer`)
- Modify: `dissect/player_markers.go` (add the method)
- Modify: `dissect/version.go` (comment)
- Create: `dissect/player_retag_test.go` (package `dissect`, imports `bytes`, `encoding/json`, `os`, `testing`)

**Interfaces:**
- Consumes: `detectPlayerMarkers`, `playerMarkers`, the test tag variables (Task 1).
- Produces: `func (r *Reader) playerMarkers() playerMarkers`; field `markers *playerMarkers` on `Reader`.

- [ ] **Step 1: Write the failing acceptance test** in `dissect/player_retag_test.go`:

```go
// readRoundRetagged reads a fixture round. When retag is set, the decompressed
// round is rewritten the way build 9734089 tagged its player records before
// the listeners run, so the result shows what readPlayer does on that build.
func readRoundRetagged(t *testing.T, path string, retag bool) (*Reader, error)
//   os.Open + NewReader (t.Fatal on error); if retag:
//     t.Fatalf if r.b already contains testIDAlpha03 or testUIAlpha03;
//     r.b = bytes.ReplaceAll(r.b, testIDClassic, testIDAlpha03)
//     r.b = bytes.ReplaceAll(r.b, testUIClassic, testUIAlpha03)
//     r.Header.CodeVersion = 9734089
//   return r, r.Read()

// roundJSON marshals {header, matchFeedback} with Header.CodeVersion zeroed.
func roundJSON(t *testing.T, r *Reader) string

// No 9734089 replay is available, so this rewrites real Y11S2_Alpha04 rounds
// with that build's tags and requires the same output as the original.
func TestReadPlayer_Y11S2Alpha03Tags(t *testing.T) {
	for _, path := range []string{
		"test/data/replays/valid/Y11S2/custom_early_plant.rec",
		"test/data/replays/valid/Y11S2/custom_solid_snake.rec",
	} {
		t.Run(path, func(t *testing.T) {
			want, err := readRoundRetagged(t, path, false)
			if !Ok(err) { t.Fatal(err) }
			got, err := readRoundRetagged(t, path, true)
			if err != nil { t.Fatalf("Read() = %v, want nil", err) }
			// t.Fatalf when len(got.Header.Players) != len(want.Header.Players)
			// t.Fatalf when len(got.MatchFeedback) != len(want.MatchFeedback)
			// t.Errorf when roundJSON(t, got) != roundJSON(t, want)
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./dissect -run TestReadPlayer_Y11S2Alpha03Tags`
Expected: FAIL, both subtests: `player_retag_test.go:NN: Read() = EOF, want nil`. (Measured on bd6ccbe plus Task 1.)

- [ ] **Step 3: Add the cache and the method**

`dissect/reader.go`, in `type Reader struct`, after `playersRead int`:
`markers *playerMarkers // player record tags of this round, see playerMarkers`

`dissect/player_markers.go`: `func (r *Reader) playerMarkers() playerMarkers`: if `r.markers == nil`, call `detectPlayerMarkers(r.b, r.Header.CodeVersion)`; when `!ok`, `log.Warn().Str("gameVersion", r.Header.GameVersion).Int("codeVersion", r.Header.CodeVersion).Msg("no known player id or uiID tag in this round")`; store `&m`. Return `*r.markers`. Doc comment: detected on the first call, kept for the rest of the round.

- [ ] **Step 4: Use it in `readPlayer`** (`dissect/player.go`)

Replace
`idIndicator := []byte{0x33, 0xD8, 0x3D, 0x4F, 0x23}` / `if r.Header.CodeVersion <= Y7S2 { idIndicator = []byte{0xE6, 0xF9, 0x7D, 0x86} }`
with `markers := r.playerMarkers()` / `idIndicator := markers.id`, and replace
`r.Seek([]byte{0x38, 0xDF, 0xEE, 0x88})` with `r.Seek(markers.uiID)`. Nothing else in `readPlayer` changes (the `>= Y9S3` gate around the uiID seek stays).

- [ ] **Step 5: Document the Y11S2 builds** in `dissect/version.go`, directly above `Y11S2 int = 9838928`:

```go
	// Y11S2 is a late Y11S2_Alpha04 build, not the first Y11S2 build. Known
	// Y11S2 builds: 9718747 and 9734089 (Y11S2_Alpha03), 9751808, 9785623 and
	// 9838928 (Y11S2_Alpha04). 9734089 used other player record tags; they
	// are detected per round (see detectPlayerMarkers), not gated here.
```

- [ ] **Step 6: Run everything**

Run: `gofmt -l dissect; go vet ./... && go test ./...`
Expected: no gofmt output; `ok` for `dissect` and `dissect/test` (the goldens pass without `-update`); `TestReadPlayer_Y11S2Alpha03Tags` PASS (about 3 s).

- [ ] **Step 7: Commit**

```bash
git add dissect/player.go dissect/reader.go dissect/player_markers.go dissect/player_retag_test.go dissect/version.go
git commit -m "fix(player): read players on Y11S2 build 9734089"
```

### Task 3: corpus check (no code)

- [ ] **Step 1: Unchanged output on real matches.** Build `before` from bd6ccbe and `after` from the branch (`go build -o /tmp/r6-before .` / `/tmp/r6-after`). For each folder in `<scratch>/replays/Match-2026-08-25_*` and `.../replays-new/Match-2026-10-18_*`, run both with `-f json -o <out>.json` and `cmp` the outputs.
Expected: identical files, exit 0, and no `no known player id or uiID tag` line on stderr.
- [ ] **Step 2: When a real 9734089 match is available** (ask in the issue thread; check the R6Stats production uploads), run `after` on it. Expected: exit 0, 10 players per round with non-zero operator IDs and profile IDs, kills in `matchFeedback`, no `large seek` warning from `readPlayer`. If it passes, consider committing one short round as `dissect/test/data/replays/valid/Y11S2/9734089_<name>.rec` with its golden, then drop the `research` label.

## Acceptance

- `go test ./dissect -run TestReadPlayer_Y11S2Alpha03Tags`: red on bd6ccbe (`Read() = EOF, want nil` for both fixtures), green after Task 2.
- `go test ./...` green with no golden change.
- Task 3 Step 1: CLI JSON of the user's Y11S2 and Y11S3 matches unchanged.
- Prototype results (already run, bd6ccbe plus the prototype): 42 of 42 real Y11S2/Y11S3 rounds of the user's corpus read identically after the rewrite; the 9-round synthetic 9734089 match exits 0 with JSON equal to the original except `codeVersion`/`gameVersion`. The prototype also passes `go test ./dissect/...` on `origin/fork/health-dbno` and `origin/track/rec-timeline`.

## Risks / open questions

- **No real 9734089 replay was parsed.** The rewrite assumes only the two tags changed on that build. Evidence: defiancer6s-hub ran fatal spawn and profileID seeks on 43 real 9734089 rounds with only these two tags changed; aa-sasha changed the same two. Alec Wang (`ca5406d`) said the profileID tag "also moved"; likely wrong (his change made that seek non-fatal, which hides the question). If a real replay shows a third tag moved, add it to the set and to the tests.
- **Other 9734089 differences outside `readPlayer`** (for example `readAtkOpSwap`'s `r.Skip(402)` before the uiID) are unknown. A wrong offset there falls back to the player-object match, so it degrades rather than fails.
- **Builds between 9734089 and 9751808** are unknown. Detection covers them whatever tags they use, as long as the tags are known.
- **A new tag set** still fails the round, as today, with one warning line per round. Item 03 makes that failure cost one packet.
- **`Y11S2 = 9838928`** stays mislabelled as a season bound. Whether to move it to 9718747 belongs to PR #10's timeline gate; follow-up candidate, not this item.
- **Stray matches.** A 5-byte classic id tag appearing by chance in a 9734089 round (about 5e-5 per 50 MB round) would pick the classic set; the round would then fail as it does today. Accepted.
