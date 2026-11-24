# Fork port 03: a failing listener must not end the round (design + implementation plan)

> **Issue:** #31 · prestonwillis10/r6-dissect · fork survey: `docs/research/2026-10-21-fork-survey.md`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When one listener fails on one packet (most often a `Seek` that runs to EOF), `Reader.Read` skips that packet, keeps calling every later listener, runs the end-of-round work, and records the failure in a per-round `warnings` list in the JSON output.

**Architecture:** In the `Reader.Read` listener loop, a listener error is recorded with `(*Reader).listenerFailed` and the loop continues, where today it returns. `Read` then always reaches `roundEnd`, `applyLoadouts` and `r.b = nil`. A new exported `ReadWarning` type and a `Reader.Warnings` field carry the failures. The two round JSON shapes (single round, and each round of a match) and the FFI round shape gain `warnings,omitempty`. `Seek` gets an exact fast path through `bytes.Index`, so k failures no longer cost k byte-by-byte scans of the round.

**Tech Stack:** Go (module `github.com/redraskal/r6-dissect`), zerolog. No new dependencies.

**Spec:** this document. The design sections come before the plan.

Verified against: `origin/main` bd6ccbeb7b46af99638df5d650187069e849f925.

## Global Constraints

- Base branch: `main` at bd6ccbe or later. Go: `export PATH=$HOME/.local/go/bin:$PATH`.
- JSON key: `warnings`, always `omitempty`. A clean round's JSON must stay byte-identical to today's.
- `ReadWarning` JSON keys, exactly: `listener`, `pattern`, `error`, `count`, `firstOffset`.
- `pattern` format: `fmt.Sprintf("% X", pattern)` (upper-case hex with single spaces, for example `22 07 94 9B DC`).
- `Reader.Read()` keeps returning `error` (callers use `!dissect.Ok(err)`). After this change it returns `nil` once `NewReader` has succeeded.
- No golden `.rec.json` file may change. `go test ./dissect/test -run TestReader_Read` must pass without `-update`.
- Commit labels: a commit that changes code is `fix(...)` or `feat(...)`, never `docs(...)`.

## Review Focus

1. **A listener that fails on every packet** (for example a marker that moved in a new build): the round must still finish, there must be one merged warning with the right `count`, and the run time must stay bounded. Pinned by `TestRead_ListenerFailuresAreRecorded` (Task 1) and the `Seek` fast path (Task 3).
2. **A non-EOF listener error** (`readMatchFeedback` returns `match feedback failed valid check` on Y9S1 to Y9S1Update3): it must not abort the round or the match either. Pinned by `TestRead_ListenerHardErrorDoesNotStopRound` (Task 1).
3. **A failure in round N of a match folder**: every later round must still be read, and `WriteJSON` must not panic. Pinned by `TestMatchReader_ListenerFailureReadsEveryRound` (Task 2).
4. **The `Seek` fast path changing a result**: the existing byte walk misses some overlapping matches (`22 22 07` when looking for `22 07`). The fast path must keep that exact behaviour; fixing the walk belongs to item 05. Pinned by `TestSeek_SameResultAsWalk` (Task 3).
5. **Two listeners on the same pattern**: when the second one fails, the first one's work must stay (`readPlayer` plus an extra listener on `22 07 94 9B DC`). Pinned by the player-count assertion in `TestRead_ListenerFailuresAreRecorded` (Task 1).

---

## Summary

`Reader.Read` runs every listener, in pattern-offset order, over the decompressed round. On the first listener that returns an error, it returns from `Read`. It skips every later listener, `stopDefuserRun`, `roundEnd`, `backfillLeadingUpdates`, `applyLoadouts` and `r.b = nil`. Listener errors are almost always `io.EOF`, from a `Seek` whose marker is not in the rest of the buffer, and `dissect.Ok(io.EOF)` is true. So every caller (`main.go`, `exports.go`, the README example) treats the truncated round as a success. On a match folder, `MatchReader.Read` stops at that round, the later rounds stay `nil`, and the CLI panics in `WriteJSON`.

No replay in our corpus triggers this today (0 of 117 rounds, see Current behaviour). The bug shows up when a new game build moves a marker that a listener seeks. aa-sasha hit it on `Y11S2_Alpha03`: their commit says `readPlayer` seeked 60.4 MB to EOF and the site went missing on 968 league rounds. Season drift makes this a matter of when, not if, and the failure loses the whole round silently, or panics the whole match.

The fix: continue past a failing listener, record the failure as a per-round warning (JSON plus one log line), and make a failing `Seek` cheap.

## Source

- **aa-sasha `cec8ad7`** ("fix(y11s2): decode Alpha03 player markers", 2026-08-27, branch `aa-sasha/codex/y11s2-alpha03-player-markers`), `dissect/reader.go`:
  - The loop becomes `if lerr := listener(r); lerr != nil { if !Ok(lerr) { err = lerr; return }; log.Debug()...Msg("listener skipped") }`, then `err = nil`.
  - Adds `SeekBounded(pattern, max)` and `ErrSeekLimit`, which no caller uses.
  - Adds the pattern hex to the `large seek` log.
  - The comment (in Russian) records the incident: on Y11S2_Alpha03 a false player packet sent `Seek` 60.4 MB to EOF, the loop exited, and the site packet at 55 KB was never read, on 968 league rounds.
  - Flaws: the failure is logged at **Debug**, which the CLI does not print by default (`main.go` sets `zerolog.InfoLevel`), so the loss is still silent. A non-EOF error still aborts. Each failed `Seek` still walks to EOF byte by byte. `SeekBounded` is dead code.
