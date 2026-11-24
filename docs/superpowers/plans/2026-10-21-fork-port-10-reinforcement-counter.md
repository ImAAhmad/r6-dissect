# Fork port 10: per-team reinforcements per round (counter `67DE20F8`) Implementation Plan

> **Issue:** #21, #22 · prestonwillis10/r6-dissect · fork survey: `docs/research/2026-10-21-fork-survey.md`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The `--timeline` sidecar of every Y11S2+ round carries a `reinforcements` section: the defending team's pool (initial, remaining, used, cancelled) and one observed event per placement start or cancel, with tick, ms clock and the object handles that changed state. Part B adds an inferred placer per start, but only after a ground-truth check passes.

**Architecture:** A small stateful observer (`reinforceTracker`) is fed every TLV token inside `buildTimeline`, the same way `msClock` is fed today. It follows the one object that writes field `67DE20F8` and the run of `FFFD5262` state tokens that comes right after each counter change. After the walk, it is turned into a `*TimelineReinforcements` using the ms clock and the header's team roles. The legacy JSON and `Timeline.Events` do not change.

**Tech Stack:** Go 1.x, packages `dissect` and `dissect/wire` on `origin/track/rec-timeline` (PR #10). No new dependencies.

**Spec:** This document. Its parent design is `docs/superpowers/specs/2026-10-19-rec-timeline-framework-design.md` (R6Stats repo), which already lists `Reinforce` as a candidate event type gated on verification (§ "Event types v1") and requires additive-only timeline changes.

---

## Summary

ArnaudCabello's fork reads property `67 DE 20 F8` as "reinforcements remaining". I checked this on our `track/rec-timeline` TLV decoder against 47 Y11S2/Y11S3 rounds (5 fixtures and 42 rounds from the user's matches). The hypothesis holds, and it is cleaner than the survey assumed:

- **There is exactly one counter object per round.** It belongs to the defending team. Its handle stays the same across the rounds of a match and changes exactly when the sides swap, so each team has its own counter, but only the defenders' counter ever changes. The survey's guess that two teams' counters are interleaved is **wrong**. The fork's "remaining 4 twice" comes from a **cancelled placement**: the counter goes 5→4, then back up to 5 (+1, 0.3–2.9 s later), then 5→4 again.
- **The count is bounded and plausible.** The initial value is 10 in 47/47 rounds. It sits in the pre-TLV snapshot as `22 67DE20F8 04 0A000000`. TLV values only move between 9 and 0, in steps of ±1, except for one 2-step drop where two placements landed in the same frame. Used per round (initial − final) ranges from 3 to 10.
- **Every counter token is followed immediately, in the same tick, by an `FFFD5262` state token on another object:** 399/399 tokens, direction-consistent in every case (value 1 on a decrement, back to 0 on the same object on a cancel). On the 2-step drop, two state tokens follow. These objects are round-local handles, probably the reinforced surfaces (guess).
- **There is no deterministic player link in the graph.** A strong statistical candidate exists: player-object field `A4DC8DD4` goes to `00` on a defender 0–70 ticks after 343/345 decrements, against 9–15 % in control windows at other offsets, and the candidate is unique in 305/345. It has no ground truth, so it is Part B with the `research` label.

Verdict: **FILE**, as two issues. 10a is the observed team counter: enhancement, ready to build. 10b is placer attribution: enhancement plus research, gated on a manual ground-truth check.

## Source

- Fork `ArnaudCabello/reinforcements`, commit `c7bd4b6d18ae02bd4326100334c223eaa6e4cd81` ("feat: reinforcement tracking from binary counter and position gap detection").
  - `dissect/reader.go`: `r.Listen([]byte{0x67, 0xDE, 0x20, 0xF8}, readReinforcementCounter)`.
  - `dissect/reinforcement.go`: `readReinforcementCounter` keeps one global `r.lastReinfCount`, and `if int(remaining) < prev` appends a `Reinforcement`. `resolveReinforcementPlayers` matches each placement to a 3.0–5.5 s gap in a defender's position stream.
  - Example output `examples/Match-2026-03-29_15-42-18-22800-R01.json`: 9 entries `remaining` 9…1 with usernames.
- Flaws:
  1. **It counts decrements, not net use.** A cancelled placement is counted twice. On `custom_fortress_plant` it would report 10 placements; the pool actually used is 8.
  2. **A 2-step drop counts as one placement** (it appends one entry per token).
  3. **It has no object attribution.** It is a raw byte-pattern listener, so a second writer of the field would corrupt it silently. On our corpus there is only one writer, but the fork never checks that.
  4. **Player attribution is a timing heuristic over position gaps.** The commit message says it is about 48 % accurate. It also needs the fork's position decoder, which we do not have.
  5. Fork code was read, not built or run (brief safety rule).

