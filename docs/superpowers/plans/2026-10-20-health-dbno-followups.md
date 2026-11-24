# Health-state downs and PR #1 follow-ups: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Emit real `DBNO`/`Revive` events from the Y10S4+ health state, fill `gmSettings` on Y11S2+, and fix C1–C5.

**Architecture:**
- **Parser:** new pattern listeners in `dissect/health.go` record health-state transitions. `DBNO` is appended
  when read. Usernames and `Revive` events are resolved at the end of `Reader.Read`, after `roundEnd`.
- **Ordering:** one ordered-insert helper keeps late events in `elapsedSeconds` order.
- **Header:** the header key/value loop becomes a pure function so the Y11S2 ordering can be unit-tested.

**Tech Stack:** Go 1.26 (`~/.local/go/bin`, `export PATH=$HOME/.local/go/bin:$PATH`), zerolog, the existing golden
tests in `dissect/test`.

**Spec:** `docs/superpowers/specs/2026-10-20-health-dbno-followups-design.md`

## Global Constraints

- **Event IDs:** `DBNO` = 11 and `Revive` = 12 keep their IDs. No new `MatchUpdate` JSON fields; `DBNOBy`,
  `FinishedBy` and `BleedOut` do not come back.
- **Version gate:** health listeners run only when `Header.CodeVersion >= Y10S4`.
- **Kills unchanged:** `Kill` and `Death` events (type, username, target, headshot, teamKill, phase, elapsedSeconds)
  are identical to `main` (2df8f2c) on all 42 Y11 rounds and all fixtures.
- **Ordering:** `elapsedSeconds` never decreases within a round's `matchFeedback`.
- **Windows:**
  - Death after leaving the downed state: 1.5 s.
  - Reviver +50 match: 3 s.
  - Health-owner back-search: 128 bytes.
- **Score inference:** the only new use is the +50 revive collector.
- **Goldens:** regenerate with `go test ./dissect/test -run TestReader_Read -update`. The only allowed golden
  diffs are:
  - the new `DBNO`/`Revive` events;
  - Y11S2 `gmSettings` (61 values);
  - the 10th Y11S2 header player's `id`/`heroName`/`alliance`/`roleImage`/`roleName`/`rolePortrait`;
  - one `phase` value in `Y8S1/ranked_1`.
- **Commits:** one per task, ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never commit
  `.rec` files outside the fixtures.
- **Commands:** run `go` from `/home/prest/projects/R6Stats/r6-dissect-dbno` (use `go -C <dir>`; the shell cwd resets).

## Review Focus

1. **A player downed twice in one round** (revived, then downed again). Expected: two `DBNO` events with one
   `Revive` between them, each resolved against its own transition. Tested in Task 7
   (`TestResolveHealth_DownedTwice`).
2. **A downed player who is then teamkilled.** Expected: `DBNO.username` = the teammate and `teamKill` = true.
   Tested in Task 7 (`TestResolveHealth_FinishedByTeamkill`).
3. **A stop event noticed after a later event, when an unresolved defuser completion is pending.** Expected:
   the stop lands before the later event and `pendingDefuserPlantIdx` still points at the completion. Tested in
   Task 1 (`TestInsertByElapsed_ShiftsPendingDefuser`).
4. **A Y11S3 header with `starttime` after `id`.** Expected: `starttime` is a match property, not a player
   field, and the players are unchanged. Tested in Task 6 (`TestSplitHeaderEntries_Y11S3StartTime`).
5. **A round where the winner was the last one standing and an opponent bled out or left.** Expected:
   `1vX` = `clutchDetail.opponentsAlive`. Tested in Task 4 (`TestPlayerStats_OneVxMatchesClutch`).

---

### Task 1: Ordered insert for late events (C5)

**Files:**
- Modify: `dissect/feedback.go` (add the helper)
- Modify: `dissect/defuse.go:426-449` (`stopDefuserRun`)
- Test: `dissect/feedback_test.go` (new)

**Interfaces:**
- Produces: `func (r *Reader) insertByElapsed(u MatchUpdate)`.
  - It inserts `u` right after the last event `e` with `e.Phase != ""` and `e.ElapsedSeconds <= u.ElapsedSeconds`.
    Phase-less events before that point are skipped over. If no such event exists, `u` goes after the leading
    phase-less events, or at index 0.
  - When `pendingDefuserPlantIdx >= 0` and the insert index is `<=` it, `pendingDefuserPlantIdx++`.

