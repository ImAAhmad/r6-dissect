# Fork port 01: incomplete match folders crash the CLI. Design and implementation plan

> **Issue:** #23, #24 · prestonwillis10/r6-dissect · fork survey: `docs/research/2026-10-21-fork-survey.md` · prototype: `docs/research/fork-port-prototypes/01/`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task by task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A match folder with an empty, truncated or otherwise unreadable `.rec` file produces JSON for every readable round, plus a `skippedRounds` list naming each file that could not be read and why. The CLI and the shared library never panic on it.

**Architecture:** `MatchReader.Read` stops aborting on the first bad round file. It records a `*RoundError` per failed file, keeps those rounds `nil`, and returns one `*IncompleteMatchError`. Every loop over `m.rounds` skips `nil`. `Data()` adds an `omitempty` `skippedRounds` array. The CLI and `exports` write the readable rounds and fail only when no round could be read. A separate guard in `Reader.Read` turns the "header but no data" case from a goroutine panic into `ErrNoReplayData`.

**Tech Stack:** Go 1.26, `github.com/rs/zerolog`, the existing `dissect/test` fixture corpus.

**Spec:** this document. Part 1 is the design and Part 2 is the plan.

**Issues:** this document covers two mechanisms, so there are two issue files:
- `01a-incomplete-match-crash`: the `MatchReader` abort, the nil-round panic, and the reporting policy (Tasks 1, 2 and 4).
- `01b-empty-replay-body-panic`: the `Reader.Read` worker index panic on a body with no data (Task 3).

The two land independently. If they ship in one PR, it closes both.

**Prototype:** a throwaway implementation of all four tasks passed every test named below, and `go test ./...`, at bd6ccbe. It is saved in the planning session's scratchpad (`.../4c44237a-.../scratchpad/protoref01/`: `prototype.diff` and the three test files). That directory is ephemeral; this document is the source of truth.

---

# Part 1: Design

## Summary

`MatchReader.Read()` returns on the first round error, and every later round in `m.rounds` stays `nil`. If that error is `io.EOF`, which is what an empty `.rec` or one cut short inside its header gives, `main.writeMatch` treats it as success (`dissect.Ok`) and calls `WriteJSON`. `Data()` then calls `(*Reader).PlayerStats` on a `nil` round and the process panics with exit code 2. R6Stats runs exactly this CLI path for every uploaded folder, so one bad file fails the whole upload with a Go stack trace as its error.

We will skip unreadable round files and report each one in the JSON (`skippedRounds`). The library returns a typed error, and the CLI exits 0 while at least one round was read. A second mechanism, a chunked `.rec` cut between its header and its first zstd frame, panics inside a worker goroutine whether or not this fix lands. It gets its own small guard and its own issue (01b).

## Source

- Fork `defiancer6s-hub`, branch `defiancer6s-hub/fix/incomplete-match-nil-panic`, commit `c625b8d7787b2c7b61cf1627a34861b341426e6a` ("fix: don't panic on incomplete/aborted matches"). It is based on upstream `e6c2ca8` and is not on our main. Files: `dissect/match.go` (+16), `dissect/stats.go` (+5).
- What it does:
  - `MatchReader.Read` carries on past `Ok` (EOF-style) errors and returns the last one. A non-`Ok` error still aborts.
  - `Data`, `MatchReader.PlayerStats` and `WriteExcel` skip `nil` rounds.
  - `(*Reader).PlayerStats` gets `len(r.Header.Teams) > 1 &&`.
- Flaws:
  1. **Silent drop.** Rounds vanish from the output with no marker. A consumer such as R6Stats cannot tell a 9-round match from a 10-round match that lost a round.
  2. **The likeliest truncation still kills the whole match.** A file cut inside a zstd frame fails `NewReader` with `io.ErrUnexpectedEOF`, which `Ok` rejects, so `Read` still aborts: our cases D and F below exit 1. The fork only helps with EOF-style failures.
  3. **Partial rounds are kept.** When `Reader.Read` stops on a listener error, the round is already stored in `m.rounds[i]` (it is assigned before `r.Read()`), and `roundEnd()` never ran. The fork emits that round, so it can have no winner.
  4. **The CLI and the library disagree.** The fork returns `lastErr` (an `io.EOF`), which the CLI tolerates through `Ok`. `exports/exports.go` checks `err != nil` and still rejects the whole match.
  5. **The `Teams` guard is dead code.** `Header.Teams` is `[2]Team` (`dissect/header.go`: `Teams                  [2]Team   \`json:"teams"\``), so `len(...) > 1` is always true.
  6. **No tests, and the header-only panic (01b) is not addressed.**