## Current behaviour

At `bd6ccbe` (main) nothing reads the field. `git grep -n -i -e 67de20f8 -e "0x67, 0xde" -e reinforc bd6ccbe -- dissect main.go` returns nothing, and the same grep on `fdaab61` (`origin/track/rec-timeline`) also returns nothing. Main has no TLV decoder, so this plan targets `track/rec-timeline`.

On `track/rec-timeline` the TLV walk already exposes the field with object attribution. Reproduction (run in a scratch clone at `fdaab61`):

```bash
go run ./cmd/dissect-lab tlv dissect/test/data/replays/valid/Y11S3/custom_fortress_plant.rec | grep '"field":"67de20f8"'
```

The output is 12 lines, all `"op":"valueNamed","object":"0f7305f000000000","field":"67de20f8","len":4`, with values 9,8,7,6,5,4,5,4,3,4,3,2 at ticks 939…7466. Values per fixture:

| Fixture | Defense team | Counter values (TLV) | Used (10 − last) | Cancels |
|---|---|---|---:|---:|
| Y11S2/custom_early_plant | 1 | 9 8 7 | 3 | 0 |
| Y11S2/custom_solid_snake | 1 | 9 8 7 6 5 4 3 2 | 8 | 0 |
| Y11S3/custom_clubhouse_short | 0 | 9 8 7 6 5 4 3 2 1 0 | 10 | 0 |
| Y11S3/custom_fortress_plant | 1 | 9 8 7 6 5 4 5 4 3 4 3 2 | 8 | 2 |
| Y11S3/custom_lair_plant_stop | 1 | 9 8 7 6 5 4 3 2 1 | 9 | 0 |

The context around one change (fortress, tick 4670→4898) shows the cancel pattern:

```
tick 4670 0f7305f0 67de20f8 = 4    then 26c304f0 fffd5262 = 1
tick 4771 0f7305f0 67de20f8 = 5    then 26c304f0 fffd5262 = 0   (cancel: same object back to 0)
tick 4898 0f7305f0 67de20f8 = 4    then 26c304f0 fffd5262 = 1
```

The coalesced drop (user match `Match-2026-10-18_19-08-03-14012-R02.rec`, tick 3102) looks like this: `67de20f8` goes 8→6, followed by `beab09f0 fffd5262 = 1` and `bfab09f0 fffd5262 = 1`.

The corpus probe was a throwaway internal test in the scratch clone (`dissect/reinf_probe_test.go`, not part of this plan). It gave these results over 47 rounds:
- 47/47 rounds have exactly one object writing `67DE20F8`. That object is never the game-state object (the one with `1F07EFC9`), and no TLV reference edge targets it (`Graph.Parent` is empty).
- Per match, the handle switches exactly at the side swap (for example `0f7305f0` for R01–R05, then `86430ff0` for R06–R07 of `Match-2026-10-18_18-19-54`).
- The initial value is found by searching `22 67 DE 20 F8 04` before the first TLV envelope start: 10 in 47/47 rounds. In 46 rounds it is in the prelude. In `Match-2026-08-25_19-56-00-32000-R01` it is at offset 53082, past the prelude (33170) but before the first TLV envelope.
- 399/399 counter tokens are followed in the same tick by an `FFFD5262` state token, 1 on a down step and 0 on an up step.
- 15 up steps (cancels) in 47 rounds, each 0.3–2.9 s after the preceding down step and in the Prep or Action phase. The counter never goes above its initial value or below 0.
- Placements happen in both Prep and Action.
- Object handles are round-local: the same site in R01 and R04 of one match uses different handle ranges. They have no class in the graph. So `objects` is an opaque per-round id, not a stable wall id.
- The field also exists in Y8S1/Y9S1 fixtures (raw byte counts 1/5/9/9). That path is not covered here, because the timeline requires Y11S2+.

## Approaches considered

1. **Legacy listener, like the fork.** Add `r.Listen([]byte{0x67,0xDE,0x20,0xF8}, …)` in `reader.go` and write a stats field into the round JSON.
   - Pro: it works on Y8–Y11 and in production today (R6Stats `api/r6stats/services/dissect.py` runs `r6-dissect <folder>` without `--timeline`).
   - Con: it has no object attribution and cannot see the paired state token. It changes the legacy JSON shape, which the timeline design forbids ("the existing JSON never changes shape"). It would also duplicate the structural path later.
2. **Timeline section from the TLV walk (chosen).** A tracker fed by `DecodeTLV` in `buildTimeline` writes `Timeline.Reinforcements`.
   - Pro: object attribution, a multi-writer guard, paired object handles, the ms clock, additive sidecar only, and no golden churn.
   - Con: Y11S2+ only, and R6Stats does not see it until it ingests the sidecar (M2/M3 of the timeline design).
