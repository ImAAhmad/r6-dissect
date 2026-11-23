# Fork port 07: a round with no score change has no winner — Design and Implementation Plan

> **Issue:** #19 (consumer: prestonwillis10/R6Stats#17) · prestonwillis10/r6-dissect · fork survey: `docs/research/2026-10-21-fork-survey.md`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On Y9S4+ replays, a round whose header scores show no single winner is reported with no winner (`won: false` on both teams, no `winCondition`) and credits no 1vX clutch, instead of being awarded to team 1.

**Architecture:** One helper, `headerWinner()`, reads the winner from the header score delta and returns -1 when it is not clear. `roundEnd` stops before deriving a win condition when there is no winner. `PlayerStats` skips the 1vX block when no team won. The Excel round sheet leaves the winner cells empty. No new JSON field.

**Tech Stack:** Go 1.x (`$HOME/.local/go/bin`), `github.com/go-test/deep` golden tests.

**Spec:** this document (design sections first, then the plan).

**Base:** `origin/main` at `bd6ccbeb7b46af99638df5d650187069e849f925`. The patch also applies cleanly to `origin/fork/health-dbno` (`c608cf8`) and `origin/track/rec-timeline` (`fdaab61`), and all tests pass there (checked, see Interactions).

---

## Summary

Our `roundEnd` takes the Y9S4+ winner from the header score delta, but it only checks team 0: `team1Won = !team0Won`. A round where neither score moved goes to team 1, with a made-up win condition (usually `Time`) and, through `PlayerStats`, a made-up 1vX for team 1's last player alive. This happens when a round is abandoned or remade, when a disconnect ends it, or when the recording ends before the result is written. The fork Animuxs/r6-stats-app fixed this in `8dc0307`. We port the mechanism (no winner when no score moved, and no clutch on such rounds). We do not port its reordering of win conditions; that is a separate change, see Decision D5.

## Source

- Fork: **Animuxs/r6-stats-app** (`https://github.com/Animuxs/r6-stats-app.git`). The same code is in atrejo26/r6-dissect-stats.
- Commit `8dc030702cf39311068d42b22bc4af273e5824b2` ("Add Pro League-style match stats and fix Y11S3 replay parsing", 2026-09-25). It is still on `Animuxs/main` (`6602cf2`); later commits `8561447` and `e8b0eb5` touch the same functions but leave this logic as it was.
- Files: `dissect/time.go` (`roundEnd`, new `winCondition`), `dissect/stats.go` (`roundDecided`).
- What it does:
  - `headerWinner := -1`, then for each team, `if StartingScore < Score { headerWinner = i }`, and both `Won` are cleared. `if r.Header.CodeVersion >= Y9S4 && headerWinner < 0 { return }`. The comment reads: "Neither score moving means the round was cut short (e.g. an abandoned match)."
  - `stats.go`: `roundDecided := r.Header.Teams[0].Won || r.Header.Teams[1].Won` and `if lastWinnerStanding > -1 && roundDecided {`.
  - It also rewrites the win-condition derivation into `winCondition(...)`. Its `switch` puts `KilledOpponents` before `DefusedBomb`, so a post-plant wipe of the defenders becomes `KilledOpponents`. It also drops `DefuserDisableComplete` events when the header says the defenders lost.
- Flaws and differences:
  - If both scores rose, it picks the last team (`headerWinner = i` overwrites). This cannot happen in a real round, but -1 is the safe answer.
  - It was written on a 2025-09 upstream base and does not have our `planted`/`disabler`/inferred-disable logic, our `reconcileKillsFromScoreboard`, or our Y9S4 `DefuserDisableComplete` role check. The code cannot be cherry-picked; we port the idea.
  - The reorder and the event dropping are separate semantic changes bundled into the same commit.
  - We read the fork code but did not build or run it (safety rule).

## Current behaviour (bd6ccbe)

`dissect/time.go`, `roundEnd`:

