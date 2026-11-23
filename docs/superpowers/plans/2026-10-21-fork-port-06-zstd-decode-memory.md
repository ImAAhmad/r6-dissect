# Fork port 06: round-buffer pinning and zstd decode memory, implementation plan

> **Issue:** #32 · prestonwillis10/r6-dissect · fork survey: `docs/research/2026-10-21-fork-survey.md`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task by task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A match read keeps no round's decompressed buffer alive after that round is read, and decompressing a round allocates about its output size. Peak RSS on a real 10-round match drops from about 1.5 GB to about 0.3 GB, and the JSON stays byte-identical.

**Architecture:** There are two independent fixes. (1) `readPlayer` stores a copy of the 4-byte `DissectID`, not a slice into `r.b`. (2) `readChunkedData` uses a synchronous zstd decoder that it closes, and decodes every section onto one buffer, which is pre-sized from the frame headers' declared content sizes. `readNonChunkedData` also closes its decoder. GOGC is not changed in either the CLI or the library.

**Tech Stack:** Go 1.26 (`go.mod`), `github.com/klauspost/compress v1.17.11` (`zstd`), the standard `testing` and `runtime` packages.

**Spec:** this document (the design sections come before the Implementation plan).

Verified-against: `bd6ccbeb7b46af99638df5d650187069e849f925` (our `origin/main`).

---

## Summary

The survey made three claims. At bd6ccbe, two are true and one is wrong.

- **TRUE, and the main problem: `DissectID` pins the round buffer.** `readPlayer` stores `id`, a 4-byte slice of `r.b`. The scoreboard rows share it. `MatchReader` keeps every round's `*Reader`, so every round's whole decompressed buffer (100 to 160 MB each) stays reachable until the match is done. Measured on a real 10-round Y11S2 match: **796 MiB of live heap after `Read`+GC, against 1.6 MiB with the fix**. The CLI's peak RSS falls from **1492 to 291 MiB**.
- **TRUE: decompression allocates far more than it outputs.** On a 27.6 MB Y11S2 round, `NewReader` allocates 239 MB. The causes are the 4-way concurrent decoder, `io.ReadAll` per section, and append growth. With the fix it allocates 41 MB. On the real 10-round match, total allocation falls from 5664 to 967 MiB.
- **WRONG: "the missing `Close()` leaks decoder goroutines."** With klauspost v1.17.11, the stream decoder goroutines exit when the stream ends. `io.ReadAll` always reads to the end or to an error, and the `Decoder` is collected with its `Reader`. I measured goroutine counts before and after 10 reads (chunked, non-chunked, truncated, corrupted): 3 → 3 in every case. I measured 2 → 2 across 3 full match reads, and the thread count stayed flat across 4 `dissect_read` calls through the shared library. We still add `Close()`, because it is the documented contract, it is free, and it guards against future early returns. It is not the fix.

GOGC: **no change in either the CLI or the library** (see Decision D4).

Verdict: FILE, label `bug` (an unbounded-by-design memory retention in the library API, with a measured 5× peak).

## Source