3. **New `MatchUpdateType`s (`Reinforce`, `ReinforceCancel`) merged into `Timeline.Events`.**
   - Pro: one event table in R6Stats, which matches the spec's candidate list.
   - Con: it breaks the M2 oracle. `TestTimeline_Parity` asserts `len(tl.Events) == len(r.MatchFeedback)` with per-index type equality, and `main_test.go TestWriteRound_Sidecar` asserts "round and sidecar disagree on the event count". `Seq` would lose its meaning as an index into `matchFeedback`, and the enum ID space shared with the legacy JSON would grow.
4. **Derive placements from `FFFD5262` transitions alone.**
   - Con: `FFFD5262` is a generic state field (it also takes value 2 on unrelated objects, for example `44c904f0`). Only the counter scopes it.

## Decision and rationale

These are my calls, answering the brainstorming questions myself as the brief allows:

- **D1, where it lives:** approach 2, in a dedicated `reinforcements` section of the sidecar. The events stay out of `Timeline.Events`, so legacy parity (the M2 oracle) stays intact. When the M3 "promoted field" machinery (`fields.yaml`) exists, these events can be mirrored into `Events` under a timeline version bump. That is out of scope here.
- **D2, team:** the section names the defending team by header index (`Teams[i].Role == Defense`), not by counter-object handle. The handle has no graph link to a team; the header role is observed. If no team has the Defense role, use `team: -1`.
- **D3, count semantics:**
  - `used = initial − remaining`, the authoritative pool consumption.
  - `cancelled` = sum of up-step sizes.
  - `started` = sum of down-step sizes.
  - Invariant: `started − cancelled == used`.
  - Each event carries `count`, which can be greater than 1 (the coalesced frame).