```go
	if r.Header.CodeVersion >= Y9S4 {
		team0Won := r.Header.Teams[0].StartingScore < r.Header.Teams[0].Score
		r.Header.Teams[0].Won = team0Won
		r.Header.Teams[1].Won = !team0Won
	}
```

and later, the derivation of the condition, which assumes a winner exists:

```go
	if r.Header.CodeVersion >= Y9S4 {
		winner := 0
		if r.Header.Teams[1].Won {
			winner = 1
		}
		...
		case roles[winner] == Defense && r.Header.GameMode != Hostage:
			r.Header.Teams[winner].WinCondition = Time
```

`dissect/stats.go`, `PlayerStats`: `winningTeamIndex := 0` / `if r.Header.Teams[1].Won {` and then `if lastWinnerStanding > -1 {`, which credits `OneVx` to the last player alive on the "winning" team.

`dissect/match.go` (Excel round sheet): `winningTeamIndex := 0` / `if r.Header.Teams[1].Won {` then `c.Right(1).Str(fmt.Sprintf("%s [%d]", r.Header.Teams[winningTeamIndex].Name, winningTeamIndex))`, so the sheet names team 0 as the winner when no team won.

### Reproduction (run 2026-10-21)

Corpus scan: our parser at bd6ccbe over the user's 5 real match folders (Y11S2 + Y11S3, 42 rounds) and over the 66 unique `.rec` blobs committed in any fork. Exactly **one** round has neither score moving:

- `nicktstewart/Y11S1-changes` (`df89e6d`): `dissect/test/data/replays/defuser_detection/match2/Match-2026-03-08_19-55-32-26676-R05.rec`, blob `e1ee37ca6efff89b86b7e35a18b2ef832a08c464`, 29,569 bytes, sha256 `18838c8faa6087f972df035572869c8b21426378c9441038d91213c247d12ec0`. A Y11S1 (`codeVersion` 9540659) 1v1 custom game, round index 4, with 0 feed events.

```
$ r6-dissect -f json -o main-R05.json e1ee37ca….rec   # bd6ccbe
teams: [{startingScore 2, score 2, won False, winCondition None, role Attack},
        {startingScore 2, score 2, won True,  winCondition 'Time', role Defense}]
stats: player39 (team 1) '1vX': 1
```

Same match, the other player's recording of the same round (`match3/Match-2026-03-08_19-55-32-21916-R05.rec`, blob `aa9644d0…`, same `matchID` `bed28be5-…`): team 1 goes `2->3`. So in this case the game did give the round to team 1, and our output is right **by accident**. The first file does not hold the result. Had team 0 won, we would have reported the wrong winner. With the prototype fix, the first file reports no winner and no 1vX, and the second file is unchanged (`(3, True, 'Time')`).

The user's real corpus has no such round. With the prototype fix, the JSON for all 5 folders is byte-identical to bd6ccbe.

## Approaches considered