- **nicktstewart `f25aa59`** ("[Fix] matchFeedback is now populated", 2026-07-08), `dissect/reader.go`: `if Ok(err) { err = nil; continue }`. Flaws: there is no record at all, and a non-EOF error still aborts.
- **nicktstewart `dd6fbc1`** ("[Fix] Header location", 2026-06-14), `dissect/player.go`: makes the `uiID` and `profileID` seeks in `readPlayer` tolerant (it saves the offset, `Seek`, restores on `Ok(err)`) and adds a Y11S2_1 `idIndicator`. These are per-marker fixes. **Not ported here.** They belong to item 02 (build 9734089 markers).

None of the sources has a test for the loop behaviour.

## Current behaviour

### Code at bd6ccbe

`dissect/reader.go` `Read()`, listener loop and end:

```go
		for _, listener := range r.listeners[entry.listenerIndex] {
			r.offset = entry.offset + 1
			n := len(r.MatchFeedback)
			if err = listener(r); err != nil {
				return
			}
			r.stampNew(n)
		}
	...
	if !r.readPartial {
		...
		r.roundEnd()
		...
		r.applyLoadouts()
	}
	r.b = nil
	return err
```

- `dissect/error.go`: `return err == nil || errors.Is(err, io.EOF) || errors.Is(err, zstd.ErrMagicMismatch)`.
- `dissect/reader.go` `Seek`: walks `r.Bytes(1)` until the pattern matches. At EOF it logs `large seek` (Warn) and returns `io.EOF`.
- `dissect/reader.go` `Skip`: `if r.offset >= len(r.b) { return io.EOF }`. Any `Bytes`/`Uint64`/`String` read past the end also gives `io.EOF`.
- The listeners that `Seek` are `readPlayer` (`dissect/player.go`: `r.Seek([]byte{0x40, 0xF2, 0x15, 0x04})`, `r.Seek(idIndicator)`, `r.Seek(spawnIndicator)`, `r.Seek([]byte{0x38, 0xDF, 0xEE, 0x88})`, `r.Seek(profileIDIndicator)`) and pre-Y9S1 `readMatchFeedback` (`dissect/feedback.go`: `r.Seek(activity2)`).
- The only non-EOF listener error is `dissect/feedback.go` `return errors.New("match feedback failed valid check")` (Y9S1 to Y9S1Update3).
- `dissect/match.go` `MatchReader.Read`: `if err := m.read(i); err != nil { return err }`. Later rounds stay `nil`.
- `main.go` `writeMatch`: `if err := m.Read(); !dissect.Ok(err) { return err }`, then `m.WriteJSON(out)`. `Data()` dereferences every round.
- `Reader.Read` returns non-nil **only** through the listener loop. Nothing else in `Read` sets `err`.

### Reproductions (run at bd6ccbe in a scratch clone)

The scratch test file `repro03_test.go` (package `dissect`) is kept at `docs/research/fork-port-prototypes/03/repro03_test.go.txt`. Command: `go test ./dissect -run TestRepro03 -v`.

**1. One extra listener on the clock pattern `1F 07 EF C9` does `r.Seek(<absent 10-byte pattern>)` at the halfway tick.** Everything else in the buffer is intact, so the difference is caused only by the abort:

| fixture | clean read | with one mid-round listener EOF |
|---|---|---|
| Y8S1 `ranked_1` | feedback=12 (kills 6), won=[false true] win=Time, loadouts=10 | **err=EOF, Ok=true**, feedback=5 (kills 0), won=[false false], loadouts=0 |
| Y9S1 `custom_1` | feedback=9 (kills 7), win=KilledOpponents, loadouts=10, sbScore=775 | **err=EOF**, feedback=3 (kills 1), won=[false false], loadouts=0, sbScore=0 |
| Y11S2 `custom_solid_snake` | feedback=15 (kills 9), win=DefusedBomb, loadouts=10, sbScore=8095 | **err=EOF**, feedback=5 (kills 1), won=[false false], loadouts=0, sbScore=5925 |
| Y11S2 `custom_early_plant` | feedback=13 (kills 8), win=DefusedBomb, loadouts=10, sbScore=2275 | **err=EOF**, feedback=3 (kills 0), won=[false false], loadouts=0, sbScore=415 |

In every faulted run, the buffer (`r.b`, 27 to 165 MB) is also not released.

**2. A realistic marker loss: the profileID marker `8A 50 9B D0` of the 10th player is absent** (XOR-ed in the buffer):

| fixture | clean | marker absent, today | marker absent, with the fix |
|---|---|---|---|
| Y9S1 `custom_1` | 10 players, 9 events, site set | **9 players, 0 events, no site, no winner**, err=EOF | 9 players, 9 events, site set, winner set |
| Y11S2 `custom_solid_snake` | 10 / 15 | **9 / 0, no site** | 9 / 15, site set |
| Y11S2 `custom_early_plant` | 10 / 13 | **9 / 0, no site** | 9 / 13, site set |

The missing 10th player is item 02's problem. The loss of all 13 to 15 events, the site and the winner is this item's problem.

**3. Through the CLI path (`MatchReader`):** two Y11S2 rounds in a folder, with an EOF listener in round 1. Result: `MatchReader.Read err=EOF Ok(err)=true rounds=2 nil=[false true]`, then `WriteJSON panicked: runtime error: invalid memory address or nil pointer dereference`. This is item 01's crash, reached through item 03.