We keep the idea (read past bad rounds, guard `nil`) and replace the policy. Credit goes to defiancer6s-hub `c625b8d`.

## Current behaviour (bd6ccbe)

Code at `bd6ccbeb7b46af99638df5d650187069e849f925`:

- `dissect/match.go`, `MatchReader.Read`:
  ```go
  for i := range m.paths {
  	if err := m.read(i); err != nil {
  		return err
  	}
  }
  ```
- `dissect/match.go`, `MatchReader.read`: `NewReader` failing leaves `m.rounds[i]` nil (`if err != nil { return err }` comes before `m.rounds[i] = r`). A `Read` failure keeps a half-read reader, because `m.rounds[i] = r` comes before `return r.Read()`.
- `main.go`, `writeMatch`: `if err := m.Read(); !dissect.Ok(err) { return err }`, then `return m.WriteJSON(out)`.
- `dissect/error.go`: `Ok` is `err == nil || errors.Is(err, io.EOF) || errors.Is(err, zstd.ErrMagicMismatch)`.
- `dissect/match.go`, `Data`: `for _, r := range m.rounds { rounds = append(rounds, round{ Header: r.Header, ...` has no nil check. The same holds for `WriteExcel` (`for i, r := range m.rounds {`) and `dissect/stats.go` `MatchReader.PlayerStats` (`for i, r := range m.rounds {`).
- `dissect/reader.go`, `Reader.Read`: a listener error returns at once (`if err = listener(r); err != nil { return }`), which skips `r.roundEnd()`. This is item 03's mechanism.
- `dissect/reader.go`, `worker`: `for i := start; i <= end; i++ { c := r.b[i]`. When `r.b` is empty, worker 0 runs `i = 0` and indexes past the end. `Read` launches it with `blockStart := r.offset + (i * blockSize)` and `blockSize` is 0.
- `exports/exports.go`, `dissect_read`: `if err := m.Read(); err != nil {` returns an error JSON, so any bad file rejects the whole match.

### Reproduction (run on bd6ccbe, binary built from the scratch clone)

Each case is a temp folder. `good` is `dissect/test/data/replays/valid/Y11S2/custom_early_plant.rec` and `snake` is `.../Y11S2/custom_solid_snake.rec` (5,836,660 bytes; its first zstd frame starts at byte 241,703).

| case | folder | bd6ccbe | after the fix (prototype) |
|---|---|---|---|
| A | `R01=good`, `R02=` 0 bytes | exit 2, `panic: ... nil pointer dereference`, `dissect.(*Reader).PlayerStats(0x0)` | exit 0, rounds `[0]`, `skippedRounds=[{R02.rec,1,"EOF"}]` |
| B | `R01=` 0 bytes, `R02=good` | exit 2, same panic. All rounds are lost because the abort happens at index 0 | exit 0, rounds `[0]`, R01 skipped |
| C | `R01=good`, `R02=snake[:2000]` (header cut) | exit 2, same panic | exit 0, R02 skipped (`EOF`) |
| D | `R01=good`, `R02=snake[:1750000]` (frame cut) | exit 1, `FTL error="unexpected EOF"`, no JSON | exit 0, R02 skipped (`unexpected EOF`) |
| E | `R01=good`, `R02=snake` (control) | exit 0, rounds `[0, 2]` | identical bytes |
| F | `R01=snake[:1750000]`, `R02=good` | exit 1, `unexpected EOF` | exit 0, R01 skipped |
| G | `R01=good`, `R02=snake[:241703]` (header only) | exit 2, `panic: runtime error: index out of range [0] with length 0` in `(*Reader).worker` | exit 0, R02 skipped (`dissect: replay has no data after its header`) |
| H | `R01=` 0 bytes, `R02=` 0 bytes | exit 2, nil-pointer panic | exit 1, `FTL error="dissect: 2 of 2 round files could not be read (first: dissect: round file R01.rec: EOF)"` |
| single | `r6-dissect snake[:241703].rec` (file mode) | exit 2, worker index panic | (01b) exit 1, `ErrNoReplayData` |

Truncation sweep (`NewReader` then `Read`, in-package probe):
- 0 to 3 bytes give `io.EOF` from `Peek` (Ok) and a nil reader.
- Inside the header gives `io.EOF` (Ok).
- Header complete with no full frame gives `NewReader` ok, an empty `r.b`, and the worker panic.
- Inside a frame gives `unexpected EOF` (not Ok). This is the most common shape: every mid-file cut tried on Y11S2 and Y9S1 gave it.
- A cut 100 bytes into the first frame of the Y11S2 file read "successfully" with 0 events and a computed winner. That is a silent partial round, and it is not detected (see Risks).