- Fork: `Animuxs/main` (r6-stats-app), commit `2ad47cdb0a525ec60795861f05693c9635a67d16` ("Speed up replay parsing, cut its memory, and add TXT export", 2026-09-25).
- Files: `dissect/player.go` (one line), `dissect/reader.go` (zstd hunk, plus the `findQueries` pattern search, which is **item 05's**, not ours), `main.go` (GOGC).
- What they did:
  1. `DissectID: bytes.Clone(id)` in `readPlayer`.
  2. `zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))` + `defer zstdReader.Close()` in `readChunkedData`. Each section is decoded with `zstdReader.WriteTo(&data)` into one `bytes.Buffer`, which replaces `io.ReadAll` and a byte-at-a-time append loop.
  3. In `main()`: `if os.Getenv("GOGC") == "" { debug.SetGCPercent(50) }`.
  4. They claim 1.38 GB → 0.35 GB peak on 9 rounds, and 840 → 230 MB allocated per round.
- Flaws, and differences from our base:
  - Their base appended `decompressed` **one byte at a time**. Ours already does `data = append(data, decompressed...)`, so part of their win does not apply to us.
  - `bytes.Buffer` doubles its capacity. In our tree it **raised** peak RSS compared with the `DissectID` fix alone (about 560 against 430 MiB on the same match, 3 runs each), because the last growth leaves an up-to-2× buffer alive. We use an append-based writer plus an exact size hint instead.
  - They did not close the decoder in `readNonChunkedData` (pre-Y8S4).
  - Their GOGC change is process-wide and hard-coded. Measured on our tree after the two fixes, GOGC=50 changes peak RSS by between +12% and −30% depending on the match. That is within the noise of this host, which had a load average of 100 to 600 during measurement.
  - Their goroutine-leak rationale does not hold for this library version (see Summary).

## Current behaviour (bd6ccbe)

Code:

- `dissect/player.go`, `readPlayer`: `id, err := r.Bytes(4)` … `DissectID: id,`. Here `Reader.Bytes` returns `r.b[r.offset-n : r.offset]`, a window into the round buffer.
- `dissect/header.go`, `deriveTeamRoles`: `r.Scoreboard.Players = append(r.Scoreboard.Players, ScoreboardPlayer{ ID: p.DissectID, })`. This is the same backing array.
- `dissect/match.go`, `MatchReader`: `rounds []*Reader`. `read(i)` stores `m.rounds[i] = r` and keeps it for the whole match.
- `dissect/reader.go`, `Read`: `r.b = nil` at the end. It does nothing while a `DissectID` still points into the array.
- `dissect/reader.go`, `readChunkedData`: `zstdReader, _ := zstd.NewReader(nil)` (the default concurrency is `min(4, GOMAXPROCS)`, so a stream goroutine starts per `Reset`), `decompressed, err := io.ReadAll(zstdReader)`, `data = append(data, decompressed...)`, and no `Close`.
- `dissect/reader.go`, `readNonChunkedData`: `zstdReader, err := zstd.NewReader(genericReader)` … `io.ReadAll(zstdReader)`, and no `Close`.
- `exports/exports.go`, `dissect_read`: builds a `MatchReader` or `Reader` per call and returns only a C string, so after the call everything is unreachable. Within one call it has the same peak as the CLI. Across calls, Go keeps the freed pages, so the host process sits at the high-water mark.

Every place that slices into `r.b` was checked for whether it outlives the round:

| Site | Kept after the round? | Status |
|---|---|---|
| `player.go` `readPlayer` `DissectID: id` | yes (`Header.Players`, `Scoreboard.Players`) | **pins**, fixed by Task 1 |
| `header.go` `deriveTeamRoles` `ScoreboardPlayer{ID: p.DissectID}` | yes | fixed by the same clone |
| `defuse.go` `peekPlayerSlotID` | yes (`slotID`) | already copies: `append([]byte(nil), r.b[k-8:k-4]...)` |
| `loadout.go` `dissectID: append([]byte(nil), p.DissectID...)` | yes | already copies |
| `defuse.go` / `scoreboard.go` `[4]byte` keys (`defuserPawnToSlot`, `pendingSBIDs`, `scoreboardEntryPlayerID`) | yes | arrays, so copies |
| `player.go` `readAtkOpSwap` `id`, `scoreboard.go` three `id, err := r.Bytes(4)`, `defuse.go` `readDefuserTimer` `id`, `feedback.go` `killTrace`, `site.go` `pattern`, `site.go` `id := r.b[o : o+8]`, `player.go` `opSwapPlayerObject` `b` | no (local to the listener) | fine |
| `String()`, `readHeaderString`, `feedback.go` `msg := string(b)` | `string(...)` copies | fine |
| `fork/health-dbno` `health.go` | `copy(health[:], …)`, `copy(slot[:], …)`, `copy(owner[:], …)` | fine |
| `track/rec-timeline` `buildTimeline` | `wire.Layout`/`Envelope.Payload`/`Token.Value` are slices of `r.b`, but local to `buildTimeline`. `wire.Graph` stores only `Handle`/`ClassID` arrays. `Timeline` has no `[]byte` | fine |
| `track/rec-timeline` `Reader.Data()` | returns `r.b` to the caller, documented as valid only before `Read` | caller-owned, fine |

Reproductions I ran (scratch clone at bd6ccbe, `GOMAXPROCS=28`):

1. CLI peak RSS (`/usr/bin/time -v`, `Maximum resident set size`), on the user's corpus:

   | Match folder | rounds | base | + DissectID clone | + zstd fix (final) | final, GOGC=50 |
   |---|---|---|---|---|---|
   | `Match-2026-08-25_19-56-00-32000` (Y11S2) | 10 | 1492 MiB | 401 | 291 / 235 | 263 |
   | `Match-2026-08-25_20-53-53-41172` (Y11S2) | 9 | 795 | 279 | 154 / 153 | 150 |
   | `Match-2026-10-18_19-08-03-14012` (Y11S3) | 12 | 1568 | 467 | 255 / 254 | 228 |
   | `Match-2026-10-18_18-19-54-14012` (Y11S3) | 7 | 1177 | 364 | 275 / 270 | 221 |
   | `Match-2026-10-18_21-07-35-14012` (Y11S3) | 4 | 600 | 470 | 286 / 285 | 199 |

   The final column shows two runs. Wall times are not reported: the host's load average was 100 to 600, and wall times varied 2× between identical runs.

2. Live heap and goroutines after `MatchReader.Read` + `runtime.GC()`, on the 10-round Y11S2 match, 3 passes:
   - base: `goroutines=2 (start 2) liveHeap=796.1MiB totalAlloc=5664MiB` per pass
   - final: `goroutines=2 (start 2) liveHeap=1.6MiB totalAlloc=967MiB` per pass
3. `TestMatchReader_DoesNotPinRoundBuffers` (Task 1) on the `Y8S2` fixture folder (5 rounds) at base: `live heap 530 MiB`, with every `DissectID cap` around 1e8. After Task 1: `1.4 MiB`.
4. `BenchmarkNewReader` (Task 2), `-benchtime 5x`:
   - `Y11S2/custom_early_plant.rec`: base `239037659 B/op 3079 allocs/op`. sync+Close+`bytes.Buffer`: `122698108 B/op`. sync+Close+append writer: `164389017 B/op`. **final (+ size hint): `41328411 B/op 550 allocs/op`**.
   - `Y8S1/ranked_1.rec` (non-chunked): `397 MB/op` before and after (only `Close` was added; see D5).
5. Goroutine leak: 10 reads each of chunked, non-chunked, half-truncated and 4 KiB-corrupted inputs: `before=3 after=3` in every case, at base.
6. Shared library (`go build -buildmode=c-shared ./exports`, called through Python `ctypes`, 4 × `dissect_read` on the 10-round match): base `VmRSS 1493 → 1510 → 1530 → 1553 MiB`, final `192 → 200 → 197 → 200 MiB`. Threads stayed flat (15 and 14).
7. Frame headers: on all 53 replays I tried (the 42 corpus rounds and 11 fixtures), the sum of the `FrameContentSize` values in the zstd frame headers **equals** the decompressed length exactly. The worst decompressed/compressed ratio was 13.04.

## Approaches considered

**A. Port Animuxs as-is** (clone, sync decoder + Close + `bytes.Buffer`, GOGC=50 in `main`).
+ It is a known quantity, a 10-line diff.
− `bytes.Buffer` doubling raised peak RSS on our tree compared with the clone alone. The GOGC change is a process-wide policy hidden in the binary. The non-chunked path is missed.

**B. The clone only.**
+ It is a one-line diff and gets about 75% of the RSS win (1492 → 401 MiB).
− Each round still allocates about 8× its output. The library's high-water mark stays near 2× the round size.

**C. The clone + sync decoder + Close on both paths + an append writer pre-sized from the frame headers (chosen).**
+ It gets the best measured peak (about 290 MiB) and allocates about 1.5× the output per round. The hint is advisory, so output cannot change, and the change touches only the two decode functions plus a new file.
− It needs a pre-scan of the compressed bytes for zstd magic (one `bytes.Index` pass over a buffer of about 10 MB, negligible) and a cap on the hint.

**D. Stream-decode without keeping the whole compressed file** (`r.b = temp` holds about 10 MB of compressed data during decode).
− The compressed bytes are 5 to 13 times smaller than the output, so the win is small, and it means rewriting header parsing to run on a stream. Rejected.

**E. Release the `Reader`s from `MatchReader` after reading.**
− `MatchReader` exposes `RoundAt`, `Rounds()` (on rec-timeline), `WriteExcel` and `Data()` over the kept readers. This is an API change for no gain once the pin is gone. Rejected.

## Decisions (autonomous; alternatives recorded)

- **D1. Clone at the source.** `DissectID: bytes.Clone(id)` in `readPlayer`. `ScoreboardPlayer.ID` and loadouts inherit it. *Alternative:* clone in `deriveTeamRoles` and at the end of `Read`. Rejected, because the clone in `readPlayer` covers every later consumer, including items 08 and 09, which add more `ScoreboardPlayer{ID: p.DissectID}` sites.
- **D2. A synchronous decoder for chunked sections.** `zstd.WithDecoderConcurrency(1)`. The sections are small (64 sections for a 27.6 MB round, about 430 KB each). The 4-way decoder spends memory on per-goroutine block decoders and history windows. The benchmark time fell from 243 ms to 96 ms per round on the same run. *Alternative:* keep the default concurrency and just `Close`. Rejected: it keeps most of the allocation.
- **D3. An append writer with an exact capacity hint, not `bytes.Buffer`.** `decompressedSizeHint` sums `zstd.Header.FrameContentSize` over every zstd magic found in the compressed bytes after the header, and caps the sum at `32 * len(compressed)`. The hint is only a capacity. If it is wrong (a false-positive magic in the compressed data, or a frame without `FCS`), the append grows as usual and the output is unchanged. *Alternatives:* `bytes.Buffer` (measured worse peak), or a per-section `slices.Grow` (still amortized 1.25× growth, so no exact size). The cap of 32× is 2.5 times the worst observed ratio (13.04), so a corrupt header cannot make us allocate gigabytes up front.
- **D4. GOGC: unchanged in both the CLI and the library.** Library (`exports/`): never. It runs inside a host process, and the GC target is the host's operational choice. CLI: after D1 to D3, the live heap between rounds is about 1.6 MiB, so the default pacer already collects each round's buffer promptly. GOGC=50 gave between +12% and −30%, within this host's noise. The R6Stats worker (`api/r6stats/services/dissect.py` `run_dissect`, `subprocess.run([str(bin), str(folder)], …)`) inherits its environment, so an operator who needs a lower ceiling sets `GOMEMLIMIT` or `GOGC` in the worker's compose `environment:`. That needs no parser release. *Alternative:* Animuxs's "default to 50 when GOGC is unset". Rejected as an unmeasurable win that hides a policy in the binary. Revisit if R6Stats gets a container memory limit.
- **D5. Non-chunked (pre-Y8S4): add `defer zstdReader.Close()` only.** The same pre-size trick would need the compressed stream buffered first. These seasons (Y8S1 to Y8S3) are legacy for R6Stats (collegiate, current season), and the 397 MB/op on `ranked_1.rec` is a one-round peak, not a retention. Out of scope.
- **D6. Put the new helpers in a new file, `dissect/decompress.go`.** Item 05 rewrites `worker`/`Read` in `dissect/reader.go` and its import block. A separate file keeps our edit in `reader.go` to the bodies of `readChunkedData`/`readNonChunkedData`, with no import changes (both `bytes` and `zstd` stay used in `reader.go`).
- **D7. Keep a goroutine-count test even though it is green today.** It records that the survey's leak claim was checked, and it catches a regression if we later stop draining the decoder (for example, an early return before EOF on a concurrent decoder).

## Interactions

- **Item 05 (pattern search, the same fork commit):** that plan explicitly leaves `readChunkedData`, `readNonChunkedData` and `NewReader` alone. It changes only `worker`/`Read` and the `reader.go` imports. With D6 we change no imports, so the two merge cleanly in either order. Item 05's benchmark notes that "zstd decode dominates, which is item 06". After this item, that is no longer true for chunked rounds.
- **Item 03 (listener EOF aborts the rest of the round)** and `track/rec-timeline` `settleTimelineOnStop`: both change the early `return` inside `Read`'s listener loop. That path skips `r.b = nil`. After item 03 or rec-timeline lands, a `Reader` whose `Read` returned early still holds `r.b`. This is harmless for the CLI and the library (the caller drops everything on error), but whoever lands second should consider `defer func() { r.b = nil }()`. That is not in this item, to avoid a three-way conflict.
- **Items 08 and 09** add `ScoreboardPlayer{ID: p.DissectID}` construction sites. D1 makes them safe without any change. **Item 12** (damage from the health stream) and any future listener must store entity IDs as `[4]byte` or clone them. Task 1's `TestMatchReader_DoesNotPinRoundBuffers` live-heap bound catches a regression in any of them.
- **Item 01 (incomplete-match crash)** touches `match.go`. There is no overlap.
- **`fork/health-dbno` (PR #3):** it edits the `Reader` struct, `NewReader`'s listener list and `Read`'s tail. There is no overlap with the decode functions or `readPlayer`'s `DissectID` line. Its health maps use `[4]byte`.
- **`track/rec-timeline` (PR #10):** `buildTimeline` reads `r.b` before `r.b = nil` and keeps no slices. `Reader.Data()` is caller-owned. There is no overlap in the edited lines (it changes `player.go` only at `r.addUpdate(u)`).
- **PR #12 (ElyasYassin):** it edits `scoreboard.go` only. There is no overlap.
- **JSON output / R6Stats consumer:** none. `DissectID` is `json:"-"`. `Scoreboard` is not in any output struct. Decompressed bytes are proven identical (Task 3). The R6Stats worker's memory use drops about 5×, with no code change on its side.
- **Golden tests:** no churn. `go test ./dissect/test -run TestReader_Read` passes without `-update`, and `git status` shows no `.json` change.

## Global Constraints

- Base: `origin/main` @ `bd6ccbe`. Go from `$HOME/.local/go/bin`.
- No new dependencies. `klauspost/compress` stays at `v1.17.11`.
- Output must be byte-identical: the CLI JSON and `--dump` bytes for every fixture and corpus round, and golden tests unchanged without `-update`.
- No GOGC/`debug.SetGCPercent` anywhere (D4).
- Do not touch `worker`, `Read`'s scan block or the `reader.go` imports (item 05's territory).

## Review Focus

1. **A false-positive zstd magic inside compressed data** inflates the hint. Expected: the output is unchanged, and capacity is capped at 32× compressed. → `TestDecompressedSizeHint/garbage_and_cap` (Task 2).
2. **A frame without a declared content size** (`HasFCS == false`). Expected: it adds 0 to the hint, and the buffer grows normally. → `TestDecompressedSizeHint/no_fcs` (Task 2).
3. **A truncated or corrupted `.rec`.** Expected: the same errors as before, no panic, no goroutine growth. → `TestNewReader_CorruptInputDoesNotLeakGoroutines` (Task 1).
4. **A future listener storing a slice of `r.b`.** Expected: the live-heap test fails. → `TestMatchReader_DoesNotPinRoundBuffers` (Task 1) bounds the live heap, not just `DissectID`.
5. **A sync decoder reading past the frame end** would break `r.offset += tempReader.n` and drop sections. Expected: the same `--dump` bytes and section count. → the Task 3 dump parity over 28 files.

---

## Implementation plan

### File structure

- Modify `dissect/player.go`: one line in `readPlayer`.
- Modify `dissect/reader.go`: the bodies of `readChunkedData` and `readNonChunkedData` only.
- Create `dissect/decompress.go`: `decompressedSizeHint`, `maxSizeHintRatio`, `appendWriter`.
- Create `dissect/memory_test.go` (`package dissect`, internal): the goroutine, pin, hint and allocation tests, plus `BenchmarkNewReader`.

The prototype diff is saved at `docs/research/fork-port-prototypes/06/prototype.diff`. It is session scratch and may be gone. It is the final layout (including `memory_test.go` with every test named below), and it was run: full `go test ./...` green, `AllocationBound` red against bd6ccbe's `reader.go` (`allocated 239208512 > bound 62238104`) and green after (`39 MB for 26 MB output`). The plan below is complete without it.

### Task 1: stop `DissectID` pinning the round buffer

**Files:**
- Create: `dissect/memory_test.go`
- Modify: `dissect/player.go` (`readPlayer`, the `DissectID: id,` line)

**Interfaces:**
- Produces (test helpers in `memory_test.go`, reused by Task 2):
  - `const chunkedFixture = "test/data/replays/valid/Y11S2/custom_early_plant.rec"`, `nonChunkedFixture = "test/data/replays/valid/Y8S1/ranked_1.rec"`, `matchFixtureDir = "test/data/replays/valid/Y8S2"`
  - `func settledGoroutines() int`: 5 × (`runtime.GC()`; `time.Sleep(20ms)`), then `runtime.NumGoroutine()`
  - `func readOnce(t *testing.T, path string)`: `os.Open`, `NewReader`, `Read`, and `t.Fatal` unless `Ok(err)`
  - `func quiet(t testing.TB)`: `zerolog.SetGlobalLevel(zerolog.Disabled)` with `t.Cleanup` restoring the previous level. Every test calls it first; otherwise the debug logs flood the output.

- [ ] **Step 1: Write the tests**
  - `TestNewReader_DoesNotLeakGoroutines`: subtests `chunked`/`nonChunked`. `readOnce` once to warm up, `before := settledGoroutines()`, 10 × `readOnce`, `after := settledGoroutines()`. Log `GOMAXPROCS`, `before`, `after`. `t.Errorf` if `after > before`.
  - `TestNewReader_CorruptInputDoesNotLeakGoroutines`: for each fixture × {`truncate`: `b[:len(b)/2]`, `corrupt`: XOR `0x5A` over `b[len(b)/3 : len(b)/3+4096]`}, 10 × (`NewReader(bytes.NewReader(b))`, then `Read` if no error). Assert `settledGoroutines()` does not grow. Errors are expected and ignored.
  - `TestMatchReader_DoesNotPinRoundBuffers`: `NewMatchReader` on `matchFixtureDir`, `Read`, `runtime.GC()` twice, `runtime.ReadMemStats`. For every round, for every `Header.Players[i].DissectID` and every `Scoreboard.Players[i].ID`: `t.Errorf` if `cap(...) > 16`. Then `t.Errorf` if `ms.HeapAlloc > 32<<20`. `runtime.KeepAlive(m)` at the end.

- [ ] **Step 2: Run them; the pin test fails, the goroutine tests pass**

  Run: `go test ./dissect -run 'DoesNotLeak|DoesNotPin' -v -count=1`
  Expected: both `DoesNotLeak` tests PASS (`before=3 after=3`). `TestMatchReader_DoesNotPinRoundBuffers` FAILS with `DissectID cap=` of about 1e8 per player and `live heap 530 MiB > 32 MiB`.

- [ ] **Step 3: Implement**

  In `dissect/player.go` `readPlayer`, replace `DissectID: id,` with `DissectID: bytes.Clone(id), // a copy: a slice of r.b would pin the whole round buffer after Read`. `bytes` is already imported.

- [ ] **Step 4: Run the tests and the golden tests**

  Run: `go test ./dissect -run 'DoesNotLeak|DoesNotPin' -v -count=1 && go test ./dissect/test -run TestReader_Read -count=1 && git status --short`
  Expected: all PASS. The pin test logs `live heap after Read+GC: 1.4 MiB` (anything under 5 MiB is fine). `git status` shows only `dissect/player.go` and `dissect/memory_test.go`.

- [ ] **Step 5: Commit**

  ```bash
  git add dissect/player.go dissect/memory_test.go
  git commit -m "fix(player): copy DissectID so a read round's buffer can be freed"
  ```

### Task 2: decode chunked sections onto one pre-sized buffer, and close the decoders

**Files:**
- Create: `dissect/decompress.go`
- Modify: `dissect/reader.go` (`readChunkedData`, `readNonChunkedData` bodies only)
- Modify: `dissect/memory_test.go` (add tests and the benchmark)

**Interfaces:**
- Consumes: the Task 1 helpers `quiet`, `chunkedFixture`, `nonChunkedFixture`.
- Produces, in `dissect/decompress.go`:
  - `const maxSizeHintRatio = 32`
  - `func decompressedSizeHint(compressed []byte) int`: for every occurrence of `28 B5 2F FD` (found with `bytes.Index`, advancing 4 bytes past each hit), `var h zstd.Header`; if `h.Decode(compressed[i:]) == nil && h.HasFCS`, add `h.FrameContentSize`. Return `min(total, maxSizeHintRatio*len(compressed))` as an `int`. The doc comment says it is a capacity hint only.
  - `type appendWriter struct{ b []byte }` with `func (w *appendWriter) Write(p []byte) (int, error)` (`w.b = append(w.b, p...)`). The doc comment says why it is not `bytes.Buffer`: append grows by about 1.25× for large slices, and `bytes.Buffer` by 2×.

- [ ] **Step 1: Write the tests**
  - `TestDecompressedSizeHint`, with subtests:
    - `chunked`: `raw := os.ReadFile(chunkedFixture)`, `r, _ := NewReader(bytes.NewReader(raw))`. Assert `decompressedSizeHint(raw) == len(r.b)` (27619511).
    - `nonChunked`: the same with `nonChunkedFixture` (173112432).
    - `no_fcs`: `decompressedSizeHint([]byte{0x28, 0xB5, 0x2F, 0xFD, 0x00, 0x00})` equals `0`. This is frame-header descriptor `0x00`: no FCS, single-segment unset. If `Decode` errors on this input, it still must be 0.
    - `garbage_and_cap`: a 64-byte slice containing magic followed by a single-segment descriptor that declares an FCS of `1<<40`. Build it with `zstd.NewWriter(nil, zstd.WithEncoderCRC(false))` `EncodeAll` on 1 byte, then patch the FCS field, or hand-write `28 B5 2F FD E0` + 8-byte LE `1<<40` + one raw last block `01 00 00`. Assert the result `<= 32*len(slice)`.
  - `TestReadChunkedData_AllocationBound`: `quiet`; `raw := os.ReadFile(chunkedFixture)`; `runtime.GC()`; read `MemStats.TotalAlloc`; `r, err := NewReader(bytes.NewReader(raw))`; read `TotalAlloc` again. Assert `delta <= 2*len(r.b) + 2*len(raw)`, and log the delta. Base is about 239 MB against a bound of about 64 MB (red). The fix is about 41 MB (green).
  - `BenchmarkNewReader`: subtests per fixture, `b.ReportAllocs()`, `NewReader(bytes.NewReader(raw))` per iteration.

- [ ] **Step 2: Run them**

  Run: `go test ./dissect -run 'SizeHint|AllocationBound' -v -count=1`
  Expected: a compile failure (`undefined: decompressedSizeHint`). After stubbing it to `return 0`, `AllocationBound` FAILS with a delta of about 239 MB.

- [ ] **Step 3: Implement `dissect/decompress.go`** (the Interfaces above).

- [ ] **Step 4: Change `readChunkedData`**

  - Replace `zstdReader, _ := zstd.NewReader(nil)` with `zstdReader, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))`, then `if err != nil { return err }`, then `defer zstdReader.Close()`. `err` already exists in scope, so `:=` here only declares `zstdReader`.
  - Replace `data := make([]byte, 0)` with `data := appendWriter{b: make([]byte, 0, decompressedSizeHint(r.b[r.offset:]))}`. `r.offset` is just past the header at this point.
  - Replace `decompressed, err := io.ReadAll(zstdReader)` and its error check with `n, err := zstdReader.WriteTo(&data)` and `if err != nil && !(n > 0 && errors.Is(err, zstd.ErrMagicMismatch)) { return err }`, keeping the `:=` shadowing exactly as the old line did. Delete `data = append(data, decompressed...)`.
  - Replace `r.b = data` with `r.b = data.b`.

- [ ] **Step 5: Change `readNonChunkedData`**: add `defer zstdReader.Close()` right after the `zstd.NewReader(genericReader)` error check. Change nothing else (D5).

- [ ] **Step 6: Run everything**

  Run: `gofmt -l dissect; go vet ./... && go test ./... -count=1 && go test ./dissect -run XXX -bench BenchmarkNewReader -benchtime 5x && git status --short`
  Expected: gofmt prints nothing and all tests PASS. The benchmark shows `custom_early_plant.rec` at about `41 MB B/op` (base 239 MB) and `ranked_1.rec` unchanged at about 397 MB. `git status` shows no `.json` changes.

- [ ] **Step 7: Commit**

  ```bash
  git add dissect/decompress.go dissect/reader.go dissect/memory_test.go
  git commit -m "perf(reader): decode zstd sections onto one pre-sized buffer and close the decoders"
  ```

### Task 3: parity proof and measurements (no code)

- [ ] **Step 1: Build the base and new binaries.** `git stash`-free: `git worktree add /tmp/<scratch>/base bd6ccbe && (cd /tmp/<scratch>/base && go build -o /tmp/<scratch>/r6-base .)`, then `go build -o /tmp/<scratch>/r6-new .` on the branch.
- [ ] **Step 2: Check byte parity.** For every `.rec` in `dissect/test/data/replays/valid/*/`, in the Y11S3 fixtures from `origin/track/rec-timeline` (`git show origin/track/rec-timeline:dissect/test/data/replays/valid/Y11S3/<f>.rec > /tmp/<scratch>/<f>.rec`), and in one Y11S2 plus one Y11S3 user match folder, run both binaries with `--dump <f> -o …` and with `<f> -o ….json`, then `cmp`. Then run both binaries on all 5 user match folders, which gives JSON for each folder, and `cmp`.
  Expected: `diffs=0`. At planning time: 28 files and their dumps, plus 5 folders, all identical.
- [ ] **Step 3: Measure RSS.** `/usr/bin/time -v <bin> <match folder> -o /dev/null` for both binaries on the 5 folders. Record `Maximum resident set size`.
  Expected: about 5× lower (planning: 1492/795/1568/1177/600 → 291/154/255/275/286 MiB).
- [ ] **Step 4: Run the library smoke test (optional, if gcc is present).** `go build -buildmode=c-shared -o /tmp/<scratch>/lib.so ./exports`. Call `dissect_read` 4 times through Python `ctypes` (`restype = c_void_p`; free with `dissect_free`), and print `VmRSS` from `/proc/self/status`.
  Expected: stable at about 200 MiB (base about 1.5 GB).
- [ ] **Step 5: Open the PR** with `/ship`. Title: `fix(reader): stop pinning round buffers; decode zstd onto one pre-sized buffer`. In the body, put the RSS table, the benchmark lines and the parity result. Note that the survey's goroutine-leak claim was refuted, with evidence. Credit Animuxs `2ad47cd`, and state why GOGC was not ported (D4).

## Acceptance

- `go test ./dissect -run 'DoesNotPin|AllocationBound' -count=1` is **red at bd6ccbe** (530 MiB live heap, about 239 MB allocated per round) and **green after**.
- `go test ./... -count=1` is green. Golden JSON is unchanged without `-update`.
- CLI JSON and `--dump` bytes are identical for every fixture and corpus round (Task 3 step 2).
- Peak RSS on `Match-2026-08-25_19-56-00-32000` is ≤ 400 MiB (base about 1.5 GB).

## Risks / open questions

- **The hint relies on `FrameContentSize`.** If a future season stops writing FCS, the hint becomes 0 and we fall back to append growth: still correct, and still better than base thanks to the sync decoder. No risk to output.
- **The sync decoder's read-ahead.** `r.offset += tempReader.n` depends on how many bytes the decoder pulled from `countedReader`. The parity of `--dump` over 28 files proves that the section boundaries are unchanged for current data. A klauspost upgrade should rerun Task 3 step 2.
- **Timing is not measured.** The host had a load average of 100 to 600 during planning. The benchmark showed 243 → 44 ms per chunked round in one same-session comparison, but treat it as indicative. If the implementer has a quiet host, add a wall-time column.
- **GOGC/GOMEMLIMIT for the R6Stats worker** is left as an ops decision (D4). If the worker container gets a `mem_limit`, set `GOMEMLIMIT` to about 80% of it in `compose.yaml`'s `worker.environment`. That is an R6Stats change, not a parser one.
- **The early-return path in `Read` keeps `r.b`** (see Interactions, item 03). This is known and deferred.
