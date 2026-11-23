# Design: real downs and revives, plus the PR #1 follow-ups

Date: 2026-10-20. Target: one PR from `fork/health-dbno` into `main` of `prestonwillis10/r6-dissect`.

## Why

The fork feeds the R6Stats website, which aggregates per-season stats from `.rec` files. PR #1 removed the
down (DBNO) and revive events taken from upstream PR #126, because they were guessed from repeated kill
packets and were wrong. Since then, research found where the replay actually records a player's health
state. This PR turns that into real `DBNO` and `Revive` events. It also fixes the five small problems
(C1–C5) the final PR #1 reviewer listed, and `gmSettings` being empty on Y11S2+ replays.

The owner handed all technical calls in this PR to Claude. Every "my call" below is Claude's, not theirs.

## Goals

1. `DBNO` and `Revive` events in `matchFeedback` on Y10S4+ replays, read from the health state, not inferred
   from the kill feed.
2. C1–C5 fixed as specified below.
3. `gmSettings` filled on Y11S2+ replays, as it already is on older ones.
4. Nothing else in the output changes. Each expected golden diff is listed under its part.

## Non-goals

- Downs on replays before Y10S4. The health link is only known from Y10S4 on; older replays get no `DBNO`
  events, as today.
- Who dealt the damage of a down that was revived. The replay does not say, as far as the research found.
- New per-player stats fields (`downs`, `revives`). The website can count the events. My call: keep this PR
  to the event feed.
- Excel export changes. The Excel kill/death feed keeps showing kills and deaths only.
- Bans, site IDs, `starttime`, `isspectator`, `startingteamscore*` (new Y11S2/Y11S3 header keys). None are
  needed here.

## Anti-goals (a failure even if it works)