Corpus check: all 53 real and fixture rounds (11 fixtures, 19 Y11S2 and 23 Y11S3 real rounds) return `err == nil` from `NewReader` and `Read`. None of them hits a listener EOF, and none would be skipped. The five real match folders give byte-identical JSON before and after the prototype.

## Approaches considered

1. **Drop silently (the fork).** This is the smallest change. Consumers cannot see the loss, D and F still fail the whole match, and partial rounds still leak. Rejected.
2. **Fail the whole match with a typed error.** The CLI exits 1 with a clean message naming the file, so R6Stats sees `DissectError` and the upload ends `failed`. It is honest and simple, but one bad last-round file (the typical crash or quit case) costs the organizer every good round of the match, and they have no way to recover them except removing the file by hand and re-zipping. Rejected as the default. It remains available to library callers, because `Read` still returns the typed error.
3. **Skip and flag (chosen).** The library returns `*IncompleteMatchError` and keeps the readable rounds. `Data()` gains `skippedRounds` (omitted when empty). The CLI and the library write the readable rounds and fail only when none could be read. R6Stats ignores unknown keys today (`normalize_match` reads `rounds` and `stats` only), so the change is backward compatible. R6Stats can turn the list into an upload warning.
4. **Keep partial rounds and flag them `"partial": true`.** Rejected for this item. A round whose `Read` errored skipped `roundEnd()`, so its winner, win condition and reconciled kills are not trustworthy. R6Stats rejects a round with no winner: `parser_contract.py` requires "exactly one team must have won the round", which would fail the whole upload. Completing such rounds is item 03's job, not a flag.

## Decision and rationale

Decisions, made autonomously and recorded here:

- **D1. What counts as a skipped round:** any error from `os.Open`, `NewReader` or `Reader.Read` for that file: EOF, unexpected EOF, `ErrInvalidFile`, zstd corruption, a listener error or `ErrNoReplayData`. The failure is per file, and other files are still read. *Alternative:* skip only `Ok` errors (the fork). Rejected because case D, a frame cut, is the realistic truncation.
- **D2. A round whose `Read` returned an error is not emitted, even for `io.EOF`.** Its round end never ran (D4 above). `m.rounds[i]` is set only after `Read` returns nil. Item 03 makes those rounds complete, and they then stop being skipped (see Interactions).
- **D3. Error shape.** `*RoundError{Index, Path, Err}` (unwraps to `Err`). `*IncompleteMatchError{Skipped []*RoundError, Total int}` deliberately does not unwrap, so `Ok(err)` is false. Third-party callers that still use the old `if !Ok(err) { return err }` pattern then get a clean error instead of a nil-round panic. *Alternative:* `Unwrap() []error`. Rejected because `errors.Is(err, io.EOF)` would turn true and `Ok` would pass it.
- **D4. JSON shape.** Top-level `"skippedRounds": [{"file": "<base name>", "index": <0-based position in file-name order>, "error": "<Err.Error()>"}]` with `omitempty`, so complete matches give byte-identical output. Per-round markers are impossible because a skipped file has no readable header.
- **D5. CLI exit policy.** Exit 0 and write JSON (or Excel) when at least one round was read. Log one `log.Error()` line per skipped file to stderr; `Error` is used because the default level is `ErrorLevel` and the line has to show. Exit 1 (`log.Fatal`) when every file failed. *Alternative:* a `--strict` flag. Not added (YAGNI): R6Stats wants the lenient default and library callers get the typed error.
- **D6. `exports` (shared library)** behaves like the CLI: data with `skippedRounds` when at least one round was read, error JSON otherwise.
- **D7. Excel** skips nil rounds. Sheet names keep the file position (`Round %d` uses `i+1`), so a gap shows which file is missing. There is no extra Excel section, because stderr carries the reason.
- **D8. `m.rounds` stays index-aligned with `m.paths`** (nil for skipped files). It is not compacted, because PR #10 indexes `m.Paths()[i]` beside `m.Rounds()`.
- **D9. Failures are cached.** `m.errs[i]` holds the `*RoundError`, so `RoundAt(i)` on a skipped round returns the same error without opening the file again.
- **D10. 01b is a separate guard.** `Reader.Read` returns `ErrNoReplayData` when `r.offset >= len(r.b)`, before it starts any worker. It is placed in `Read` rather than `readChunkedData` because the non-chunked path has the same hole (a header with no body gives `offset == len(r.b)`), and `--dump` should still be able to write a header-only file. It is an error rather than an empty round, because an empty round would have its winner made up by `roundEnd()`.
- **D11. `printHead` (`--info`)** keeps using `FirstRound()`. If round 1 is unreadable it now returns the `*RoundError` cleanly. Falling back to the next readable round is not in scope.