**4. Cost of a failing Seek:** a full `Seek` to EOF on the 165 MB `ranked_1` buffer takes **~600 ms**. `bytes.Index` for the same absent pattern takes ~11 to 17 ms.

**5. Corpus scan (no natural trigger today):** 117 rounds were read: our 53 (16 fixtures, 2 Y11S2 and 3 Y11S3 real matches) plus 64 rounds committed on fork branches. **0** of them left `Read` with an error or an unreleased buffer, and the CLI printed no `large seek` on any of them. A truncated compressed `.rec` fails in `NewReader` (`unexpected EOF` from zstd), not in a listener, so truncation does not reproduce this item. That is item 01/06 territory.

**6. Item 01 has its own trigger:** a match folder with an empty `R02.rec`, or one truncated to 2000 bytes, panics the CLI (`rc=2`, nil pointer) both before and after this fix. `NewReader` fails, and `m.read` returns before setting `m.rounds[i]`.

## Approaches considered

**A. Continue only on `Ok` errors, silently or at Debug level** (nicktstewart `f25aa59`, aa-sasha `cec8ad7`).
- Pro: a one-line diff.
- Con: the data loss becomes invisible. Today a failure at least makes the round obviously broken (no winner) or crashes the match, so someone notices. With A, a round could lose half its kills and nobody would know.
- Con: hard errors still kill the match.

**B. Continue on every listener error and record it** (chosen). Every listener error is a per-packet failure: the decompressed buffer is still intact, and the next listener starts from its own offset (`r.offset = entry.offset + 1`). Each failure is surfaced in three ways:
1. one Warn log line (stderr; R6Stats keeps the tail on failure);
2. `Reader.Warnings` for library users;
3. a `warnings` array on each round in the JSON. R6Stats stores the full round object in `rounds.raw`, so the warnings are persisted with no R6Stats change.

**C. Continue, and return an aggregated non-nil error from `Read`.** Rejected: every caller uses `!dissect.Ok(err)`, so a non-EOF aggregate error would make the CLI fail the whole match, the opposite of the goal. An `io.EOF`-wrapping aggregate error would be swallowed by `Ok` anyway.

**D. Count only** (a `listenerFailures int`). Rejected: not actionable. You cannot tell which marker broke.