- [ ] **Step 1: Write the failing tests** in `dissect/feedback_test.go`:
  - `TestInsertByElapsed_LateStop`: feed `[Kill@10, Kill@20]`, insert a `DefuserPlantStop@15` → types in order
    `Kill, DefuserPlantStop, Kill`.
  - `TestInsertByElapsed_Ties`: feed `[Kill@10]`, insert `@10` → the inserted event is last.
  - `TestInsertByElapsed_ShiftsPendingDefuser`: feed `[Kill@10, DefuserPlantComplete@12 (Username ""), Kill@20]`
    with `pendingDefuserPlantIdx = 1`; insert `@5` → `pendingDefuserPlantIdx == 2` and
    `MatchFeedback[2].Type == DefuserPlantComplete`.
  - `TestInsertByElapsed_PhaseLessPrefix`: feed `[Other (no phase), Kill@10]`, insert `@1` → index 1.
  - In all tests, every event except the phase-less one gets `Phase: ActionPhase`.
- [ ] **Step 2: Run** `go test ./dissect -run TestInsertByElapsed -v`. Expected: FAIL, `insertByElapsed` undefined.
- [ ] **Step 3: Implement `insertByElapsed`** in `feedback.go`. In `stopDefuserRun`, replace the
  `append(r.MatchFeedback, u)` with `r.insertByElapsed(u)`.
- [ ] **Step 4: Run** `go test ./dissect/... -count=1`. Expected: PASS, with no golden change (the data has 0
  out-of-order stops).
- [ ] **Step 5: Commit** `fix: insert late defuser stop events in time order`.

### Task 2: `MatchReader.Listen` duplicates (C4)

**Files:**
- Modify: `dissect/match.go:41-51`
- Test: `dissect/match_test.go` (new)

- [ ] **Step 1: Write the failing test.** `TestMatchReaderListen_SamePattern`: call `m.Listen(p, a)` and then
  `m.Listen(p, b)` on a zero `MatchReader` → `len(m.queries) == 1` and `len(m.listeners[0]) == 2`.
- [ ] **Step 2: Run** `go test ./dissect -run TestMatchReaderListen -v`. Expected: FAIL (2 queries).
- [ ] **Step 3: Return** right after appending to an existing pattern, the same way `Reader.Listen` does
  (`reader.go:311`).
- [ ] **Step 4: Run** the test. Expected: PASS.
- [ ] **Step 5: Commit** `fix: MatchReader.Listen registers a callback once per pattern`.

### Task 3: Teamkills in `OpeningKill` and `Trades` (C2)

**Files:**
- Modify: `dissect/stats.go:38-70`
- Test: `dissect/stats_test.go` (new)

- [ ] **Step 1: Write the failing tests:**
  - `TestOpeningKill_SkipsTeamKill`: feed `[Kill a1→a2 TeamKill@5, Kill a1→d1@8]` → `OpeningKill().Target == "d1"`.
  - `TestTrades_KillsOnly`: feed `[Kill d1→a1@10, DefuserPlantStop a2@11, Kill a2→d1@12]` → exactly one pair
    `[Kill d1→a1, Kill a2→d1]`.
  - `TestTrades_SkipsTeamKill`: feed `[Kill d1→a1@10, Kill a2→a3 TeamKill@11, Kill a2→d1@12]` → one pair, which
    does not contain the teamkill.
- [ ] **Step 2: Run** `go test ./dissect -run 'TestOpeningKill|TestTrades' -v`. Expected: FAIL.
- [ ] **Step 3: Implement.** `OpeningKill` skips `TeamKill`. `Trades` updates `previous` only on non-teamkill
  `Kill` events and evaluates only those. Its condition is unchanged: same players, and the time difference is
  `<= 3`.
- [ ] **Step 4: Run** the tests. Expected: PASS.
- [ ] **Step 5: Commit** `fix: skip teamkills and non-kill events in OpeningKill and Trades`.

### Task 4: `1vX` agrees with `clutchDetail` (C1)

**Files:**
- Modify: `dissect/stats.go:139-197` (the 1vX block in `PlayerStats`)
- Test: `dissect/stats_test.go`

- [ ] **Step 1: Write the failing tests:**
  - `TestPlayerStats_OneVxMatchesClutch`. Setup: 3v3, team 1 wins, `Teams[1].Won = true`.
    - Feed `[Kill d1→a1@10, Kill a2→d2@20, Kill a3→d3@25]`. Team 1 is now down to d1.
    - Then `Death a2@30` (no killer) and `Kill d1→a3@40`.
    - Expect d1's `OneVx == 2` and `ClutchDetail.OpponentsAlive == 2`. Today `OneVx` gives 1.
  - `TestPlayerStats_OneVxNoClutchFallback`: the winner's team never drops to 1 → `OneVx == 0`, no `ClutchDetail`.
    This test should pass already; keep it.