**What R6Stats should see** (consumer change, separate R6Stats issue, not part of this r6-dissect work):
- The match JSON carries `skippedRounds` and the process exits 0.
- `parser_contract.normalize_match` should read `skippedRounds` into `MatchData.skipped_rounds: tuple[SkippedRoundData, ...]` (`file: str`, `index: int`, `error: str`), optional and empty when absent.
- `run_pipeline` (`api/r6stats/worker/jobs.py`) should add one upload warning per skipped file through `_add_warnings`, for example `"round file R07.rec could not be read (unexpected EOF); the match is missing that round"`.
- Dedup by `(replay_match_id, round_number, overtime_round_number)` (`services/ingest.py`) already lets the other captain's upload fill the missing round.
- Until R6Stats reads the key, the skipped rounds are just absent and the upload succeeds with the readable rounds. That is already better than today's `failed` with a stack trace.

## Interactions

- **Item 03 (a listener EOF aborts the rest of the round). Ownership, in writing:**
  - **01 owns** the match-level policy: any non-nil error from `NewReader` or `Reader.Read` for a file means that round is excluded and listed in `skippedRounds`. 01 also owns nil-guarding every `m.rounds` loop, the CLI and exports exit policy, and the `Reader.Read` empty-body guard (01b).
  - **03 owns** `Reader.Read`'s listener loop. When a listener returns an `Ok` error near the end of the buffer, 03 should stop calling listeners, still run the end-of-round block (`stopDefuserRun`, `roundEnd`, `stampNew`, `backfillLeadingUpdates`, `applyLoadouts`, plus PR #3's `resolveHealthEvents`/`relabelPostPlantWithoutPlant` and PR #10's timeline build), and **return nil**.
  - **Contract:** after 03, `Read` returns non-nil only when the round's derived data cannot be trusted. If 03 wants to mark a round as cut short, it must do so with a field on `Reader` or `Header` that `Data()` serializes, never through the returned error, because 01 drops every round whose `Read` errored.
  - **Order:** either order works. 01 first means listener-EOF rounds are skipped and flagged until 03 lands; none occur in our 53-round corpus. 03 first means those rounds complete, while bad files still crash until 01 lands.
  - **Textual conflict:** small. 01b adds four lines at the top of `Read` and 03 edits the listener loop.
- **Item 05 (bytes.Index rewrite of the worker):** may remove the index panic by itself. 01b is still needed: an empty body should be an error, not an empty round with a made-up winner. Whichever lands second rebases the four-line guard.
- **Item 07 (an abandoned round awards team 1):** related symptom, separate mechanism (`roundEnd` winner logic), not touched here.
- **PR #10 `origin/track/rec-timeline`:**
  - It adds `m.Rounds()` (doc: "entries are nil until their round is read") and, in `main.go`, `for i, r := range m.Rounds() { if err := writeRoundSidecar(timelineDir, m.Paths()[i], r)`, which is another unguarded loop.
  - It changes `writeMatch` to `writeMatch(in, format, out, timelineDir string)` and adds `TimelineError` to `Data()`'s `round` struct.
  - Whichever lands second must: add `if r == nil { continue }` to the sidecar loop, update the `Rounds()` doc to "nil until read, and for skipped round files", pass `""` as `timelineDir` in `main_test.go`, and merge the `round`/`output` struct fields in `Data()`.
- **PR #3 `origin/fork/health-dbno`:** touches `MatchReader.Listen` (`break` changed to `return`), the end-of-round block of `Reader.Read`, and `Reader` stats. There is no overlap with the changed lines, so it should rebase cleanly.
- **PR #12 (ElyasYassin):** does not touch these files.
- **Golden tests:** no churn. `TestReader_Read` goldens are per-round `Reader` JSON. `skippedRounds` is match-level and `omitempty`. The prototype passed `go test ./...` with unchanged goldens.
- **JSON consumers:** the new key appears only for incomplete folders. Upstream-format readers ignore unknown keys.

---

# Part 2: Implementation plan

Work in a branch off `main` (bd6ccbe or later) of `prestonwillis10/r6-dissect`. `export PATH=$HOME/.local/go/bin:$PATH` first. Commit messages use `fix(...)`, never `docs(...)`, for code commits.

## Global Constraints