For **surfacing**, these were considered:
- a per-round list (chosen: it keeps round attribution, matches PR #10's per-round `timelineError` precedent, and the match level can be derived);
- a match-level list (loses the round);
- log only (R6Stats drops stderr when the exit code is 0).

For **performance**, these were considered:
- **P1. No change.** Continuing turns 1 scan to EOF into k scans. At ~0.6 s each on a 165 MB round, a marker that is gone for all 10 players costs 6 s per round, and a kill-feed marker gone for 30 kills costs 18 s per round. R6Stats' timeout is 600 s per folder.
- **P2. Bounded seek** (aa-sasha `SeekBounded`). Every call site needs a window size, and nobody has measured those. A window that is too small changes output when a marker is legitimately far away. Rejected. This kind of per-marker tolerance belongs to item 02.
- **P3. An exact fast-fail inside `Seek`** (chosen). Before the walk: `if bytes.Index(r.b[start:], pattern) < 0 { r.offset = len(r.b); return io.EOF }`. `bytes.Index` reports every real occurrence, and the walk only ever reports real occurrences. So when `bytes.Index` finds none, the walk would fail too, and the result is identical. Measured: ~600 ms became ~38 to 58 ms per failure.
- **P4. Replace the walk with `bytes.Index`.** Faster on success too, but it changes results. The walk misses overlapping matches, because it restarts after a mismatched byte without re-checking it. Changing that is item 05's call, with its own golden review.

## Decisions

1. **Continue on every listener error, EOF or not** (Approach B). Reason: no listener uses an error to mean "the round is over". The only hard error (`match feedback failed valid check`) is a per-packet sanity check, like the many `return nil` skips in the same listeners.
2. **`Read` returns `nil` after `NewReader` succeeded**, and `Reader.Read()`'s signature becomes `func (r *Reader) Read() error` (the named result is unused). `ReadPartial` inherits this. Reason: callers keep working unchanged; warnings are the channel.
3. **Warnings are merged** by `(listener, pattern, error)` and carry a `count` and a `firstOffset`. `firstOffset` is where the pattern match **starts** in the decompressed round, which is `entry.offset - len(pattern) + 1`, because the worker reports the index of the pattern's last byte. Reason: a listener that fails on every tick must not add hundreds of JSON entries, and `firstOffset` equals what `bytes.Index` gives, which is easy to check in a hex editor.
4. **The listener name** comes from `runtime.FuncForPC(reflect.ValueOf(listener).Pointer()).Name()`, with the package path cut: everything up to the last `/`, then up to the first `.`. That gives `readPlayer` for named functions, and `(*Reader).listenLoadouts.func1` or `TestX.func1` for closures. Reason: no change to `Listen`'s signature, so MatchReader and external callers keep working.
5. **Log every failure at Warn**, with listener, pattern, offset and error. `Seek`'s existing `large seek` Warn stays, now with the pattern hex (from aa-sasha) and still with the caller's function name. Reason: Warn is visible at the CLI's default Info level, and 2 lines per failure is acceptable because `Seek` failures are now fast.
6. **Use the exact `Seek` fast-fail (P3), not a bounded seek.**
7. **Out of scope** (each is a separate item): per-marker tolerant seeks inside `readPlayer` (item 02); `MatchReader` handling of rounds whose `NewReader` fails, and nil rounds in `Data`/`WriteExcel` (item 01); fixing the `Seek` overlap miss or rewriting pattern search (item 05); showing warnings in the Excel output; R6Stats turning round `warnings` into upload warnings (an R6Stats follow-up).
8. **Plan against `main`, land before PR #10.** Task 5 gives the exact conflict resolution for `track/rec-timeline`. If PR #10 has merged by the time this is implemented, do Task 5 inside this branch instead. Reasons:
   - the loop is the same on main, PR #3 and PR #12, and the scratch merges with `fork/health-dbno` and PR #12 were clean;
   - main is what R6Stats deploys;
   - the fix *removes* `settleTimelineOnStop`, the path PR #10's review had to patch (run log W5 R8-1), so PR #10 gets simpler;
   - the conflict was resolved and fully tested in a scratch merge (all five rec-timeline packages pass).
9. **A builder check was done as a self-review** (autonomous mode), not by a subagent.

## Interactions

- **Item 01 (incomplete match, nil rounds, panic).** 01 owns the match level: what `MatchReader.Read` does when a round cannot be opened or decompressed (empty file, truncated zstd), and guarding `Data`/`WriteExcel` against nil rounds. 03 removes one trigger of 01: after 03, `Reader.Read` never returns a listener error, so `m.read` can only fail in `os.Open`/`NewReader`. 01's other triggers stay (repro 6 above). Either can land first. If 01 lands first with a `continue on Ok(err)` in `MatchReader.Read` (defiancer6s `c625b8d`), that is harmless after 03.
- **Item 02 (build 9734089 markers).** 02 stops `readPlayer` failing on that build in the first place. 03 makes any remaining or future marker failure cost one packet instead of the round, and shows it in `warnings`. 02 should not need its own loop change. If 02 copies nicktstewart's tolerant seeks, they still work (a tolerated seek never reaches the loop).
- **Item 05 (pattern search, `bytes.Index` rewrite).** If 05 rewrites `Seek` on `bytes.Index`, Task 3's fast path becomes redundant and can be folded in. `TestSeek_SameResultAsWalk` case `overlap the walk misses` is then *expected* to change, and 05 updates it.
- **PR #10 (`track/rec-timeline`).** Conflicts in `dissect/reader.go` (the struct fields and the loop's `r.settleTimelineOnStop(err)`), `dissect/match.go` (the `round` struct with `TimelineError`) and `main.go` (`writeRound` rewritten). The resolution is in Task 5. Its existing `TestRead_ListenerEOFStillSettlesTimeline` (`dissect/timeline_internal_test.go`) still passes after the resolution. Its README sentence "A round read stopped by a listener error gives the reason `round read stopped: <error>`" must be replaced.
- **PR #3 (`fork/health-dbno`) and PR #12.** A scratch merge onto the prototype was clean.
- **JSON / R6Stats.** A new optional key, `warnings`, appears only on a round that had failures. `api/r6stats/services/parser_contract.py` `normalize_round` ignores unknown keys and keeps `raw=raw`, which `rounds.raw` stores. Today such a round either fails the upload with `ParserContractError("teams", "...exactly one team must have won the round")`, because `roundEnd` never ran, or crashes the CLI (later rounds nil). After the fix, the upload succeeds with the data that was read. Follow-up (not filed here): R6Stats could copy round `warnings` into `upload.warnings` (`api/r6stats/worker/jobs.py` `_add_warnings`).
- **Golden-test churn: none.** The goldens hold `{header, matchFeedback}` only (`dissect/test/reader_test.go` `writeRoundExpected`), and no fixture fails a listener today. Verified: `go test ./...` passes on the prototype, and the CLI output of two real matches (Y11S3 `Match-2026-10-18_18-19-54-14012`, Y11S2 `Match-2026-08-25_19-56-00-32000`) is byte-identical before and after.

---

## Implementation plan

### File structure

- Create `dissect/warning.go`: the `ReadWarning` type, `(*Reader).listenerFailed`, `listenerName`.
- Modify `dissect/reader.go`: the `Warnings` field, the `Read` loop and its return, the `Seek` fast path, `logLargeSeek`.
- Modify `dissect/match.go` `Data()`, `main.go` `writeRound`, `exports/exports.go` (single-round branch), `README.md`.
- Create `dissect/listener_failure_test.go` and `dissect/seek_test.go` (package `dissect`, internal: they need `r.b`).

A verified prototype of all of this, at bd6ccbe, is at `docs/research/fork-port-prototypes/03/` (`proto.diff`, `warning.go`, `listener_failure_test.go`, `seek_test.go`). Use it as a reference, not as a patch to apply blindly.

### Task 1: The listener loop continues and records failures

**Files:**
- Create: `dissect/warning.go`
- Modify: `dissect/reader.go` (the `Reader` struct, `Read`)
- Test: `dissect/listener_failure_test.go`

**Interfaces:**
- Produces:
  - `type ReadWarning struct { Listener string \`json:"listener"\`; Pattern string \`json:"pattern"\`; Error string \`json:"error"\`; Count int \`json:"count"\`; FirstOffset int \`json:"firstOffset"\` }`
  - the field `Reader.Warnings []ReadWarning \`json:"warnings,omitempty"\`` (after `Scoreboard`)
  - `func (r *Reader) listenerFailed(listenerIndex int, listener func(*Reader) error, offset int, err error)`
  - `func listenerName(listener func(*Reader) error) string`
  - `func (r *Reader) Read() error`
  - the test helpers `openFixture`, `seekAbsent`, `absentPattern`, `clockPattern`, `playerPattern`, `failureFixture` (Task 2 uses them).

- [ ] **Step 1: Create a branch.** `git checkout -b fix/listener-eof-resilience origin/main`

- [ ] **Step 2: Write the failing tests** in `dissect/listener_failure_test.go`:

```go
package dissect

import (
	"bytes"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
)

const failureFixture = "test/data/replays/valid/Y11S2/custom_early_plant.rec"

var (
	clockPattern  = []byte{0x1F, 0x07, 0xEF, 0xC9}       // readTime, Y8S1+
	playerPattern = []byte{0x22, 0x07, 0x94, 0x9B, 0xDC} // readPlayer
	// absentPattern occurs in no fixture, so a Seek for it runs to EOF.
	absentPattern = []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x03, 0x03, 0x03, 0x03, 0xFE, 0xED}
)

// seekAbsent is a listener whose Seek always runs to EOF, like a listener whose marker moved.
func seekAbsent(r *Reader) error { return r.Seek(absentPattern) }

func openFixture(t *testing.T, path string) *Reader {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(r.b, absentPattern) {
		t.Fatalf("%s contains absentPattern; pick another", path)
	}
	return r
}

// One listener hits EOF halfway through the round. Everything the other listeners read,
// before and after that point, and the end-of-round work, must be the same as a clean read.
func TestRead_ListenerEOFDoesNotStopRound(t *testing.T) {
	clean := openFixture(t, failureFixture)
	ticks := 0
	clean.Listen(clockPattern, func(*Reader) error { ticks++; return nil })
	if err := clean.Read(); err != nil {
		t.Fatalf("clean Read: %v", err)
	}
	faulted := openFixture(t, failureFixture)
	calls := 0
	faulted.Listen(clockPattern, func(r *Reader) error {
		calls++
		if calls == ticks/2 {
			return r.Seek(absentPattern)
		}
		return nil
	})
	if err := faulted.Read(); err != nil {
		t.Fatalf("Read returned %v; a listener failure must not fail the round", err)
	}
	if !reflect.DeepEqual(faulted.MatchFeedback, clean.MatchFeedback) {
		t.Fatalf("matchFeedback differs: got %d events, want %d", len(faulted.MatchFeedback), len(clean.MatchFeedback))
	}
	if !reflect.DeepEqual(faulted.Header, clean.Header) {
		t.Fatal("header differs (winner, loadouts or players)")
	}
	if !reflect.DeepEqual(faulted.Scoreboard, clean.Scoreboard) {
		t.Fatal("scoreboard differs")
	}
	if faulted.b != nil {
		t.Fatal("Read did not finish: the buffer was not released")
	}
	if len(faulted.Warnings) != 1 || faulted.Warnings[0].Count != 1 || faulted.Warnings[0].Error != io.EOF.Error() {
		t.Fatalf("want one EOF warning with count 1, got %+v", faulted.Warnings)
	}
}

// Failures of one listener are merged into one warning that names it.
func TestRead_ListenerFailuresAreRecorded(t *testing.T) {
	r := openFixture(t, failureFixture)
	var starts []int
	for i := 0; ; {
		j := bytes.Index(r.b[i:], playerPattern)
		if j < 0 {
			break
		}
		starts = append(starts, i+j)
		i += j + 1
	}
	r.Listen(playerPattern, seekAbsent)
	if err := r.Read(); err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []ReadWarning{{Listener: "seekAbsent", Pattern: "22 07 94 9B DC", Error: "EOF", Count: len(starts), FirstOffset: starts[0]}}
	if !reflect.DeepEqual(r.Warnings, want) {
		t.Fatalf("warnings:\n got %+v\nwant %+v", r.Warnings, want)
	}
	if len(r.Header.Players) != 10 {
		t.Fatalf("readPlayer shares the pattern and must still run: got %d players", len(r.Header.Players))
	}
}

// A listener error that is not EOF is a per-packet failure too.
func TestRead_ListenerHardErrorDoesNotStopRound(t *testing.T) {
	clean := openFixture(t, failureFixture)
	if err := clean.Read(); err != nil {
		t.Fatal(err)
	}
	r := openFixture(t, failureFixture)
	r.Listen(playerPattern, func(*Reader) error { return errors.New("boom") })
	if err := r.Read(); err != nil {
		t.Fatalf("Read returned %v; want nil", err)
	}
	if len(r.MatchFeedback) != len(clean.MatchFeedback) {
		t.Fatalf("got %d events, want %d", len(r.MatchFeedback), len(clean.MatchFeedback))
	}
	if len(r.Warnings) != 1 || r.Warnings[0].Error != "boom" {
		t.Fatalf("want one \"boom\" warning, got %+v", r.Warnings)
	}
}

// A clean read records no warnings.
func TestRead_CleanRoundHasNoWarnings(t *testing.T) {
	r := openFixture(t, failureFixture)
	if err := r.Read(); err != nil {
		t.Fatal(err)
	}
	if r.Warnings != nil {
		t.Fatalf("want no warnings, got %+v", r.Warnings)
	}
}
```

- [ ] **Step 3: Run the tests and check that they fail.**
Run: `go test ./dissect -run 'TestRead_(ListenerEOFDoesNotStopRound|ListenerFailuresAreRecorded|ListenerHardErrorDoesNotStopRound|CleanRoundHasNoWarnings)' -v`
Expected: build failure `undefined: ReadWarning` / `r.Warnings undefined`. Without the Warnings lines, the first test fails with `Read returned EOF; a listener failure must not fail the round`. That was verified at bd6ccbe.

- [ ] **Step 4: Implement.**
  - In `dissect/warning.go`, add `ReadWarning`, with a doc comment saying the packet was skipped and the rest of the round was read.
  - `listenerFailed`:
    - `pattern := fmt.Sprintf("% X", r.queries[listenerIndex])`;
    - `start := offset - len(r.queries[listenerIndex]) + 1`;
    - log once with `log.Warn().Str("listener", name).Str("pattern", pattern).Int("offset", start).Err(err).Msg("listener failed; skipping this packet, the rest of the round is still read")`;
    - then either increment `Count` on the existing entry with equal `Listener`, `Pattern` and `Error` (`err.Error()`), or append `{..., Count: 1, FirstOffset: start}`.
  - `listenerName`: as in Decision 4. Return `"unknown"` when `FuncForPC` is nil.
  - In `dissect/reader.go`:
    - add the field;
    - change `func (r *Reader) Read() (err error)` to `func (r *Reader) Read() error`;
    - replace the loop body's `if err = listener(r); err != nil { return }` with `if err := listener(r); err != nil { r.listenerFailed(entry.listenerIndex, listener, entry.offset, err) }`, keeping `r.stampNew(n)` after it;
    - replace the final `return err` with `return nil`;
    - add to `Read`'s doc comment (it has none; add one): "Read returns an error only for an unusable reader. A listener that fails is skipped for that packet and recorded in Warnings."
  - Run `gofmt -w dissect/reader.go dissect/warning.go`.

- [ ] **Step 5: Run the tests and check that they pass.** Same command. Expected: 4 PASS. `TestRead_ListenerFailuresAreRecorded` runs 10 full-round `Seek`s and takes ~1 s now, faster after Task 3.

- [ ] **Step 6: Commit.**
```bash
git add dissect/warning.go dissect/reader.go dissect/listener_failure_test.go
git commit -m "fix(reader): a failing listener skips its packet instead of ending the round"
```

### Task 2: Warnings in the JSON outputs

**Files:**
- Modify: `dissect/match.go` `Data()`, `main.go` `writeRound`, `exports/exports.go` (the `round` struct in the single-file branch), `README.md`
- Test: `dissect/listener_failure_test.go` (append)

**Interfaces:**
- Consumes: `ReadWarning`, `Reader.Warnings`, `seekAbsent`, `playerPattern`, `failureFixture` (Task 1).
- Produces: the JSON key `warnings` on each round object.

- [ ] **Step 1: Write the failing tests** (append; add the imports `encoding/json` and `path/filepath`):

```go
func copyFixtures(t *testing.T, names ...string) *os.File {
	t.Helper()
	dir := t.TempDir()
	for i, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "R0"+string(rune('1'+i))+".rec"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// A listener failure in round 1 must not leave round 2 unread (nil), and the match JSON
// carries the warnings on the round they happened in.
func TestMatchReader_ListenerFailureReadsEveryRound(t *testing.T) {
	m, err := NewMatchReader(copyFixtures(t, failureFixture, "test/data/replays/valid/Y11S2/custom_solid_snake.rec"))
	if err != nil {
		t.Fatal(err)
	}
	m.Listen(playerPattern, seekAbsent)
	if err := m.Read(); err != nil {
		t.Fatalf("MatchReader.Read: %v", err)
	}
	for i := 0; i < m.NumRounds(); i++ {
		if m.rounds[i] == nil {
			t.Fatalf("round %d was not read", i+1)
		}
	}
	var buf bytes.Buffer
	if err := m.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Rounds []struct {
			Warnings []ReadWarning `json:"warnings"`
		} `json:"rounds"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for i, rd := range out.Rounds {
		if len(rd.Warnings) != 1 || rd.Warnings[0].Listener != "seekAbsent" {
			t.Fatalf("round %d warnings: got %+v", i+1, rd.Warnings)
		}
	}
}