- Any change to kills. Each player's kills must still equal the change in their scoreboard kill counter on
  all 42 Y11 rounds (312 kills, 2 teamkills, as measured at PR #1's head).
- A down credited to the wrong team, or a player with two downs in a row without a revive in between.
- `elapsedSeconds` going backwards anywhere in a round's feed.
- Re-adding the general score-delta event inference that PR #1 removed. The only score use added is the
  narrow revive match in part 1.

## Constraints

- Go, in the existing style: pattern listeners registered in `NewReader`, post-processing at the end of
  `Reader.Read`, `log.Debug()` for traces, and constants for magic bytes.
- `MatchUpdateType` IDs are stable: `DBNO` = 11 and `Revive` = 12 are already reserved for this.
- Test data: the fixtures under `dissect/test/data/replays/valid`, plus the 42 Y11 rounds outside the repo:
  - Y11S2: two matches in the Steam `MatchReplay` folder.
  - Y11S3: three matches in `/mnt/c/Users/prest/Downloads/TEST REPLAYS`.
- Goldens are regenerated with `go test ./dissect/test -run TestReader_Read -update`. Every golden change must
  be one of the expected diffs listed below.

## Part 1: downs and revives from the health state

### What the replay contains (from `research/health-dbno`, becc28b)

- **Link.** Each player slot owns a health component. Its link record is
  `1B <slot id:4> 00000000 41 54 DC C4 <health id:4> 00000000`. The slot ID is the same one already read for
  the defuser chain (`Player.slotID`).
- **Health state.** Property `E7 88 F6 A5` (uint32) on the health component: 0 healthy, 2 wounded (≤ 20 hp,
  e.g. after a revive), 3 downed, 4 dead.
  - The property is written either as the first property of a reference record
    (`23 <health id> 00000000 <hash> …`) or as a continuation (`22 <hash> …`) shortly after one. The prototype
    searches back at most 128 bytes for the owner.
- **Current health.** `25 26 76 C9` (uint32): 20 right after a revive, 0 when downed or dead.
- **Transitions:**
  - Entering 3 is a down.
  - A down ends in one of three ways:
    - **death:** 4, sometimes through a brief 2 just before the kill feed record;
    - **revive:** the next state is not 4 and no death follows within 1.5 s. It is usually 2 with health
      reset to 20, but that is an observation, not a requirement. My call: use the prototype's rule and do
      not read current health, so a revive at a different health value still counts and the player never
      gets two downs without a revive between them;
    - **round end:** still 3.
- **Validation of the prototype on the 42 rounds:**
  - Every kill-feed target, and only those, reaches 4.
  - No down is followed by an immediate headshot kill (smallest gap 0.35 s).
  - No double downs.
  - 0–5 downs per round, 73 downs in total.

### Events

- **`DBNO`:**
  - `target` = the downed player. Phase and time are the moment of the down, set with `stamp`.
  - `username`:
    - If the down ends in that player's death, it is the player the kill feed credits with that kill, read
      after `reconcileKillsFromScoreboard`, so scoreboard corrections apply.
    - It is empty if the player was revived, was still down at round end, or died without a killer (a
      `Death` event).
    - Prototype numbers: 69 of 73 downs get a username.
  - `teamKill`: true when that kill is a teamkill. My call, mirroring the kill.
- **`Revive`:**
  - `target` = the revived player. Phase and time are the moment they left the downed state.
  - `username` = the reviver, found as follows:
    - Look for a +50 score update on a teammate of the target (never the target), within 3 s of the revive,
      closest first. Each update is used once.
    - Empty when nothing matches. Prototype: 2 of 3 revives matched; the miss may be a self-revive or a
      Doc-style revive.
  - This is the only score inference added. It is constrained by a health-confirmed revive, the team and the
    window, which the removed score-delta events were not.

### Behaviour

- **Version gate:** listen for the link and state patterns only when `CodeVersion >= Y10S4`.
  - Y10S4–Y11S1 have no test replays. If no link record is found, no events come out and nothing else
    changes.
- **States before the clock starts are ignored,** as in the prototype.
- **Reviver score:** main no longer collects score deltas for revives, so add a narrow collector of +50
  updates in `readScoreboardScoreY10S4` (player, elapsed). Nothing else uses it.
- `DBNOBy`/`FinishedBy`/`BleedOut` do not come back.
- **When events are created:**
  - `DBNO` is appended when the transition into 3 is read, so it sits in feed order. My call: the prototype
    buffered downs, which is no longer needed because main's kill dedupe only looks at `Kill`/`Death`.
  - `Revive` events and the `DBNO` usernames are resolved after `roundEnd` (which runs the scoreboard
    reconciliation). Revives are inserted by `elapsedSeconds` with the helper from C5.
- **Unaffected consumers:**
  - Kill counts, deaths, the win condition and `OpeningDeath` only look at `Kill`, `Death` and the defuser
    events.
  - Clutch and 1vX count deaths only, so a downed opponent is still alive for them.
  - After C2, `Trades` only looks at kills.
- **Docs:**
  - Update the `DBNO`/`Revive` comment in `feedback.go`.
  - README: the features list; the match feedback fields table (`username`/`target` meaning for both types);
    and the "inferred data" paragraph (reviver from the +50 bonus; Y10S4+ only).

### Expected golden diff

`DBNO` and `Revive` events are added to the two Y11S2 fixtures. Nothing else changes in those files except
parts 2–3. Y7–Y9 fixtures: no change from this part.

### Acceptance checks (on the 42 rounds, scripted)

- Kills still match the scoreboard per player (312/2 as above).
- Every `DBNO` target is afterwards either killed/died, revived (`Revive` with the same target), or alive at
  round end.
- No two `DBNO` for the same player without a `Revive` between them.
- A `DBNO` username, when set, is on the other team (or `teamKill` is set).
- `elapsedSeconds` never decreases within a round.
- Unit tests:
  - Port `health_test.go`: synthetic link + state bytes → one down; the same state again is not a new down.
  - Resolution cases: finished, revived, round end, death without killer, revive with and without a +50 match.

## Part 2: `gmSettings` on Y11S2+

### Cause

`readHeader` treats every key after a `playerid` as a player field until it sees `playlistcategory` or `id`.

- Y9S1 order: match props → `gmsetting`×N → players → `id`.
- Y11S2/Y11S3 order:
  - 5 players → match props (`version` … `startingteamscore1`) → `gmsetting`×N (61 on the Y11S2 fixture,
    57 on a Y11S3 round) → `maxnbplayersperteam` → `id`;
  - then on Y11S3 only, `starttime`;
  - then 5 more players → `teamscore0/1`.

The `gmsetting` keys fall into the player branch's `default`, which writes `props["gmsetting"]` and drops the
list.

### Fix

- A key that is not a player key (`playerid`, `profileid`, `playername`, `team`, `heroname`, `alliance`,
  `roleimage`, `rolename`, `roleportrait`) ends the current player: append the player, leave player mode,
  then handle the key as a match property.
- This also covers the existing `playlistcategory`/`id` cases.

### Expected golden diff

- Y11S2 fixtures: `gmSettings` goes from `[]` to the 61 values.
- **The last header player is fixed too.** Today the header loop stops at `teamscore1` before appending the
  player it is building. On Y11S2+ that is the 10th player (RYCEEEEEEEEEEEE in both fixtures), who then only
  comes from the packet data: `id` 6366317606386794496 and no `heroName`, `alliance`, `roleImage`, `roleName`
  or `rolePortrait`. With the fix, `teamscore0` ends that player, so they get the header's `playerid` like the
  other nine (14848335987929793453) and those fields are filled in. My call: accept this; it is a correct
  fix, and `RecordingPlayer()` matches on `ID`. List it in the PR body.
- Player order and every other field unchanged.
- Y8/Y9 fixtures: no change.

### Checks

- A unit test that feeds a synthetic Y11S2-ordered header and asserts the players and gmSettings.
- On a Y11S3 round, 57 values.

## Part 3: the C1–C5 follow-ups

### C1: `1vX` disagrees with `clutchDetail.opponentsAlive`

- **Problem:** an opponent who dies without a killer during the clutch is in `opponentsAlive` but not in
  `1vX` (match 08-25 20-53, round index 1: flux_ shows 3 vs 4).
- **Fix:** when a clutch started, `1vX` = `clutchDetail.opponentsAlive`, i.e. the opponents alive when the
  winner became the last one standing. Otherwise keep today's fallback (opponents alive at round end plus
  the winner's kills).
- `clutchDetail` stays present whenever `1vX > 0`.
- **Checks:** the goldens hold only `header` and `matchFeedback`, so C1 and C2 cannot show up as a golden
  diff. My call: cover them with unit tests and the 42-round script (including flux_ in match 08-25 20-53,
  round index 1, which must give 1vX 4). Do not add stats to the goldens.

### C2: teamkills in `OpeningKill` and `Trades`

- `OpeningKill` skips teamkills.
- `Trades` only pairs consecutive non-teamkill `Kill` events. It no longer compares against whatever event
  came before, which today pairs defuser-stop events (3 of 25 pairs on the Y11 data) and would pair `DBNO`
  events.
- The trade rule itself is unchanged: second kill within 3 s, involving the first kill's players.
- Affects the Excel export and the Go API only.

### C3: `PostPlant` without a plant

- **Problem:** when the action timer runs out with no plant, the next countdown is labelled `PostPlant`
  (Y8S1 `ranked_1`: PlayerLeave at elapsed 183).
- **Fix:** at the end of `Read`, after all stamping, a round with no `DefuserPlantComplete` gets every
  `PostPlant` event relabelled `Action`.
  - `elapsedSeconds` is already monotonic and stays.
  - `time` stays the raw timer, which then shows the post-round countdown. Document this in the README's
    `time` row.
- **Expected golden diff:** one `phase` value in `Y8S1/ranked_1`.

### C4: `MatchReader.Listen` registers duplicates

- In `match.go`, return after appending the callback to an existing pattern, as `Reader.Listen` already does.
- Unit test: two callbacks on one pattern are registered once each.

### C5: stop events can break ordering

- `DefuserPlantStop`/`DefuserDisableStop` carry the time of the attempt's last timer packet but are appended
  when the stop is noticed.
- Add one helper that inserts an update after the last event whose `elapsedSeconds` is ≤ its own; events
  without a phase are skipped over. Use it for stop events and for `Revive`.
- It must keep `stampNew(n)` correct: inserting before index `n` only shifts already-stamped events.
- It must keep `pendingDefuserPlantIdx` pointing at the same event. An insert at or before it shifts it.
- Unit test: a stop noticed after a later event lands before that event.

## Coordination

- The replay-timeline lane (`track/rec-timeline`) is in flight and also touches `reader.go` (`Reader.Data()`)
  and adds Y11S3 fixtures with goldens.
- Whichever lands second regenerates the goldens and resolves the small `reader.go` conflict.
- That lane's landing PR currently targets `fork/integration`, not `main`. That is the lane owner's call; it
  is flagged to the owner, not changed here.

## How the PR is built and reviewed

- One PR, one commit per part or C-item.
- Tests first for each behaviour change.
- The scripted acceptance checks run on the 42 rounds, and their output goes in the PR body.
- Then `/deduce-pr`, then the owner confirms the merge.

## Left to the builder

- File layout and names. A new `dissect/health.go` is expected.
- The exact owner-search and death windows (128 bytes, 1.5 s, 3 s). They may change if the data says so;
  keep the acceptance checks green and say why in the commit.
- The structure of the test helpers and the acceptance script. A Go test behind an env var with the replay
  path, or a script under the scratchpad, is fine; do not commit the 42 rounds.