- Module path stays `github.com/redraskal/r6-dissect`, with Go 1.26.
- No new dependencies.
- CI runs `gofmt -l .` (it must be empty), `go vet ./...`, `go test -race -count=1 ./...`, staticcheck `v0.8.2`, `go build -buildmode=c-shared -o <tmp> exports/exports.go`, and `go mod tidy -diff`. All of them must pass.
- JSON for complete match folders must be byte-identical to bd6ccbe.
- Error strings are fixed:
  - `ErrNoReplayData` is `"dissect: replay has no data after its header"`.
  - `RoundError.Error()` is `"dissect: round file <base>: <err>"`.
  - `IncompleteMatchError.Error()` is `"dissect: <n> of <total> round files could not be read (first: <Skipped[0]>)"`.
- Test fixtures are built in `t.TempDir()` from existing fixture files. Do not commit any new `.rec` files.

## Review Focus

1. **Every file unreadable:** `Read` returns `*IncompleteMatchError` with `len(Skipped) == Total`. The CLI exits 1 and writes nothing to stdout. Tested by `TestMatchReader_Read_NoReadableRound` and `TestWriteMatch_FailsWhenNoRoundIsReadable`.
2. **Old caller pattern `if !dissect.Ok(err)`:** it must treat an incomplete match as an error. Asserted in `TestMatchReader_Read_SkipsUnreadableRounds` (`dissect.Ok(err)` must be false).
3. **Skipped file in the middle:** the order of the remaining rounds is kept and nothing shifts. Covered by the "empty middle file" case, whose expected round numbers are `[0, 2]`.
4. **`RoundAt` on a skipped index:** returns `(nil, *RoundError)` on repeated calls without panicking. Tested by `TestMatchReader_RoundAt_SkippedRound`.
5. **`--info` on a folder whose first file is empty:** `printHead` returns an error and does not panic. Tested by `TestPrintHead_EmptyFirstRoundFile`.

## File structure

- `dissect/error.go`: add `RoundError`, `IncompleteMatchError` and `ErrNoReplayData`.
- `dissect/match.go`:
  - add `errs []*RoundError`;
  - split `read` into `read` and `readRound`;
  - rewrite `Read`;
  - add `SkippedRounds()` and the `SkippedRound` JSON type;
  - add nil guards in `Data` and `WriteExcel`.
- `dissect/stats.go`: add a nil guard in `MatchReader.PlayerStats`.
- `dissect/reader.go`: add the empty-body guard at the top of `Read` (01b).
- `main.go`: `writeMatch` exit policy.
- `exports/exports.go`: the same policy.
- New tests:
  - `dissect/test/match_test.go` (01a);
  - `dissect/test/nodata_test.go` (01b);
  - `main_test.go` (package `main`, CLI policy).

### Task 1: MatchReader reads past unreadable round files and reports them

**Files:**
- Modify: `dissect/error.go`, `dissect/match.go` (`MatchReader` struct, `NewMatchReader`, `read`, `Read`, `WriteExcel`, `Data`), `dissect/stats.go` (`MatchReader.PlayerStats`)
- Test: `dissect/test/match_test.go` (new)

**Interfaces:**
- Produces:
  - `type RoundError struct { Index int; Path string; Err error }` with `Error() string` and `Unwrap() error`.
  - `type IncompleteMatchError struct { Skipped []*RoundError; Total int }` with `Error() string` and no `Unwrap`.
  - `func (m *MatchReader) Read() error` returns `nil` or `*IncompleteMatchError`. Only `*RoundError` failures are collected; any other error is returned as it is, though there is none today.
  - `func (m *MatchReader) SkippedRounds() []*RoundError` returns a non-nil slice in file order.
  - `type SkippedRound struct { File string \`json:"file"\`; Index int \`json:"index"\`; Error string \`json:"error"\` }`.
  - `Data()`'s `output` gains `SkippedRounds []SkippedRound \`json:"skippedRounds,omitempty"\``.