- [ ] **Step 2: Run** `go test ./dissect -run TestPlayerStats_OneVx -v`. Expected: the first test FAILS.
- [ ] **Step 3: Implement.** After the loop, when `clutch != nil`, set `oneVx = clutch.OpponentsAlive`. Otherwise
  keep the current count. `ClutchDetail` stays set when `oneVx > 0 && clutch != nil`.
- [ ] **Step 4: Run** `go test ./dissect/... -count=1`. Expected: PASS.
- [ ] **Step 5: Commit** `fix: 1vX is the number of opponents alive when the clutch started`.

### Task 5: No `PostPlant` without a plant (C3)

**Files:**
- Modify: `dissect/time.go` (new function), `dissect/reader.go:239-300` (`Read`)
- Modify: `README.md` (the `time` / `timeInSeconds` and `phase` rows)
- Test: `dissect/clock_sentinel_test.go`

**Interfaces:**
- Produces: `func (r *Reader) relabelPostPlantWithoutPlant()`. It is called in `Read` (non-partial branch) right
  after `r.backfillLeadingUpdates()`.

- [ ] **Step 1: Write the failing tests:**
  - `TestRelabelPostPlant_NoPlant`: feed `[Kill Action@100, PlayerLeave PostPlant@183]` → both `Action`, elapsed
    unchanged.
  - `TestRelabelPostPlant_Planted`: with a `DefuserPlantComplete` present, `PostPlant` stays.
- [ ] **Step 2: Run** `go test ./dissect -run TestRelabelPostPlant -v`. Expected: FAIL, undefined.
- [ ] **Step 3: Implement and call it.** README:
  - `phase` row: `PostPlant` only appears after a plant.
  - `time` row: after the action timer runs out without a plant, `time` shows the post-round countdown.
- [ ] **Step 4: Regenerate the goldens** with `go test ./dissect/test -run TestReader_Read -update`, then
  `git diff --stat dissect/test`. Expected: only `Y8S1/ranked_1.rec.json`, one `"phase"` line.
- [ ] **Step 5: Commit** `fix: label events after the action timer Action when nothing was planted`.

### Task 6: `gmSettings` and the last header player on Y11S2+

**Files:**
- Modify: `dissect/header.go:346-431` (`readHeader`)
- Test: `dissect/header_test.go` (new)

**Interfaces:**
- Produces:
  - `type headerEntry struct{ key, value string }`
  - `func splitHeaderEntries(entries []headerEntry) (props map[string]string, gmSettings []int, players []Player, err error)`
- `readHeader` reads entries until `teamscore1` is read, calls `splitHeaderEntries`, then parses `props` exactly as today.

**Rule in `splitHeaderEntries`:**
- The player keys are `playerid`, `profileid`, `playername`, `team`, `heroname`, `alliance`, `roleimage`,
  `rolename` and `roleportrait`.
- `playerid` starts a new player, appending the previous one.
- Any non-player key while a player is open appends that player and leaves player mode before the key is
  handled.
- At the end, an open player is appended.
- `gmsetting` values go to `gmSettings`; all other non-player keys go to `props`.
- `profileid` stays out of `props`. Player fields are parsed as today.

- [ ] **Step 1: Write the failing tests** in `header_test.go`:
  - `TestSplitHeaderEntries_Y11S2Order`. Entries:
    - 2 players (`playerid`/`playername`/`team`/`heroname`);
    - then `version`, `gmsetting`=1, `gmsetting`=2, `maxnbplayersperteam`, `id`;
    - then 2 players, `teamscore0`, `teamscore1`.
    - Expect: 4 players in order, each with its `playerid` and `heroname`; `gmSettings == [1 2]`;
      `props["version"]` set; `props["id"]` set.
  - `TestSplitHeaderEntries_Y11S3StartTime`: the same, with `starttime` after `id` → `props["starttime"]` set and
    4 players.
  - `TestSplitHeaderEntries_Y9Order`: props, then `gmsetting`×2, then 2 players, then `id` and `teamscore*` →
    2 players, `gmSettings == [..]` (2 values).
- [ ] **Step 2: Run** `go test ./dissect -run TestSplitHeaderEntries -v`. Expected: FAIL, undefined.
- [ ] **Step 3: Implement** `splitHeaderEntries` and switch `readHeader` to it.
- [ ] **Step 4: Regenerate the goldens and diff them.** Expected:
  - both Y11S2 fixtures: `gmSettings` has 61 values;
  - RYCEEEEEEEEEEEE: `id` 14848335987929793453, plus `heroName`/`alliance`/`roleImage`/`roleName`/`rolePortrait`;
  - nothing else.
  - Confirm with `python3 -I` that `[p['username'] for p in players]` is unchanged in both files.
  - Y8/Y9: no diff.