- **D4, initial:** read from the first `22 67 DE 20 F8 04 <u32>` before the first TLV envelope start (`initialStatus: "observed"`). If it is missing, use `first TLV value + its step` (`initialStatus: "assumed"`), which means "first value + 1" on the corpus. Never hard-code 10: custom game settings may differ, and nothing here proves they don't.
- **D5, objects:** an event's `objects` are the handles of the contiguous run of `FFFD5262` value tokens that follows the counter token in the same tick. "Contiguous" means tokens that are on the counter object itself are skipped, and any other token ends the run. At most `count` handles are collected. Missing handles are not an error; the event still counts.
- **D6, multiple writers:** if a second object ever writes `67DE20F8` in a round, the section is omitted (`nil`) and `Coverage` gains nothing. Never guess. This never happened in the corpus.
- **D7, status:** the team counter facts are `"observed"`. The Part B player is `"inferred"` (the timeline design's anti-goal: inferred values must say so).
- **D8, Part B is gated:** the `A4DC8DD4` placer link ships only after a human ground-truth check on one round (task B2). Until then, only the lab command exists.
- **D9, no R6Stats change here.** R6Stats ingestion of the sidecar is the timeline design's M2/M3 work. A follow-up R6Stats issue ("store per-round reinforcements used from the timeline sidecar") can be filed once that ingestion lands.

## Interactions

- **PR #10 `origin/track/rec-timeline`:** the hard dependency. Implement on a branch cut from it, or from main once it is merged. Everything here touches `dissect/timeline.go` (the `buildTimeline` callback and the `Timeline` struct).
- **PR #3 `fork/health-dbno`:** it adds `gmSettings` on Y11S2+. That may hold a reinforcement-pool setting for custom games; D4 makes this irrelevant. There are no file conflicts beyond `reader.go`, which this plan does not touch.
- **PR #12 (ElyasYassin scoreboard reconcile):** no interaction.
- **Other items in this batch:**
  - 11 (weapon per kill) and 12 (damage from the health stream) are also likely to be TLV field observers in `buildTimeline`. Expect trivial merge conflicts in the same `DecodeTLV` callback and `Timeline` struct; whichever lands second adds one line.
  - 03 (listener EOF) and 05 (pattern search across blocks) affect only the legacy listener path, which this plan avoids.
  - 09 (scoreboard entity link) has no overlap.
  - 14 (trade stats) has no overlap.
- **JSON / R6Stats:**
  - Legacy round JSON: unchanged (no golden churn in `*.rec.json`).
  - Sidecar: new optional key `reinforcements`. `version` stays 1, since additions are allowed by the design.
  - R6Stats production does not pass `--timeline`, so there is no consumer impact today.
- **Golden tests:** none regenerate. The new assertions are property tests over the fixtures, like `dissect/test/timeline_test.go`.

## Global Constraints

- Base branch: `origin/track/rec-timeline` (`fdaab61` or later). Go: `export PATH=$HOME/.local/go/bin:$PATH`.
- `dissect/wire` carries no game semantics and must not import `dissect`: field IDs and the tracker live in package `dissect`.
- Field IDs, in file byte order: counter `wire.FieldID{0x67, 0xde, 0x20, 0xf8}`; state `wire.FieldID{0xff, 0xfd, 0x52, 0x62}`; Part B player flag `wire.FieldID{0xa4, 0xdc, 0x8d, 0xd4}`.
- Snapshot initial pattern: bytes `22 67 de 20 f8 04` followed by a little-endian u32.
- JSON keys are camelCase. Every status string is one of `"observed"`, `"assumed"` (initial only) or `"inferred"`.
- `Timeline.Events`, `Timeline.Version` and the legacy JSON stay unchanged.

## Review Focus

1. **A cancel at the very first token** (the counter goes up from the snapshot value) must not produce a negative `started`, and `used` must stay `initial − remaining`. Test: `TestReinforceTracker_CancelAfterFirst` (Task 1).
2. **A coalesced 2-step drop** must count 2 and collect 2 object handles. Test: `TestReinforceTracker_CoalescedDrop` (Task 1).
3. **A round where defenders place nothing** (snapshot initial found, no TLV tokens) must emit `used: 0`, `events: []`, not omit the section. Test: `TestReinforcementsResult_NoPlacements` (Task 2).
4. **A second object writing the field** must omit the section. Test: `TestReinforceTracker_SecondWriterVoids` (Task 1).
5. **State tokens on unrelated objects in a later tick, or after a non-state token,** must not be attached to the previous event. Test: `TestReinforceTracker_ObjectRunStops` (Task 1).

---

## File Structure

- Create `dissect/reinforce.go`: field IDs, `reinforceTracker` (token observer), `snapshotInitial`, and conversion to output types.
- Create `dissect/reinforce_internal_test.go`: unit tests on synthetic tokens.
- Modify `dissect/timeline.go`: the `Timeline` struct gains `Reinforcements`, the output types, and wiring in `buildTimeline`.
- Modify `dissect/test/timeline_test.go`: fixture property test `TestTimeline_Reinforcements`.
- Modify `cmd/dissect-lab/commands.go` and `main.go`: subcommand `reinforce` (Part B research tool; Task B1).
- Modify `docs/wire-coverage.md`: add a short "Known fields" note for `67DE20F8` and `FFFD5262` with the evidence numbers above (Task 3).

---

## Part A: observed team counter (issue 10a)

### Task 1: `reinforceTracker` token observer

**Files:**
- Create: `dissect/reinforce.go`
- Test: `dissect/reinforce_internal_test.go`

**Interfaces:**
- Consumes: `wire.Token`, `wire.Handle`, `wire.FieldID`, `wire.OpValueNamed` (`dissect/wire/tlv.go`).
- Produces:
  ```go
  var fieldReinforcementsLeft = wire.FieldID{0x67, 0xde, 0x20, 0xf8}
  var fieldObjectState        = wire.FieldID{0xff, 0xfd, 0x52, 0x62}

  type reinforceObs struct {
      Tick      uint32
      Delta     int           // new value − previous value; previous is the initial for the first token
      Remaining int           // the new value
      Objects   []wire.Handle // FFFD5262 run that followed in the same tick (≤ |Delta|)
  }
  type reinforceTracker struct { /* unexported state */ }
  func newReinforceTracker(initial int, haveInitial bool) *reinforceTracker
  func (t *reinforceTracker) observe(tick uint32, tok wire.Token)
  func (t *reinforceTracker) observations() (obs []reinforceObs, initial int, initialObserved bool, ok bool)
  ```
  `ok` is false when two distinct objects wrote the counter (D6). With no counter tokens: `ok` is true, `obs` is empty, and `initial` is as given.

- [ ] **Step 1: Write the failing tests** in `dissect/reinforce_internal_test.go`. Use a helper `u32Tok(obj wire.Handle, f wire.FieldID, v uint32) wire.Token` (an `OpValueNamed` token with a 4-byte LE value, like `clockTokOn` in `dissect/msclock_test.go`). Handles: `ctr := wire.Handle{0x0f, 0x73, 0x05, 0xf0}`, `w1 := wire.Handle{0x26, 0xc3, 0x04, 0xf0}`, `w2 := wire.Handle{0x27, 0xc3, 0x04, 0xf0}`.
  - `TestReinforceTracker_FortressSequence`:
    - Setup: `newReinforceTracker(10, true)`. Feed values 9,8,7,6,5,4,5,4,3,4,3,2 at ticks 100, 200, …, 1200. Each one is followed in the same tick by `u32Tok(w1, fieldObjectState, 1)` on a down step and `0` on an up step.
    - Assert `ok`, `initial == 10`, `initialObserved`, and `len(obs) == 12`.
    - Assert the Deltas are `[-1,-1,-1,-1,-1,-1,+1,-1,-1,+1,-1,-1]`, `obs[11].Remaining == 2`, and every `obs[i].Objects == []wire.Handle{w1}`.
  - `TestReinforceTracker_CoalescedDrop`:
    - Setup: initial 10. Feed 9@10, 8@20, then at tick 30: counter 6, `w1` state 1, `w2` state 1.
    - Assert `obs[2].Delta == -2` and `obs[2].Objects == []wire.Handle{w1, w2}`.
  - `TestReinforceTracker_CancelAfterFirst`:
    - Setup: initial 10. Feed 9@10, then 10@20 (cancel).
    - Assert Deltas `[-1, +1]` and `obs[1].Remaining == 10`.
  - `TestReinforceTracker_SecondWriterVoids`: counter 9 on `ctr`, then counter 8 on `w2`. Assert `ok == false`.
  - `TestReinforceTracker_ObjectRunStops`. At tick 10: counter 8 (initial 10, so Delta −2), then a token on `ctr` with field `{0xfc,0xc6,0xa8,0x60}` (skipped, as it is on the counter object), then `w1` state 1, then an unrelated token `u32Tok(w2, wire.FieldID{1,2,3,4}, 0)` (ends the run), then `w2` state 1 in the same tick. Assert `obs[0].Objects == []wire.Handle{w1}`. A further `w2` state token at tick 11 also stays unattached.
  - `TestReinforceTracker_NoTokens`: `newReinforceTracker(10, true)` with no tokens. Assert `ok`, `len(obs) == 0` and `initial == 10`.
  - `TestReinforceTracker_AssumedInitial`: `newReinforceTracker(0, false)`, feed 9@10 and 8@20. Assert `initial == 10`, `!initialObserved` and `obs[0].Delta == -1`.

- [ ] **Step 2: Run to verify they fail**
  Run: `go test ./dissect -run TestReinforceTracker -count=1`
  Expected: FAIL (build error: `undefined: newReinforceTracker`).

- [ ] **Step 3: Implement** `dissect/reinforce.go`.
  - `observe` ignores tokens that are not value ops (`tok.Op.IsValue()`).
  - On a 4-byte `fieldReinforcementsLeft` token:
    - The first such token fixes the counter object. A token on any other object sets a `voided` flag.
    - Previous value = the last value, or the initial for the first token.
    - When `!haveInitial` on the first token, set `initial = v + 1`. This assumes a first-token down step of 1, as on the whole corpus.
    - Append a `reinforceObs` and open the "run" with `want = |Delta|` and `runTick = tick`.
  - While a run is open:
    - A token with `tick != runTick` closes it.
    - A token on the counter object is skipped.
    - A 4-byte `fieldObjectState` value token appends `tok.Object` to the last observation's `Objects`, and closes the run when `len == want`.
    - Any other token closes it.
  - Comment the `+1` assumption next to the code.

- [ ] **Step 4: Run to verify they pass**
  Run: `go test ./dissect -run TestReinforceTracker -count=1 -v`
  Expected: PASS, 7 tests.

- [ ] **Step 5: Commit**
  `git add dissect/reinforce.go dissect/reinforce_internal_test.go && git commit -m "feat(timeline): reinforcement counter observer for field 67DE20F8"`

### Task 2: output types, snapshot initial, result conversion, wiring

**Files:**
- Modify: `dissect/reinforce.go`, `dissect/timeline.go`
- Test: `dissect/reinforce_internal_test.go`, `dissect/test/timeline_test.go`

**Interfaces:**
- Consumes: Task 1's `reinforceTracker`. Also `msClock.at(tick) (RoundPhase, uint32, int64, bool)` (`dissect/msclock.go`), `tickIndex.spans` (sorted by `start`, `dissect/timeline.go`), and `r.Header.Teams[i].Role`.
- Produces (in `dissect/timeline.go`, next to `TimelineCoverage`):
  ```go
  type TimelineReinforcements struct {
      Team          int                     `json:"team"`          // header team index of the defenders, -1 if unknown
      Initial       int                     `json:"initial"`
      InitialStatus string                  `json:"initialStatus"` // "observed" | "assumed"
      Remaining     int                     `json:"remaining"`
      Used          int                     `json:"used"`          // Initial − Remaining
      Started       int                     `json:"started"`
      Cancelled     int                     `json:"cancelled"`
      Events        []TimelineReinforcement `json:"events"`
      Status        string                  `json:"status"`        // "observed"
  }
  type TimelineReinforcement struct {
      Tick         uint32     `json:"tick"`
      ElapsedMs    int64      `json:"elapsedMs"`
      RoundClockMs uint32     `json:"roundClockMs"`
      Phase        RoundPhase `json:"phase,omitempty"`
      Kind         string     `json:"kind"`              // "start" | "cancel"
      Count        int        `json:"count"`             // |delta|
      Remaining    int        `json:"remaining"`
      Objects      []string   `json:"objects,omitempty"` // wire.Handle.String()
      Status       string     `json:"status"`            // "observed"
  }
  ```
  - `Timeline` gains `Reinforcements *TimelineReinforcements `json:"reinforcements,omitempty"``, placed after `Positions`.
  - In `dissect/reinforce.go`:
    - `func snapshotInitial(b []byte, limit int) (int, bool)`: index of the pattern in `b[:limit]`, then the u32 after it.
    - `func reinforcementsResult(t *reinforceTracker, clock *msClock, team int) *TimelineReinforcements`: returns `nil` when `!ok`. Returns `nil` when there are no observations and no observed initial (the field is absent). Otherwise returns a non-nil section with `Events` non-nil (may be empty).

- [ ] **Step 1: Write the failing unit tests** in `dissect/reinforce_internal_test.go`:
  - `TestSnapshotInitial`:
    - `b := append([]byte{0,0,0x22,0x67,0xde,0x20,0xf8,0x04,0x0a,0,0,0}, 0x22,0x67,0xde,0x20,0xf8,0x04,0x07,0,0,0)`.
    - `snapshotInitial(b, len(b))` returns `(10, true)`.
    - `snapshotInitial(b, 5)` returns `(0, false)`.
    - `snapshotInitial(b[:10], 10)` (the u32 is cut off) returns `(0, false)`.
  - `TestReinforcementsResult_Fortress`:
    - Setup: the tracker from the fortress sequence of Task 1, and a clock built with `clockTok` as in `dissect/msclock_test.go` (`fieldRoundTimer` at tick 1, `fieldRoundClockMs` 44000 at tick 50).
    - Assert `Initial 10`, `InitialStatus "observed"`, `Remaining 2`, `Used 8`, `Started 10`, `Cancelled 2`, `len(Events) 12`, and `Status "observed"`.
    - Assert `Events[6].Kind == "cancel"` and `Events[6].Count == 1`.
    - Assert `Events[0].Phase == PrepPhase` and `Events[0].RoundClockMs == 44000`.
    - Assert `Events[0].Objects == []string{"26c304f000000000"}`.
  - `TestReinforcementsResult_NoPlacements`: `newReinforceTracker(10, true)` with no tokens. Assert a non-nil result, `Used 0`, `Remaining 10`, and `Events` not nil with length 0.
  - `TestReinforcementsResult_Absent`: `newReinforceTracker(0, false)` with no tokens returns `nil`.
  - `TestReinforcementsResult_Voided`: a tracker with two writers returns `nil`.

- [ ] **Step 2: Write the failing fixture test** `TestTimeline_Reinforcements` in `dissect/test/timeline_test.go`. Make it table-driven over the five fixtures, with the values from the "Current behaviour" table:

  | path | team | used | cancelled | events |
  |---|---:|---:|---:|---:|
  | `data/replays/valid/Y11S2/custom_early_plant.rec` | 1 | 3 | 0 | 3 |
  | `data/replays/valid/Y11S2/custom_solid_snake.rec` | 1 | 8 | 0 | 8 |
  | `data/replays/valid/Y11S3/custom_clubhouse_short.rec` | 0 | 10 | 0 | 10 |
  | `data/replays/valid/Y11S3/custom_fortress_plant.rec` | 1 | 8 | 2 | 12 |
  | `data/replays/valid/Y11S3/custom_lair_plant_stop.rec` | 1 | 9 | 0 | 9 |

  For each fixture, assert:
  - `tl.Reinforcements != nil`, `Initial == 10`, `InitialStatus == "observed"`, and `Used == Initial - Remaining`.
  - `Started - Cancelled == Used`.
  - `r.Header.Teams[Team].Role == dissect.Defense`.
  - Event ticks are non-decreasing and inside `[tl.Clock.TickFirst, tl.Clock.TickLast]`.
  - Every event has `len(Objects) == Count`.
  - Every event's `Phase` is `PrepPhase` or `ActionPhase`.

  Also extend `TestTimeline_LegacyLayout` to assert nothing new; the timeline is nil there already.

- [ ] **Step 3: Run to verify they fail**
  Run: `go test ./dissect -run 'TestSnapshotInitial|TestReinforcementsResult' -count=1 && go test ./dissect/test -run TestTimeline_Reinforcements -count=1`
  Expected: FAIL (build errors `undefined: snapshotInitial`, `tl.Reinforcements undefined`).

- [ ] **Step 4: Implement.**
  - In `buildTimeline` (`dissect/timeline.go`), after `idx` is checked non-empty:
    - `ini, hasIni := snapshotInitial(r.b, idx.spans[0].start)`.
    - `reinf := newReinforceTracker(ini, hasIni)`.
    - Add `reinf.observe(tick, t)` to the existing `DecodeTLV` callback, next to `clock.observe` and `graph.Add`.
    - After `tl` is built: `tl.Reinforcements = reinforcementsResult(reinf, clock, defenseTeam(r))`.
  - Add `func defenseTeam(r *Reader) int`: the first index with `Role == Defense`, else -1.
  - In `reinforcementsResult`:
    - `Kind` is `"start"` for Delta < 0 and `"cancel"` for Delta > 0; `Count` is `|Delta|`.
    - Time fields come from `clock.at(tick)`. When `!ok`, leave the zero values and an empty phase.
    - `Started` / `Cancelled` are sums of `Count` by kind.

- [ ] **Step 5: Run to verify they pass, plus the existing suites**
  Run: `go test ./... -count=1`
  Expected: PASS. This includes `TestTimeline_Parity`, `TestWriteRound_Sidecar` and `TestReader_Read`, with no `-update` needed and no `.rec.json` change (`git status --short dissect/test/data` is empty).

- [ ] **Step 6: Commit**
  `git add dissect/reinforce.go dissect/reinforce_internal_test.go dissect/timeline.go dissect/test/timeline_test.go && git commit -m "feat(timeline): per-round defending-team reinforcements section in the sidecar"`

### Task 3: sidecar smoke check and field note

**Files:**
- Modify: `main_test.go` (`TestWriteRound_Sidecar`), `docs/wire-coverage.md`

**Interfaces:**
- Consumes: `dissect.Timeline.Reinforcements` (Task 2).

- [ ] **Step 1: Extend `TestWriteRound_Sidecar`.** After the sidecar is decoded, assert `tl.Reinforcements != nil && tl.Reinforcements.Used == 10` (clubhouse uses 10). Also assert that the raw sidecar bytes contain `"reinforcements":{"team":0`.
- [ ] **Step 2: Run it**
  Run: `go test . -run TestWriteRound_Sidecar -count=1`
  Expected: PASS.
- [ ] **Step 3: Add a "Known fields" section to `docs/wire-coverage.md`.** Give one row each for `67DE20F8` (defending-team reinforcements remaining, u32, one writer per round, initial in the snapshot) and `FFFD5262` (generic object state; 1/0 on the object reinforced right after a counter change). Cite this plan and the 47-round corpus numbers from "Current behaviour".
- [ ] **Step 4: Commit**
  `git add main_test.go docs/wire-coverage.md && git commit -m "test(timeline): sidecar carries reinforcements; document 67DE20F8/FFFD5262"`

Part A is done when `go test ./... -count=1` passes and this prints the expected values:

```bash
go run . --timeline /tmp/tl dissect/test/data/replays/valid/Y11S3/custom_fortress_plant.rec > /dev/null && jq -c '.reinforcements|{team,initial,used,cancelled,n:(.events|length)}' /tmp/tl/custom_fortress_plant.timeline.json
```

It should print `{"team":1,"initial":10,"used":8,"cancelled":2,"n":12}`.

---

## Part B: inferred placer (issue 10b, `research`)

Evidence so far, from the probe on 42 user rounds (345 down steps):

- Player-object field `A4DC8DD4` is a 1-byte value on the player object itself; the handle is in `r.inventory.players`.
- In 343/345 down steps, at least one **defender** has an `A4DC8DD4 = 00` token within ticks `[t, t+70]` (about 0–0.3 s) of the counter token at tick `t`. Control windows at other offsets show this only 9–15 % of the time.
- Exactly one defender candidate: 305/345.
- The earliest-candidate rule yields 1–6 placements per player per round.
- Zero placements are attributed to an already-dead player (6 down steps happened after a defender death).
- The field also fires for attackers in other contexts, so it is a generic player flag, not a reinforcement flag. Its meaning is unknown.

### Task B1: `dissect-lab reinforce` (research output)

**Files:**
- Modify: `cmd/dissect-lab/commands.go`, `cmd/dissect-lab/main.go` (add `"reinforce"` to `commandList` and the switch)
- Test: `cmd/dissect-lab/main_test.go`

**Interfaces:**
- Consumes: `readTimeline(path)` (`cmd/dissect-lab/commands.go`), `tl.Reinforcements` and `tl.Entities` (player handles to usernames), plus a second `wire.DecodeTLV` pass over the buffer `readTimeline` returns.
- Produces: one JSON line per reinforcement event:
  `{"tick":…, "kind":"start", "count":1, "remaining":8, "objects":[…], "candidates":[{"player":"player55","dt":0}, …]}`
  - `candidates` lists the players with an `A4DC8DD4` value token `00` in `[tick, tick+70]`, sorted by `dt`.
  - The command does not filter by team; the reviewer judges.
- [ ] **Step 1: Test** `TestReinforce_Fortress`: run `run([]string{"reinforce", fortressPath}, &out, &errb)` and assert exit 0, 12 lines, and that line 7 has `"kind":"cancel"`.
- [ ] **Step 2:** Run `go test ./cmd/dissect-lab -run TestReinforce -count=1`. Expected: FAIL (unknown command).
- [ ] **Step 3:** Implement `(*lab).reinforce` following the `graph` command's pattern.
- [ ] **Step 4:** Run it again. Expected: PASS.
- [ ] **Step 5:** Commit `feat(dissect-lab): reinforce command lists counter events with placer candidates`.

### Task B2: ground-truth gate (human, blocking)

- [ ] Run `go run ./cmd/dissect-lab reinforce <Match-2026-10-18_19-08-03-14012-R02.rec>` and hand the output to Preston.
- [ ] Preston replays that round in the Siege replay viewer and notes who started each reinforcement in Prep (10 placements, defenders player43, player06, player76, player61, player20). Then the same for `Match-2026-10-18_18-19-54-14012-R01.rec`, which has the two cancels.
- [ ] **Gate:** proceed to B3 only if the earliest-defender-candidate rule matches at least 18 of 20 placements. If it fails, close 10b with an eval note recording the miss pattern, and do not ship attribution.

### Task B3: emit the inferred placer (only after B2 passes)

**Files:**
- Modify: `dissect/reinforce.go`, `dissect/timeline.go`, tests as in Task 2

**Interfaces:**
- `TimelineReinforcement` gains `Player string `json:"player,omitempty"`` and `PlayerStatus string `json:"playerStatus,omitempty"`` (always `"inferred"` when `Player` is set).
- `reinforceTracker` additionally records, per player handle, the ticks of `A4DC8DD4` value tokens equal to `00`. Add an exported-nothing helper `func (t *reinforceTracker) placer(tick uint32, defenders map[wire.Handle]string) string`. It returns the defender with the earliest such token in `[tick, tick+70]`, or `""` when two defenders tie on the earliest tick. Constant: `const placerWindowTicks = 70`.
- Players: `r.inventory.players` (handle `wire.HandleOf(h)` → `p.username`), filtered to defenders through `r.Header.Players[i].TeamIndex == team`. Only `"start"` events get a placer.
- [ ] Tests:
  - `TestReinforceTracker_Placer` (synthetic): unique candidate, tie leaves it empty, outside the window leaves it empty, an attacker is ignored.
  - Extend `TestTimeline_Reinforcements`: every start event's `Player` is a defender or `""`, and `PlayerStatus == "inferred"` whenever `Player != ""`.
- [ ] Run `go test ./... -count=1`. Expected: PASS.
- [ ] Commit `feat(timeline): inferred reinforcement placer (A4DC8DD4 window, gated on ground truth)`.

## Acceptance

- **10a:** `go test ./dissect -run 'TestReinforce|TestSnapshotInitial|TestReinforcementsResult' -count=1` and `go test ./dissect/test -run TestTimeline_Reinforcements -count=1` are red today (undefined symbols) and green when done. The `jq` command at the end of Part A prints `{"team":1,"initial":10,"used":8,"cancelled":2,"n":12}`. `go test ./... -count=1` stays green, with no golden diffs.
- **10b:** B2's gate passes (≥ 18/20 placements), then `TestReinforceTracker_Placer` is green.

## Risks / open questions

- **Meaning of a cancel:** the +1 step is consistent with an interrupted placement (same object back to state 0, re-placed shortly after), but this is inferred. The counts don't depend on the interpretation, because `used = initial − remaining` either way.
- **Objects are opaque round-local handles.** Turning them into "which wall" (room or position) needs the snapshot or map-object decoding, which is outside the timeline's current scope.
- **Other modes and versions:** the corpus is all Bomb, Y11S2/Y11S3. Secure Area, Hostage, Arcade and older versions are unverified. The multi-writer guard (D6) and `initialStatus` keep a surprise visible rather than silent.
- **Legacy path:** if R6Stats wants the number before it ingests the sidecar, a later item could add approach 1 behind the same semantics. The corpus shows one writer per round, so a byte listener would be accurate for `used` on Y11; Y8/Y9 would need their own check.
- **Part B:** `A4DC8DD4`'s meaning is unknown, and it also fires for attackers. The B2 gate is the only defence against shipping a plausible-looking wrong attribution.