- [ ] **Step 1: Write the failing tests** in `dissect/test/match_test.go` (package `test`).
  - Helpers:
    - `readFixture(t, path) []byte`;
    - `matchFolder(t, files map[string][]byte) *os.File`, which writes the files into `t.TempDir()`, opens the directory, and closes it with `t.Cleanup`.
  - Constants:
    - `earlyPlant = "data/replays/valid/Y11S2/custom_early_plant.rec"` (header `roundNumber` 0);
    - `solidSnake = "data/replays/valid/Y11S2/custom_solid_snake.rec"` (`roundNumber` 2).
  - Decode `WriteJSON` output into `struct{ Rounds []struct{ RoundNumber int \`json:"roundNumber"\` }; SkippedRounds []struct{ File string; Index int; Error string } }`, with JSON tags `rounds`, `roundNumber`, `skippedRounds`, `file`, `index` and `error`.
  - `TestMatchReader_Read_SkipsUnreadableRounds`, a table test. Each row is (name, files, wantRounds, wantSkipped):
    - `"empty last file"`: `{R01: good, R02: {}}`, `[0]`, `[R02.rec]`
    - `"empty first file"`: `{R01: {}, R02: good}`, `[0]`, `[R01.rec]`
    - `"empty middle file"`: `{R01: good, R02: {}, R03: snake}`, `[0, 2]`, `[R02.rec]`
    - `"header cut short"`: `{R01: good, R02: snake[:2000]}`, `[0]`, `[R02.rec]`
    - `"data cut short"`: `{R01: good, R02: snake[:len(snake)/2]}`, `[0]`, `[R02.rec]`
  - Assertions for every row:
    - `errors.As(err, &incomplete)` is true;
    - `dissect.Ok(err)` is false;
    - `incomplete.Total == len(files)`;
    - the round numbers equal `wantRounds`;
    - the `skippedRounds` file names equal `wantSkipped`, and each `error` is non-empty;
    - `m.WriteExcel(io.Discard)` returns nil.
  - `TestMatchReader_Read_NoReadableRound`: `{R01: {}, R02: snake[:2000]}` gives `len(Skipped) == 2`, `Total == 2`, and `Skipped[0]` is a `*RoundError` with `filepath.Base(Path) == "R01.rec"` and `errors.Is(Skipped[0], io.EOF)`.
  - `TestMatchReader_Read_CompleteMatchHasNoSkippedRounds`: `{R01: good, R02: snake}` gives `Read() == nil`, and the `WriteJSON` output does not contain `"skippedRounds"`.
  - `TestMatchReader_RoundAt_SkippedRound`: `{R01: good, R02: {}}`. After `Read()`, calling `m.RoundAt(1)` twice returns `r == nil` and an error that `errors.As` matches as `*RoundError` with `Index == 1` both times. `m.RoundAt(0)` returns a non-nil reader.

- [ ] **Step 2: Run them and see them fail**

  Run: `go test ./dissect/test -run 'TestMatchReader' -count=1`
  Expected: build failure `undefined: dissect.IncompleteMatchError` / `dissect.RoundError`. Before the types exist, the same folders panic in `(*Reader).PlayerStats(0x0)`; that is the bug.

- [ ] **Step 3: Implement.**
  - **`error.go`:** add the two types, using the doc comments from the Decision section (D3). The `IncompleteMatchError.Error()` text is fixed in Global Constraints. If `len(Skipped) == 0`, return `"dissect: incomplete match"` instead of indexing.
  - **`match.go`:**
    - Add the field `errs []*RoundError` and allocate it in `NewMatchReader` next to `rounds`.
    - `read(i)`:
      - return `ErrInvalidFile` for an index out of range (unchanged);
      - return nil if `m.rounds[i] != nil`;
      - return `m.errs[i]` if it is set;
      - otherwise call `m.readRound(m.paths[i])`. On error, store `&RoundError{Index: i, Path: m.paths[i], Err: err}` in `m.errs[i]` and return it. On success, store the reader in `m.rounds[i]`.
    - New `readRound(path string) (*Reader, error)`. It opens the file, calls `NewReader`, and attaches the match listeners using a separate loop variable `q`. The current code reuses `i`, which is harmless but confusing. It then calls `r.Read()` and returns `nil, err` on any non-nil error (D2).
    - `Read()` collects `*RoundError` values (`errors.As`) and returns any other error at once. It returns `&IncompleteMatchError{Skipped, Total: len(m.paths)}` when something was skipped.
    - `SkippedRounds()` walks `m.errs`.
    - `Data()`:
      - skip `nil` rounds;
      - build `[]SkippedRound` from `SkippedRounds()` with `File: path.Base(e.Path)` (`path` is already imported), `Index: e.Index` and `Error: e.Err.Error()`;
      - leave the slice nil when there is nothing to report, so `omitempty` drops it.
    - `WriteExcel`: `if r == nil { continue }` at the top of the round loop.
  - **`stats.go`:** `if r == nil { continue }` at the top of the `MatchReader.PlayerStats` round loop.