- [ ] **Step 5: Commit** `fix: read gmSettings and the last header player on Y11S2+`.

### Task 7: Downs and revives from the health state

**Files:**
- Create: `dissect/health.go`, `dissect/health_test.go`. Port from `git show becc28b:dissect/health.go` and
  `…:dissect/health_test.go`.
- Modify: `dissect/reader.go` (fields, `NewReader` maps and listeners behind the Y10S4 gate, the `Read` call),
  `dissect/feedback.go` (an unexported field on `MatchUpdate`, the DBNO/Revive comment),
  `dissect/scoreboard.go:237-286` (the +50 collector).

**Interfaces:**
- Consumes: `insertByElapsed` (Task 1); `Player.slotID`; `r.stamp`, `r.elapsed`, `r.clock`.
- Produces:
  - Reader fields:
    - `healthToSlot map[[4]byte][4]byte`
    - `healthState map[int]uint32`
    - `healthTransitions []healthTransition` (`player int, state uint32, elapsed float64, timeRaw string,
      time float64, phase RoundPhase`)
    - `scoreRevives []scoreRevive` (`player int, elapsed float64`)
  - `MatchUpdate.healthTransition int`: index+1 into `healthTransitions` for a `DBNO` read from the health state,
    0 otherwise.
  - Listeners `readHealthLink(r *Reader) error` (pattern `41 54 DC C4`) and `readHealthState(r *Reader) error`
    (pattern `E7 88 F6 A5 04`).
  - `func (r *Reader) resolveHealthEvents()`, called in `Read` (non-partial) after `roundEnd` and its `stampNew`,
    before `backfillLeadingUpdates`.