// The match JSON has no "warnings" key on a round that had none.
func TestMatchReader_CleanRoundOmitsWarnings(t *testing.T) {
	m, err := NewMatchReader(copyFixtures(t, failureFixture))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Read(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := m.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte(`"warnings"`)) {
		t.Fatal(`clean match JSON contains "warnings"`)
	}
}
```

- [ ] **Step 2: Run the tests and check the result.** `go test ./dissect -run 'TestMatchReader_' -v`. Expected: `ListenerFailureReadsEveryRound` FAILs with `round 1 warnings: got []`. (Task 1 already fixed the nil round, so that is the remaining failure.) `CleanRoundOmitsWarnings` passes.

- [ ] **Step 3: Implement.**
  - In `dissect/match.go` `Data()`, add `Warnings []ReadWarning \`json:"warnings,omitempty"\`` as the last field of `type round struct`, and `Warnings: r.Warnings,` in the literal.
  - In `main.go` `writeRound`, add `Warnings []dissect.ReadWarning \`json:"warnings,omitempty"\`` as the last field of `type output struct`, and `r.Warnings,` as the last positional value.
  - In `exports/exports.go`, make the same change to the single-file `round` struct and its literal. The match branch already uses `m.Data()`.
  - In `README.md`, after the "Match feedback fields" paragraph, add a `### Round warnings` section: "A round can carry `warnings` when a part of the replay could not be read (usually a marker that a newer game build moved). The rest of the round is still read; the entries say what may be missing: `listener` (the parser function), `pattern` (the bytes it looks for), `error`, `count`, `firstOffset` (the offset of the first failing match in the decompressed round). The key is absent when nothing failed." In the "Importing a .rec file" example, add one comment line under the `Read()` call: `// r.Warnings lists listeners that failed; the rest of the round was read`.
  - `gofmt -w main.go dissect/match.go exports/exports.go`.