- [ ] **Step 4: Run the tests and see them pass**

  Run: `go test ./dissect/test -run 'TestMatchReader' -count=1 -v`
  Expected: all four tests and every subtest PASS. Then `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**
  ```bash
  git add dissect/error.go dissect/match.go dissect/stats.go dissect/test/match_test.go
  git commit -m "fix(match): read past unreadable round files and report them as skippedRounds"
  ```

### Task 2: CLI and shared library write the readable rounds

**Files:**
- Modify: `main.go` (`writeMatch`), `exports/exports.go` (`dissect_read`, folder branch)
- Test: `main_test.go` (new, package `main`)

**Interfaces:**
- Consumes `*dissect.IncompleteMatchError` from Task 1.
- `writeMatch(in *os.File, format OutputFormat, out io.Writer) error` keeps its signature. It returns nil when at least one round was read, and the `*IncompleteMatchError` when none was.

- [ ] **Step 1: Write the failing tests** in `main_test.go`.
  - `TestMain` sets `zerolog.SetGlobalLevel(zerolog.Disabled)` and calls `os.Exit(m.Run())`.
  - Helper `openMatchFolder(t, files)` works like Task 1's `matchFolder`.
  - Fixture constant `"dissect/test/data/replays/valid/Y11S2/custom_early_plant.rec"`.
  - `TestWriteMatch_SkipsEmptyRoundFile`: `{R01: good, R02: {}}`. `writeMatch(..., JSON, &buf)` returns nil, and the decoded JSON has 1 round and `skippedRounds[0].file == "R02.rec"`.
  - `TestWriteMatch_FailsWhenNoRoundIsReadable`: `{R01: {}, R02: {}}`. `writeMatch` returns non-nil and `buf.Len() == 0`.
  - `TestPrintHead_EmptyFirstRoundFile`: `{R01: {}, R02: good}`. `printHead(folder)` returns a non-nil error, which `errors.As` matches as `*dissect.RoundError`, and does not panic.

- [ ] **Step 2: Run them and see them fail**

  Run: `go test . -count=1 -run 'TestWriteMatch|TestPrintHead'`
  Expected: `TestWriteMatch_SkipsEmptyRoundFile` FAILS with `writeMatch(): dissect: 1 of 2 round files could not be read (first: dissect: round file R02.rec: EOF)`, because the CLI still rejects any non-`Ok` error. The other two pass already once Task 1 is in. They are regression pins.

- [ ] **Step 3: Implement.**
  - **`main.go` `writeMatch`:** replace `if err := m.Read(); !dissect.Ok(err) { return err }` with the following. If `m.Read()` returns non-nil and it is not `*dissect.IncompleteMatchError`, or `len(Skipped) == Total`, return it. Otherwise log `log.Error().Err(s.Err).Str("file", filepath.Base(s.Path)).Msg("round skipped: the file could not be read")` for each skipped file and continue to Excel or JSON. Add the `errors` and `path/filepath` imports.
  - **`exports.go`:** use the same condition in the folder branch, returning `convertErrorForExport(err)` only for the "not incomplete, or nothing readable" case. Add the `errors` import.

- [ ] **Step 4: Run the tests and see them pass**

  Run: `go test . -count=1 -v -run 'TestWriteMatch|TestPrintHead'`, then `go build -buildmode=c-shared -o "$(mktemp -d)/lib" exports/exports.go`
  Expected: 3 PASS, and the c-shared build succeeds.

- [ ] **Step 5: Commit**
  ```bash
  git add main.go main_test.go exports/exports.go
  git commit -m "fix(cli): write the readable rounds of an incomplete match folder"
  ```

### Task 3: Reader.Read returns ErrNoReplayData for a replay with no data (issue 01b)

**Files:**
- Modify: `dissect/error.go` (add `ErrNoReplayData`), `dissect/reader.go` (`Read`, first lines)
- Test: `dissect/test/nodata_test.go` (new); add one row to `TestMatchReader_Read_SkipsUnreadableRounds` if Task 1 has landed

**Interfaces:**
- Produces `var ErrNoReplayData = errors.New("dissect: replay has no data after its header")`. `(*Reader).Read()` returns it, before starting any worker, when `r.offset >= len(r.b)`.

- [ ] **Step 1: Write the failing test.** `TestReader_Read_HeaderOnlyReplay`:
  - read `solidSnake` (Task 1's `readFixture`, or `os.ReadFile` if this task lands alone; in that case define the constant locally);
  - set `end := bytes.Index(b, []byte{0x28, 0xB5, 0x2F, 0xFD})`, and fail the test if `end <= 0`;
  - `dissect.NewReader(bytes.NewReader(b[:end]))` returns a nil error;
  - `r.Read()` satisfies `errors.Is(err, dissect.ErrNoReplayData)`.

  If Task 1 is in, also add the row `"no data after the header"` with files `{R01: good, R02: snake[:bytes.Index(snake, zstdMagic)]}`, wantRounds `[0]` and wantSkipped `[R02.rec]`.

- [ ] **Step 2: Run it and see it fail**

  Run: `go test ./dissect/test -run TestReader_Read_HeaderOnlyReplay -count=1`
  Expected: once the `ErrNoReplayData` var exists, the test binary crashes with `panic: runtime error: index out of range [0] with length 0` in `(*Reader).worker` (`dissect/reader.go`). Before that, a build error.

- [ ] **Step 3: Implement.** At the top of `Reader.Read`, before `numWorkers := 5`, add `if r.offset >= len(r.b) { return ErrNoReplayData }` with the comment `// nothing to scan: the workers would index past the end of r.b`.