**A. No winner: `won: false` on both teams, no `winCondition`.** (Animuxs's shape.)
- + No new schema. The parser can already emit false/false today: the pre-Y9S4 branch returns without a winner when a `DefuserDisableComplete` player is not found, and a round whose listeners fail never reaches `roundEnd`, so its `Won` flags stay false (relevant to item 01).
- + Honest: the file does not contain the result.
- − Consumers that assume exactly one winner break. R6Stats does (see Interactions) and must change first.

**B. A + an explicit field (for example, round-level `"result": "undecided"` or team-level `"decided": false`).**
- + Self-describing; a consumer can tell "undecided" from "parser bug".
- − Redundant: on Y9S4+, false/false only ever means "no result". The field adds surface area that must be kept in step with `won`, and does not save R6Stats any work, because it must handle no winner either way. It can be added later without breaking anyone if a consumer needs it.

**C. Fall back to inferring the winner from the feed (the pre-Y9S4 path) when the header is flat.**
- + Fills in a winner for a "recorder left before the end" file.
- − Invents results for abandoned or remade rounds, which have kills but no real result. For the one real example (0 events) it would invent a `Time` win for the defense, which is exactly today's bug. It contradicts the reason we trust the header from Y9S4 on.

**D. Keep awarding team 1 (status quo).** Wrong whenever team 0 would have won, and it fabricates conditions and clutches.

## Decisions (made autonomously; alternatives above)

- **D1 — Approach A.** On Y9S4+, the winner is the one team whose score rose. If neither or both rose, there is no winner. No new JSON field. The README documents the shape.
- **D2 — No win condition and no feed edits on an undecided round.** `roundEnd` returns before deriving a condition. It neither appends the inferred `DefuserDisableComplete` nor sets `DisabledDefuser` from a disable event. Feed events are kept as recorded.
- **D3 — No 1vX on an undecided round.** Guard `PlayerStats`'s 1vX block on "some team won". This also covers pre-Y9S4 rounds that end without a winner.
- **D4 — Excel shows empty winner cells** for an undecided round, instead of team 0.
- **D5 — The win-condition reorder (`KilledOpponents` before `DefusedBomb` after a plant) is NOT in this issue.** It is a different mechanism: it changes the label of decided rounds, not who won. It would change many rounds: with our parser, 8 of the 8 `DefusedBomb` rounds in the user's real corpus, and 10 of 14 in the fork corpus, have every defender `died: true`, so the reorder would relabel them `KilledOpponents`. That would churn goldens and change R6Stats round-length sources (`services/facts.py` maps `DefusedBomb` to "detonation", plant + 45 s). It deserves its own closer-look item with replay ground truth (the in-game round-end reason). Recommended to the dispatcher as a new candidate; not filed here.
- **D6 — Do not port the fork's dropping of `DefuserDisableComplete` events "when the defenders did not win".** Our Y9S4 branch already ignores such a disable for the condition (`log.Debug().Msg("warn: defuser disabled but attack won the round")`) and keeps the event. Changing the feed is out of scope.
- **D7 — Fixture.** Add the nicktstewart file as `dissect/test/data/replays/valid/Y11S1/custom_unscored_round.rec`. The fork is MIT-licensed, like upstream (`LICENSE`: "Copyright (c) 2022 Benjamin Ryan"). Item 15 (Y11S1 fixtures from forks) may import the same `defuser_detection` set; if it lands first and holds this blob, reuse its path and skip the copy.
- **D8 — R6Stats is changed first, in its own issue (07b).** R6Stats pins the parser through `vendor/r6-dissect` (`Dockerfile`: `ARG R6_DISSECT_SRC=vendor/r6-dissect`). So the fork can merge this at any time, but R6Stats must not bump its submodule past this commit until 07b ships.
- **Definition, shared with item 01:** an *undecided round* is a round object with no team `won: true`. Item 07 produces one when a fully parsed Y9S4+ file has flat header scores. Item 01 produces one if it keeps a round whose body read failed: `Reader.Read` returns on a listener error before `roundEnd`, so `Won` stays false/false. A *partial round* (item 01's term) is a file whose read errored. It may or may not also be undecided. Consumers handle both the same way.

## Interactions

- **`origin/fork/health-dbno` (PR #3, C1 clutch change).** It adds `if clutch != nil { oneVx = clutch.OpponentsAlive }` inside the same `if lastWinnerStanding > -1 {` block. Our guard changes only that `if` line, so their change is covered too. `git apply --3way` of the prototype onto `c608cf8`: clean; `go test ./dissect/...` passes. **Sequencing:** not blocked. If PR #3 merges first, rebase (no conflict expected). If this lands first, PR #3 rebases cleanly.
- **`origin/track/rec-timeline` (PR #10).** It changes the inferred-disable append in `roundEnd` to `u.offset = -1; u.derived = true; r.addUpdate(u)`. Our early return comes before that block. Prototype applied cleanly onto `fdaab61`; `go test ./dissect/...` passes, including `dissect/wire`.
- **PR #12 (ElyasYassin, `scoreboard.go` only):** no overlap.
- **Item 01 (incomplete-match crash):** shares the "undecided round" representation (see the definition above). No code overlap: 01 is in `MatchReader.Read`/`match.go` reading, and 07 is `roundEnd`/`PlayerStats`.
- **Item 14 (per-player trades), item 08 (late joiner):** both may touch `stats.go` `PlayerStats`. Only a textual conflict is possible, on nearby lines.
- **Item 15 (Y11S1 fixtures):** see D7.
- **JSON / R6Stats consumer (`origin/main` of prestonwillis10/R6Stats):**
  - `api/r6stats/services/parser_contract.py` `normalize_round`: `if sum(t.won for t in teams) != 1: raise ParserContractError("teams", "r6-dissect output: exactly one team must have won the round")`.
  - `api/r6stats/worker/jobs.py` `run_pipeline`: `match = normalize_match(dissect(...))` inside `try`, with `except Exception as exc: ... _fail(db, upload_id, UploadStatus.failed, str(exc))`. **One undecided round fails the whole upload.**
  - Also: `RoundData.winner_team_index` is `next(i for i, t in enumerate(self.teams) if t.won)`, `models/replay.py` has `winner_team_index: Mapped[int]` (NOT NULL), and `services/scoring.py` counts rounds by `winner_team_index`.
  - Today the round example above ingests "successfully" with a guessed winner. After this change, without 07b, it fails the upload. Hence D8.
  - 07b's suggested handling: skip undecided rounds at ingest with an upload warning. Another recording of the same round, if one exists, supplies the result. Abandoned or remade rounds should not count toward scores or stats.
- **Golden churn:** none on existing fixtures (prototype: `go test ./dissect/test` passes unchanged). There is one new fixture plus its `.json`.

## Global Constraints

- Go toolchain: `export PATH=$HOME/.local/go/bin:$PATH`.
- Y9S4+ only for the header rule (`r.Header.CodeVersion >= Y9S4`). Pre-Y9S4 derivation is unchanged.
- Representation of an undecided round: `teams[0].won == false && teams[1].won == false`, `winCondition` omitted on both (the `omitempty` tag already in `header.go` `Team`). No new fields.
- Do not edit `/home/prest/projects/R6Stats/r6-dissect` from a planning session; implement on a branch `fork/undecided-round` from `origin/main`.

## Review Focus

1. **Plant + disable in a flat-score round:** no `DisabledDefuser`, no inferred `DefuserDisableComplete` appended, feed length unchanged. Covered by `TestRoundEnd_UndecidedRoundKeepsFeedAsIs` (Task 1).
2. **Both scores rose (corrupt header):** no winner. Covered by `TestHeaderWinner` (Task 1).
3. **Normal decided rounds unchanged:** the existing `TestRoundEnd_*` tests and all goldens, plus a byte-identical JSON diff on the user's corpus (Task 3).
4. **Clutch on an undecided round with both sides alive:** no `1vX`, no `clutchDetail`. Covered by `TestPlayerStats_UndecidedRoundHasNoClutch` (Task 2).
5. **Excel export of an undecided round** must not crash or name a winner. Covered by `TestRoundDecided` + the Task 2 manual check.

---

## Implementation plan

### File structure

- Modify `dissect/time.go`: add `headerWinner()`, use it in `roundEnd`, add the early-out.
- Modify `dissect/stats.go`: add `roundDecided()`, guard the 1vX block.
- Modify `dissect/match.go`: empty winner cells in the Excel round sheet.
- Modify `dissect/roundend_test.go`: new unit tests (the file already holds `roundEnd` unit tests and `roundEndPlayers()`).
- Create `dissect/test/data/replays/valid/Y11S1/custom_unscored_round.rec` + `.rec.json` (golden).
- Modify `README.md`: one sentence after the JSON example.

### Task 1: `roundEnd` reports no winner when the header shows none

**Files:**
- Modify: `dissect/time.go` (`roundEnd`, new `headerWinner` directly above it)
- Test: `dissect/roundend_test.go`

**Interfaces:**
- Produces: `func (r *Reader) headerWinner() int`, which returns 0 or 1 when exactly that team's `Score > StartingScore`, and -1 otherwise.

- [ ] **Step 1: Write the failing tests** (append to `dissect/roundend_test.go`)

```go
// neither header score moved: the round has no winner and no win condition.
func TestRoundEnd_NeitherScoreMovedHasNoWinner(t *testing.T) {
	r := &Reader{Header: Header{CodeVersion: Y10S4, GameMode: Bomb,
		Teams:   [2]Team{{Role: Attack, StartingScore: 2, Score: 2}, {Role: Defense, StartingScore: 2, Score: 2}},
		Players: roundEndPlayers()},
	}
	r.roundEnd()
	for i, team := range r.Header.Teams {
		if team.Won || team.WinCondition != "" {
			t.Fatalf("team %d: want no result, got won=%v condition=%q", i, team.Won, team.WinCondition)
		}
	}
}

// a plant and a disable in a round without a result: no condition, no inferred disable.
func TestRoundEnd_UndecidedRoundKeepsFeedAsIs(t *testing.T) {
	feed := []MatchUpdate{{Type: DefuserPlantComplete, Username: "a1"}, {Type: DefuserDisableComplete, Username: "d1"}}
	r := &Reader{Header: Header{CodeVersion: Y10S4, GameMode: Bomb,
		Teams:   [2]Team{{Role: Attack, StartingScore: 3, Score: 3}, {Role: Defense, StartingScore: 1, Score: 1}},
		Players: roundEndPlayers()},
		MatchFeedback: append([]MatchUpdate(nil), feed...),
	}
	r.roundEnd()
	if len(r.MatchFeedback) != len(feed) {
		t.Fatalf("feed changed: want %d events, got %d", len(feed), len(r.MatchFeedback))
	}
	for i, team := range r.Header.Teams {
		if team.Won || team.WinCondition != "" {
			t.Fatalf("team %d: want no result, got won=%v condition=%q", i, team.Won, team.WinCondition)
		}
	}
}

func TestHeaderWinner(t *testing.T) {
	cases := []struct{ s0, e0, s1, e1, want int }{
		{0, 1, 0, 0, 0}, {4, 4, 2, 3, 1}, {2, 2, 2, 2, -1}, {1, 2, 1, 2, -1},
	}
	for _, c := range cases {
		r := &Reader{Header: Header{Teams: [2]Team{{StartingScore: c.s0, Score: c.e0}, {StartingScore: c.s1, Score: c.e1}}}}
		if got := r.headerWinner(); got != c.want {
			t.Errorf("%d->%d | %d->%d: want %d, got %d", c.s0, c.e0, c.s1, c.e1, c.want, got)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./dissect/ -run 'TestRoundEnd_NeitherScoreMovedHasNoWinner|TestRoundEnd_UndecidedRoundKeepsFeedAsIs|TestHeaderWinner'`
Expected: build failure `r.headerWinner undefined`. (With `TestHeaderWinner` commented out, the other two fail with `team 1: want no result, got won=true condition="Time"` and `... condition="DisabledDefuser"`; this was checked at bd6ccbe.)

- [ ] **Step 3: Implement**
  - Add `headerWinner()` above `roundEnd` with the doc comment "returns the index of the only team whose header score rose this round, or -1 when neither score (or both) rose: the round was abandoned, remade, or recorded without its end."
  - In `roundEnd`, replace the `team0Won` block with `decided := true`; inside `if r.Header.CodeVersion >= Y9S4 {`, set `winner := r.headerWinner()`, `decided = winner >= 0`, `Teams[0].Won = winner == 0`, and `Teams[1].Won = winner == 1`.
  - In the `case DefuserDisableComplete:` Y9S4 branch, add `if !decided { continue }` as its first statement. The event stays in the feed.
  - In the post-loop `if r.Header.CodeVersion >= Y9S4 {` block, first add `if !decided { log.Debug().Msg("round has no result: neither header score moved"); return }`.
  - Leave `reconcileKillsFromScoreboard` and the death counting as they are.

- [ ] **Step 4: Run to verify they pass, and nothing else moved**

Run: `go test ./dissect/ -run 'TestRoundEnd|TestHeaderWinner' && go test ./dissect/test`
Expected: `ok` for both (the existing `TestRoundEnd_UnattributedPlantDefenseWin` and `TestRoundEnd_DuplicateDeathCountedOnce` still pass; no golden diff).

- [ ] **Step 5: Commit** — `fix: a round where neither header score moved has no winner`

### Task 2: no clutch and no Excel winner for an undecided round

**Files:**
- Modify: `dissect/stats.go` (`PlayerStats`, new `roundDecided`)
- Modify: `dissect/match.go` (Excel round sheet "Winning team" / "Win condition" cells)
- Test: `dissect/roundend_test.go`

**Interfaces:**
- Consumes: Task 1's `roundEnd` behaviour (both `Won` false when undecided).
- Produces: `func (r *Reader) roundDecided() bool`, which returns `r.Header.Teams[0].Won || r.Header.Teams[1].Won`.

- [ ] **Step 1: Write the failing tests**

```go
// a round without a result credits no clutch to either team.
func TestPlayerStats_UndecidedRoundHasNoClutch(t *testing.T) {
	r := &Reader{Header: Header{CodeVersion: Y10S4, GameMode: Bomb,
		Teams:   [2]Team{{Role: Attack, StartingScore: 2, Score: 2}, {Role: Defense, StartingScore: 2, Score: 2}},
		Players: []Player{{Username: "a1", TeamIndex: 0}, {Username: "d1", TeamIndex: 1}}},
	}
	r.roundEnd()
	for _, s := range r.PlayerStats() {
		if s.OneVx != 0 || s.ClutchDetail != nil {
			t.Fatalf("%s: want no clutch in a round without a result, got 1vX=%d detail=%v", s.Username, s.OneVx, s.ClutchDetail)
		}
	}
}

func TestRoundDecided(t *testing.T) {
	r := &Reader{}
	if r.roundDecided() {
		t.Fatal("no team won: want false")
	}
	r.Header.Teams[1].Won = true
	if !r.roundDecided() {
		t.Fatal("team 1 won: want true")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./dissect/ -run 'TestPlayerStats_UndecidedRoundHasNoClutch|TestRoundDecided'`
Expected: build failure `r.roundDecided undefined`. Without `TestRoundDecided`: `a1: want no clutch in a round without a result, got 1vX=1 detail=<nil>` (checked with Task 1 applied and `stats.go` at bd6ccbe).

- [ ] **Step 3: Implement**
  - `stats.go`: add `roundDecided()` next to `NumPlayers`. Change `if lastWinnerStanding > -1 {` to `if lastWinnerStanding > -1 && r.roundDecided() {`, with the comment `// a round without a result has no clutch`. Change only that line, so that PR #3's `oneVx = clutch.OpponentsAlive` inside the block merges cleanly.
  - `match.go`: in the round sheet, when `!r.roundDecided()`, write `""` in the "Winning team" and "Win condition" value cells instead of the `fmt.Sprintf("%s [%d]", …)` and `WinCondition` values. Keep the row labels so the layout does not shift.

- [ ] **Step 4: Run to verify**

Run: `go test ./dissect/... && go vet ./...`
Expected: all `ok`, vet silent.

- [ ] **Step 5: Commit** — `fix: no 1vX or Excel winner for a round without a result`

### Task 3: real-replay golden fixture and docs

**Files:**
- Create: `dissect/test/data/replays/valid/Y11S1/custom_unscored_round.rec`, `…/custom_unscored_round.rec.json`
- Modify: `README.md`

- [ ] **Step 1: Add the fixture** (skip if item 15 already added this blob; then use its path)

```bash
git fetch -q https://github.com/nicktstewart/r6-dissect.git Y11S1-changes
mkdir -p dissect/test/data/replays/valid/Y11S1
git show FETCH_HEAD:dissect/test/data/replays/defuser_detection/match2/Match-2026-03-08_19-55-32-26676-R05.rec \
  > dissect/test/data/replays/valid/Y11S1/custom_unscored_round.rec
sha256sum dissect/test/data/replays/valid/Y11S1/custom_unscored_round.rec
```
Expected: `18838c8faa6087f972df035572869c8b21426378c9441038d91213c247d12ec0`, 29,569 bytes. (The commit is `df89e6d012333ff207b0cfba5dc5a4f58c94dd90`; the blob is `e1ee37ca6efff89b86b7e35a18b2ef832a08c464`.)

- [ ] **Step 2: Generate and inspect the golden**

Run: `go test ./dissect/test -run TestReader_Read -update && go test ./dissect/test`
Expected: `ok`. Then check that only the new `.json` appeared (`git status --short` shows the new `Y11S1/` files and nothing else) and that `header.teams` in it holds `"startingScore": 2, "score": 2, "won": false` for both teams with no `winCondition`, and `"matchFeedback": null` (verified in the prototype).

- [ ] **Step 3: Confirm the red/green on the real file**

Run: `git stash push -- dissect/time.go dissect/stats.go && go test ./dissect/test -run 'TestReader_Read$/data/replays/valid/Y11S1/custom_unscored_round' ; git stash pop`
Expected while stashed: `FAIL`, with `Teams.array[1].Won: true != false` and `Teams.array[1].WinCondition: Time != ` (verified). After the pop, re-running the same `go test` passes. (The subtest name is the file path, so the `-run` pattern must spell out each `/` level.)

- [ ] **Step 4: Corpus no-regression check** (if the user's replays are still present)

Build at `origin/main` and at the branch, run `r6-dissect -f json -o <out> <folder>` on each `Match-*` folder under the two corpus directories named in the brief, and `cmp` the outputs.
Expected: byte-identical for all 5 folders (no real round there has flat scores).

- [ ] **Step 5: README** — after the "Example output (abbreviated)" JSON block, add: "A round whose replay holds no result (neither header score moved: the round was abandoned or remade, or the recording ended first) has `"won": false` on both teams and no `winCondition`."

- [ ] **Step 6: Commit** — `test: Y11S1 replay of a round recorded without its result` (the README line goes in this commit, as part of the test/fixture change; it is not a docs-only commit, see the commit-label rule).

## Acceptance

- `go test ./dissect/ -run 'TestRoundEnd_NeitherScoreMovedHasNoWinner|TestRoundEnd_UndecidedRoundKeepsFeedAsIs|TestHeaderWinner|TestPlayerStats_UndecidedRoundHasNoClutch'` is red at bd6ccbe and green on the branch.
- `go test ./dissect/test` is green, and the only new golden is `Y11S1/custom_unscored_round.rec.json`, with both `won: false`.
- `r6-dissect -f json` on the nicktstewart `match2 … R05.rec` shows both teams `won: false`, no `winCondition`, and no `1vX` in `stats`. On `match3 … R05.rec`, team 1 still has `won: true` with `Time`.
- The user's 5 real match folders produce byte-identical JSON before and after.

## Risks / open questions

- **R6Stats breaks on upgrade** if 07b is not done first (one such round fails the whole upload). This is mitigated by the submodule pin and D8.
- **Single real example.** It is a 1v1 custom game with 0 events. We have no ranked or competitive "remake" sample. The rule rests on the header, not on events, so the sample's shape matters little. If a real replay ever shows a flat score on a round the game did decide (for example, a header written before the score update), the right fix is still "no winner from this file". Another recording of the round supplies the result.
- **Both-rose case** is assumed impossible; -1 is safe either way.
- **D5 follow-up** (post-plant wipe labelled `DefusedBomb`) is unverified against in-game ground truth. It needs its own item.
- The fixture's 0-event feed serializes as `"matchFeedback": null` (the reader's existing behaviour; R6Stats `_req_list` would reject `null`, but in this round the file is undecided and skipped by 07b anyway). Do not normalize it in this change; item 01 may care.