- [ ] **Step 4: Run the tests and check that they pass.** Run `go test ./dissect -run 'TestMatchReader_|TestRead_' -v` and `go build ./... && go vet ./...`. Expected: PASS, and no vet output.

- [ ] **Step 5: Commit.**
```bash
git add dissect/match.go main.go exports/exports.go README.md dissect/listener_failure_test.go
git commit -m "feat(output): per-round warnings for listeners that failed"
```

### Task 3: An exact fast-fail for a `Seek` that cannot match

**Files:**
- Modify: `dissect/reader.go` `Seek`; add `logLargeSeek`
- Test: `dissect/seek_test.go`

**Interfaces:**
- Produces: `func logLargeSeek(start, end int, pattern []byte)` (unexported). `Seek` keeps its signature.

- [ ] **Step 1: Write the tests** in `dissect/seek_test.go`:

```go
package dissect

import (
	"io"
	"testing"
)

func TestSeek_AbsentPatternIsEOF(t *testing.T) {
	r := &Reader{b: []byte{0x01, 0x02, 0x03, 0x04, 0x05}, offset: 1}
	if err := r.Seek([]byte{0x09, 0x09}); err != io.EOF {
		t.Fatalf("want io.EOF, got %v", err)
	}
	if r.offset != len(r.b) {
		t.Fatalf("offset: want %d, got %d", len(r.b), r.offset)
	}
}

// The fast path must not change what Seek finds, including where the walk misses an
// overlapping match (22 22 07: the walk restarts after the second 0x22 and never sees 22 07).
func TestSeek_SameResultAsWalk(t *testing.T) {
	cases := []struct {
		name       string
		b, pattern []byte
		start      int
		wantErr    error
		wantOffset int
	}{
		{"found", []byte{0, 1, 2, 3, 4}, []byte{2, 3}, 0, nil, 4},
		{"found at start", []byte{2, 3, 0, 0}, []byte{2, 3}, 0, nil, 2},
		{"before start is not found", []byte{2, 3, 0, 0, 0}, []byte{2, 3}, 1, io.EOF, 5},
		{"overlap the walk misses", []byte{0x22, 0x22, 0x07, 0x00}, []byte{0x22, 0x07}, 0, io.EOF, 4},
		{"offset at end", []byte{1, 2}, []byte{1}, 2, io.EOF, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &Reader{b: c.b, offset: c.start}
			err := r.Seek(c.pattern)
			if err != c.wantErr || r.offset != c.wantOffset {
				t.Fatalf("got (%v, offset %d), want (%v, offset %d)", err, r.offset, c.wantErr, c.wantOffset)
			}
		})
	}
}
```