- [ ] **Step 4: Run it and see it pass**

  Run: `go test ./dissect/test -run 'TestReader_Read_HeaderOnlyReplay|TestMatchReader' -count=1`
  Expected: PASS.

- [ ] **Step 5: Commit**
  ```bash
  git add dissect/error.go dissect/reader.go dissect/test/nodata_test.go dissect/test/match_test.go
  git commit -m "fix(reader): return ErrNoReplayData instead of panicking on a replay with no data"
  ```

### Task 4: Full verification and corpus parity

**Files:** none changed. If PR #10 is already on main, do the Interactions edits for PR #10 here: the nil guard in the sidecar loop, the `Rounds()` doc line, and the `""` argument in `main_test.go`.

- [ ] **Step 1:** `gofmt -l .` prints nothing; `go vet ./...` is clean; `go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...` is clean; `go mod tidy -diff` is empty.
- [ ] **Step 2:** `go test -race -count=1 ./...` gives three `ok` lines (root, `dissect`, `dissect/test`).
  - On a heavily loaded host, the whole `dissect/test` package can exceed the 10-minute default under `-race`. The prototype run did, at load average around 500, while the same suite without `-race` passed in 22 s and the new tests alone passed under `-race` in 47 s. If that happens, rerun with `-timeout 30m`. It is host load, not a code problem.
  - CI on GitHub-hosted runners is the gate.
- [ ] **Step 3:** `go test ./dissect/test -run TestReader_Read -count=1` passes without `-update`. `git status` shows no `.rec.json` changes (no golden churn).
- [ ] **Step 4: Corpus parity.**
  - Build the old binary from `main` and the new one from the branch.
  - For each real match folder (`.../a0d4e668-.../scratchpad/replays/Match-2026-08-25_*` and `.../replays-new/Match-2026-10-18_*`), run both and `cmp` the JSON.
  - Expected: all 5 identical, with exit 0/0.
- [ ] **Step 5: CLI acceptance.**
  - Build the folders A, D, G and H from the reproduction table under `mktemp -d` and run the new binary on each.
  - Expected: A, D and G exit 0 with `skippedRounds` listing R02; H exits 1 with `FTL error="dissect: 2 of 2 round files could not be read ..."`.

## Acceptance

- **01a:** `r6-dissect <folder with good R01 and empty R02>` exits 0 and prints JSON with 1 round and `"skippedRounds":[{"file":"R02.rec","index":1,"error":"EOF"}]`. At bd6ccbe it exits 2 with `panic: ... (*Reader).PlayerStats(0x0)`. `go test ./dissect/test -run TestMatchReader` and `go test . -run TestWriteMatch` pass.
- **01b:** `r6-dissect <solid_snake cut at byte 241703>.rec` exits 1 with `dissect: replay has no data after its header`. At bd6ccbe it exits 2 with `index out of range [0] with length 0`. `TestReader_Read_HeaderOnlyReplay` passes.
- Complete match folders give byte-identical JSON. There is no golden churn and CI is green.

## Risks / open questions

- **Silent partial rounds are still possible.** A file cut exactly at a zstd frame boundary decodes "successfully" into a short round. The probe found one: 100 bytes into the first frame gave 0 events and a winner computed from the header scores. No signal in the current code distinguishes it. A follow-up could flag rounds whose clock never left prep, or that have no round-end packet, once item 03 or the PR #10 timeline gives such a signal. Not filed here; it is a guess-level mechanism with no real-world sample.
- **Salvaging frame-cut data.** Case D decodes a large share of the round before `unexpected EOF`. Keeping that partial data is possible, but it would emit untrustworthy rounds (D2). Out of scope.
- **Item 03 contract.** If item 03 decides to return a non-nil error for "completed but cut short" rounds, they will be skipped by this policy. The contract in Interactions asks 03 to use a field instead. Whoever reviews the second PR must check this.
- **R6Stats consumer.** Until R6Stats reads `skippedRounds`, the loss of a round is visible only in the CLI's stderr, which R6Stats discards on exit 0. The R6Stats follow-up (Decision section) should be filed alongside this issue.
- **`log.Error` on stderr for a non-fatal condition.** Chosen so the line shows at the default level. If anyone scrapes stderr, the new line only appears for incomplete folders.