**Behaviour to port and change:**
- Owner lookup, link parsing and the state-3 detection are the prototype's. Keep its byte-layout comments.
- On a transition into 3, append a stamped `DBNO` with `Target` set and `healthTransition` set. Do not buffer it
  (the prototype's `healthDowns` goes away).
- **The +50 collector,** in `readScoreboardScoreY10S4`:
  - when `prev, seen := r.lastPlayerScores[idx]`, `seen`, `score-prev == 50` and `r.clock.started`, append
    `scoreRevive{idx, r.elapsed()}`;
  - do this before `lastPlayerScores` is updated, and leave the +100 logic untouched.
- **`resolveHealthEvents`,** for each `DBNO` with `healthTransition > 0`:
  - **Revived** (the prototype's `downRevived` rule: the next state of that player is not 4, and no state 4 and no
    `Kill` of them within 1.5 s): insert a `Revive` with `insertByElapsed`. Its `Target` is the player and its
    time/phase/elapsed come from the transition out of 3. Its `Username` comes from
    `takeScoreReviver(target, elapsed)`: the closest `scoreRevive` within 3 s from a teammate (not the target),
    consumed; `""` if none.
  - **Otherwise:** scan forward from the `DBNO` for the first `Kill` with `Target == player` or `Death` with
    `Username == player`, stopping at the next `DBNO`/`Revive` of that player.
    - On a `Kill`: set `DBNO.Username = kill.Username` and `DBNO.TeamKill = kill.TeamKill`.
    - On a `Death` or nothing: leave `Username` empty.
- Order: compute all revive decisions first, then the usernames, then insert the revives, so an insert does not
  shift the indices being scanned.

- [ ] **Step 1: Write the failing tests** in `health_test.go`, building the `Reader` the way `defuse_test.go` does:
  - `TestHealthStateDown` (ported): a link plus a state-3 record → one `DBNO` for the slot's player; the same
    state again → no new event.
  - `TestHealthStateIgnoredBeforeClock`: clock not started → no event.
  - **`TestResolveHealth_*`,** with hand-built `healthTransitions` and feed:
    - `Finished`: down, then 4, then `Kill d1→a1` → `DBNO.Username == "d1"`.
    - `FinishedByTeamkill`: the kill is a teamkill by `a2` → `Username == "a2"` and `TeamKill`.
    - `Revived`: down@10, then 2@20, plus `scoreRevive{a2, 21}` → `Revive{Username a2, Target a1, ElapsedSeconds 20}`
      inserted in order, and `DBNO.Username == ""`.
    - `RevivedNoScore` → `Revive.Username == ""`.
    - `RevivedScoreFromOpponent`: the only `scoreRevive` is from `d1` → `""`.
    - `TransientTwoThenDeath`: 3, then 2@30, then 4@30.5 → not revived.
    - `RoundEnd`: still downed → no `Revive`, `Username == ""`.
    - `DeathNoKiller` → `Username == ""`.
    - `DownedTwice`: down@10, revive@20, down@40, 4 plus `Kill d2→a1@45` → the feed order is
      `DBNO@10, Revive@20, DBNO@40, Kill@45`; the first `DBNO` has `""` and the second has `"d2"`.
- [ ] **Step 2: Run** `go test ./dissect -run 'TestHealth|TestResolveHealth' -v`. Expected: FAIL, undefined.
- [ ] **Step 3: Implement** `health.go`, the reader wiring, the collector and the `feedback.go` comment.
- [ ] **Step 4: Run** `go vet ./... && go test ./... -count=1`. Expected: PASS, except `TestReader_Read` on the two
  Y11S2 fixtures (new events).
- [ ] **Step 5: Regenerate the goldens and diff them.** Expected:
  - only `DBNO`/`Revive` objects are added in the two Y11S2 files;
  - Y7–Y9 files are unchanged, which confirms the gate.
- [ ] **Step 6: Commit** `feat: DBNO and revive events from the player health state (Y10S4+)`.

### Task 8: 42-round acceptance check and docs

**Files:**
- Create: `dissect/test/acceptance_test.go`
- Modify: `README.md` (features list; the match feedback fields table rows for `DBNO`/`Revive`; the inferred-data
  paragraph)

**The test, `TestAcceptance_Replays`:**
- It is skipped unless `R6_DISSECT_REPLAY_DIRS` is set (a list of match folders separated by `:`).
- For every round, it asserts:
  1. `elapsedSeconds` is non-decreasing.
  2. Each `DBNO` target, before their next `DBNO`, has a `Kill`/`Death`, a `Revive` with the same target, or
     neither (round end).
  3. No `DBNO` for a player whose previous `DBNO` had no `Revive` after it.
  4. A set `DBNO.Username` is on the other team, or `TeamKill` is set.
  5. No `PostPlant` phase without a `DefuserPlantComplete`.
  6. `len(Header.GMSettings) > 0`.
- It logs totals: rounds, DBNO, DBNO with username, Revive, Revive with username.

- [ ] **Step 1: Write the test.**
- [ ] **Step 2: Run it** on the 5 matches:
  - `R6_DISSECT_REPLAY_DIRS=<the 2 Y11S2 folders under /tmp/claude-1001/-home-prest-projects-R6Stats/a0d4e668-1ac5-4759-a83d-9ce8d9f2e018/scratchpad/replays and the 3 Y11S3 folders under /mnt/c/Users/prest/Downloads/TEST REPLAYS>`
  - `go test ./dissect/test -run TestAcceptance -v`
  - Expected: PASS, with totals near the prototype's (73 DBNO / 69 with username / 3 revives / 2 with username).
    Explain any difference in the PR body.
- [ ] **Step 3: Compare kill/death events with main.**
  - Build `main` (2df8f2c) and this branch to two binaries in the scratchpad and export all 5 matches with each.
  - A `python3 -I` script compares `Kill`/`Death` events (all fields) per round.
  - Expected: 0 differences.
- [ ] **Step 4: Update the README.**
  - Features: "DBNOs (downs) and revives (Y10S4+)".
  - Fields table, `DBNO`: `target` is the downed player; `username` is the player credited with the kill if they
    died, otherwise empty.
  - Fields table, `Revive`: `target` is the revived player; `username` is the reviver from the +50 bonus, if
    found.
  - Inferred data: the reviver is inferred from the +50 score bonus.
- [ ] **Step 5: Run** `go vet ./... && go test ./... -count=1`. Expected: PASS.
- [ ] **Step 6: Commit** `test: acceptance checks on real replays; docs for downs and revives`.

### Task 9: PR and review

- [ ] **Step 1: Push and open the PR.** `git push -u origin fork/health-dbno`, then open the PR against `main`
  with `gh pr create --repo prestonwillis10/r6-dissect --base main`. The body has:
  - one section per part;
  - the expected golden diffs;
  - the Task 8 totals and the kill comparison output, pasted;
  - the coordination note about `track/rec-timeline`;
  - and it ends with the Claude Code attribution line.
- [ ] **Step 2: Wait for CI.** Wait for the 6 build jobs to go green on the head SHA (watch in the background).
- [ ] **Step 3: Review.** Run `/deduce-pr <N>` (repo `prestonwillis10/r6-dissect`). Ask the owner before merging.