- [ ] **Step 2: Run them.** `go test ./dissect -run TestSeek_ -v`. Expected: **PASS already.** They pin today's walk, so they are a characterization guard: the fast path must keep them green. (A speed test is not used, because timing is flaky in CI.)

- [ ] **Step 3: Implement.**
  - At the top of `Seek`, after `start := r.offset`: `if start < len(r.b) && bytes.Index(r.b[start:], pattern) < 0 { r.offset = len(r.b); logLargeSeek(start, r.offset, pattern); return io.EOF }`, with a comment saying why the result is identical (`bytes.Index` finds every match the walk can find).
  - Move the existing `large seek` logging into `logLargeSeek`:
    - use `runtime.Caller(2)`, because 0 is `logLargeSeek`, 1 is `Seek` and 2 is the listener;
    - add `.Str("pattern", fmt.Sprintf("% X", pattern))`;
    - keep the message `large seek`.
  - Call `logLargeSeek` from the walk's `if Ok(err)` branch as well.

- [ ] **Step 4: Run and measure.**
  - `go test ./dissect -run 'TestSeek_|TestRead_|TestMatchReader_' -v`. Expected: all PASS.
  - Then check that a log line names the caller: `go test ./dissect -run TestRead_ListenerFailuresAreRecorded -v 2>&1 | grep -m1 'large seek'`. Expected: a JSON log line with `"func":"github.com/redraskal/r6-dissect/dissect.seekAbsent"` and `"pattern":"DE AD BE EF 03 03 03 03 FE ED"`.
  - Optional timing check (scratch only, not committed): a full `Seek` for an absent pattern on `Y8S1/ranked_1.rec` takes ~600 ms before and ~40 to 60 ms after.

- [ ] **Step 5: Commit.**
```bash
git add dissect/reader.go dissect/seek_test.go
git commit -m "fix(reader): fail a Seek fast when its pattern is not in the rest of the round"
```

### Task 4: Regression on the corpus

**Files:** none changed.

- [ ] **Step 1: Run the full suite.** `go test ./...`. Expected: `ok` for `dissect` and `dissect/test`. `TestReader_Read` passes **without** `-update`, and `git status` shows no changed `.rec.json`.
- [ ] **Step 2: Check that the CLI output is byte-identical** on real matches:
```bash
git stash -u   # or build origin/main in a second worktree
go build -o /tmp/r6d-before . && git stash pop && go build -o /tmp/r6d-after .
R=<scratch>
for d in $R/replays-new/Match-2026-10-18_18-19-54-14012 $R/replays/Match-2026-08-25_19-56-00-32000; do
  /tmp/r6d-before $d > /tmp/b.json 2>/dev/null; /tmp/r6d-after $d > /tmp/a.json 2>/dev/null; cmp /tmp/b.json /tmp/a.json && echo identical
done
```
Expected: `identical` twice. If those paths are gone, use any two match folders. Clean rounds have no `warnings` key.
- [ ] **Step 3:** Run `/ship` (open the PR against `main`; title `fix(reader): a failing listener skips its packet instead of ending the round`; the body says "Closes #<issue>" and credits aa-sasha `cec8ad7` and nicktstewart `f25aa59`).

### Task 5: Carry the change into `track/rec-timeline` (PR #10)

Do this when main (with Tasks 1 to 4) is merged into `track/rec-timeline`. If PR #10 merged before this plan was implemented, do it inside this branch instead. A scratch merge was resolved this way and all of `go test ./...` passed on the result (the `.`, `cmd/dissect-lab`, `dissect`, `dissect/test` and `dissect/wire` packages).

**Files:** `dissect/reader.go`, `dissect/match.go`, `main.go`, `README.md`, `dissect/listener_failure_test.go`

- [ ] **Step 1: Resolve the conflicts.**
  - `dissect/reader.go` struct: keep both sides (`timelineEnabled`, `timeline`, `timelineErr`, then `Warnings`).
  - `dissect/reader.go` loop: keep this change's `if err := listener(r); err != nil { r.listenerFailed(...) }`, and drop `r.settleTimelineOnStop(err)` and `return`.
  - Delete the function `settleTimelineOnStop` and its comment. The normal end-of-round path now always runs `buildTimeline` when `r.timelineEnabled`.
  - `dissect/match.go` `round`: keep `TimelineError` and add `Warnings` after it. Keep both in the literal (`TimelineError: r.timelineErr, Warnings: r.Warnings`).
  - `main.go` `writeRound`: keep rec-timeline's version. Add `Warnings []dissect.ReadWarning \`json:"warnings,omitempty"\`` after `TimelineError` in `type output struct`, and `Warnings: r.Warnings,` after `PlayerStats:` in the `o := output{...}` literal.
  - `gofmt -w` the three files. `grep -rn settleTimelineOnStop --include=*.go .` must print nothing.
- [ ] **Step 2: Replace the README sentence.** Replace "A round read stopped by a listener error gives the reason `round read stopped: <error>`;" in the Timeline section with "A listener that fails no longer stops the round read: it is listed in the round's `warnings`, and the timeline is still built;".
- [ ] **Step 3: Add a test** to `dissect/listener_failure_test.go`:
```go
func TestTimeline_ListenerFailureStillBuildsTimeline(t *testing.T) {
	r := openFixture(t, "test/data/replays/valid/Y11S3/custom_clubhouse_short.rec")
	r.EnableTimeline()
	r.Listen(playerPattern, seekAbsent)
	if err := r.Read(); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if tl, err := r.Timeline(); err != nil || tl == nil {
		t.Fatalf("Timeline: got (%v, %v), want a timeline", tl, err)
	}
	if len(r.Warnings) != 1 {
		t.Fatalf("want one warning, got %+v", r.Warnings)
	}
}
```
The existing `TestRead_ListenerEOFStillSettlesTimeline` (`dissect/timeline_internal_test.go`) stays. Its invariant still holds.
- [ ] **Step 4: Run the tests.** `go test ./...`. Expected: all five packages `ok`.
- [ ] **Step 5: Commit** on the merge, or as `fix(timeline): build the timeline after a listener failure; drop settleTimelineOnStop`.

## Acceptance

- `TestRead_ListenerEOFDoesNotStopRound` with its `Warnings` assertion removed, so that it compiles at bd6ccbe (kept as `docs/research/fork-port-prototypes/03/accept03_test.go.txt`), fails at bd6ccbe with `Read returned EOF; a listener failure must not fail the round` (verified) and passes after Task 1. All tests of Tasks 1 to 3 pass after Task 3.
- `go test ./...` is green, with no golden file changed.
- The CLI output on clean real matches is byte-identical to bd6ccbe (Task 4 Step 2).
- On a round with a failing listener: the round JSON has `warnings` with listener, pattern, error, count and firstOffset; the round still has its winner, loadouts and later events; and the CLI exits 0.

## Risks / open questions

- **Partial state from a failed listener.** A listener that changed state before it failed keeps that change. `readPlayer` increments `r.playersRead` before its seeks, which is the same counting a successful read does, so team assignment is unchanged. No other listener appends a `MatchUpdate` before a fallible read. A future listener must set its state only after its last fallible read. Add that sentence to the `Listen` doc comment in Task 1 if you like (optional).
- **Warn-log volume.** A listener that fails on every clock tick logs about 2 lines per tick (hundreds per round). That is acceptable at the CLI's default level and cheap after Task 3. If it proves noisy, log only the first failure per `(listener, pattern, error)`, because the JSON count carries the rest.
- **The listener name for closures** (`(*Reader).listenLoadouts.func1`) is stable only while the source order of closures is stable. It is a diagnostic, not an API. Nothing should match on it.
- **No natural trigger in our corpus.** The fix is verified with injected faults. When item 02's build 9734089 replay (or any replay with `large seek` in stderr) appears, run it and check that its `warnings` are present and its round still has a winner.
- **`ReadPartial`.** It also records warnings now and reads every match in its first third, where before it stopped at the first failure. `--info` gets marginally slower on a broken round, which is negligible.
