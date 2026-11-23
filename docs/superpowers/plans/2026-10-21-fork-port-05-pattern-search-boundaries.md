# Fork port 05: the pattern scanner can drop packet matches. Design and implementation plan

> **Issue:** #33 · prestonwillis10/r6-dissect · fork survey: `docs/research/2026-10-21-fork-survey.md` · prototype: `docs/research/fork-port-prototypes/05/`

> **For agentic workers:** REQUIRED SUB-SKILL: use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to carry out this plan task by task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `Reader.Read` hands every listener every occurrence of its pattern, in a fixed order (offset, then registration order), whatever the worker split or the bytes before the pattern.

**Architecture:** Replace the 5-block byte state machine (`Reader.worker` plus the block split in `Reader.Read`) with `Reader.findQueries`. It runs one `bytes.Index` loop per registered query over the whole scan range, one goroutine per query, then sorts the matches by `(offset, listenerIndex)`. The `match` struct and the dispatch loop stay as they are.

**Tech stack:** Go 1.26 (`go.mod`), standard library `bytes`, `cmp`, `slices`, `sync`. No new dependency.

**Base:** `origin/main` at `bd6ccbeb7b46af99638df5d650187069e849f925`. Every quote below is from that commit.

**Item boundaries:** item 06 owns the zstd/decoder/memory half of the same fork commit (`readChunkedData`). This plan does not touch `readChunkedData`, `readNonChunkedData` or `NewReader`.

---

# Part 1: Design

## Summary

`Reader.Read` splits the decompressed round into 5 blocks and scans each with its own copy of a byte-at-a-time state machine. That scanner loses a pattern occurrence in two ways:

1. **Block boundary.** An occurrence that starts in one block and ends in the next is not seen by either worker. This is certain, and a unit test shows it.
2. **Failed partial match.** When the scanner has a partial match of query `q` in progress and the next byte breaks it, it resets that query to 0 and does not test the same byte as the start of a new match. So `A1 A1 B2 C3 D4` never matches `A1 B2 C3 D4`, and `A1 B2 A1 B2 A1 C3` never matches `A1 B2 A1 C3`. This is certain, and unit tests show it.

The survey also said that `sort.Slice` is unstable on tied offsets. That is true in principle. With today's query set, though, a tie cannot happen: two matches share an offset only when one query is a suffix of another, and none is (checked below). The fix makes the order fully defined anyway.

**Real-world impact is latent.** On 102 real rounds (6.1 GB of decompressed data, about 2.75 M matches) the old scanner and an exact `bytes.Index` search found **exactly the same match set**. The golden tests and the CLI JSON for all five of the user's Y11S2/Y11S3 matches are byte-identical before and after the change. The bug is real and can silently drop a kill, time or scoreboard packet, but it is rare: a rough estimate is under 1 in 1000 rounds on Y11 data. That makes this change **safe to land**. Output does not move, the scan gets about 3 to 5 times faster, and correctness no longer depends on luck.

## Source

- Fork: **Animuxs** (`Animuxs/main`), commit `2ad47cdb0a525ec60795861f05693c9635a67d16`, "Speed up replay parsing, cut its memory, and add TXT export" (Rey T, 2026-09-25). File: `dissect/reader.go`.
- What they did: they deleted `worker` and the block split, and added `findQueries(start, end int) []match`. It runs one goroutine per query with `bytes.Index(b[i:], query)`, advances `i += k + len(query)`, records `match{start + i - 1, j}`, then does `slices.Concat` and sorts with `cmp.Or(cmp.Compare(a.offset, b.offset), cmp.Compare(a.listenerIndex, b.listenerIndex))`. They claim byte-identical output on 22 matches and 13 single rounds, and 592 ms → 74 ms for the scan of a 74 MB round.
- Their base was upstream's naive scanner, which compared every query at every byte. Ours had already been sped up in `ceac7d2` ("perf: only compare patterns that can match the current byte"), so our gain is smaller than theirs (measured below).
- Flaws and gaps in the fork version:
  - It ships no test for the boundary or the partial-match cases. Their "byte-identical" claim is consistent with our finding that the bug almost never triggers, but it proves nothing about correctness.
  - The commit bundles the zstd, memory and GOGC changes (item 06), plus Python app changes. Only the `findQueries` hunk is ported here.
  - The goroutine closure relies on Go 1.22 per-iteration loop variables. That is fine for us (`go 1.26.0`), but this plan passes `j` and `query` as arguments so the code reads unambiguously.
  - It does not bound-check `end` against `len(r.b)`. This plan does.

The code in this plan was written from the idea, not copied: the structure is the same, the details differ.

## Current behaviour (bd6ccbe)

`dissect/reader.go`, `Read`, splits the range:

```go
	numWorkers := 5
	...
	blockSize := int(math.Floor(float64(end-start) / float64(numWorkers)))
	...
		blockStart := r.offset + (i * blockSize)
		blockEnd := blockStart + blockSize
		if i > 0 {
			blockStart += 1
		}
```

Each worker gets a fresh state (`indexes := make([]int, len(r.queries))`), so a match spanning `blockEnd`/`blockStart` is lost.

`dissect/reader.go`, `worker`, on a mismatch:

```go
			if c == query[indexes[j]] {
				...
			} else {
				indexes[j] = 0
			}
```

The `started` list is built before this loop and only from queries with `indexes[j] == 0`, so a query that was mid-match on byte `c` is never restarted at `c`. The comment above the loop says this is "equivalent to comparing every query at every byte". That is true: upstream's naive loop has the same flaw.

Ordering: `sort.Slice(matches, func(i, j int) bool { return matches[i].offset < matches[j].offset })`, fed from a channel that all 5 workers write to concurrently.

### Reproduction (run on bd6ccbe in the scratch clone)

The five tests of Task 1, run against unmodified bd6ccbe `reader.go`:

```
$ go test ./dissect -run 'TestRead_' -v 2>&1 | grep -v '^{'
--- FAIL: TestRead_PatternAcrossWorkerBlockBoundary   hits = [], want [{0 203}]
--- FAIL: TestRead_PatternAfterRepeatedFirstByte      hits = [], want [{0 15}]
--- FAIL: TestRead_SelfOverlappingPrefix              hits = [], want [{0 16}]
--- FAIL: TestRead_TiedOffsetsRunInListenerOrder      hits = [{1 14} {0 14}], want [{0 14} {1 14}]
--- FAIL: TestRead_RepeatedPrefixRun                  got 4 hits, want 200
FAIL	github.com/redraskal/r6-dissect/dissect
```

`RepeatedPrefixRun` repeats `22 22 22 07 00` 200 times and searches for `22 22 07`. The old scanner finds only 4 of the 200 occurrences, the ones where a worker block happens to start at the right byte.

### Real replays: does it trigger?

This was measured with a scratch harness, not part of the plan. It holds a verbatim copy of the bd6ccbe `worker` and block split, and an exact per-query `bytes.Index` search, both run over `r.b` of each round after `NewReader`. It counts the matches found by one and not the other, and classifies each miss as boundary or partial-match.

| corpus | rounds | decompressed | matches (old) | only-new | only-old |
|---|---|---|---|---|---|
| user Y11S2 (2 matches) + Y11S3 (3 matches) | 42 | 2.76 GB | 43,506 | 0 | 0 |
| main fixtures (Y8S1, Y8S2, Y9S1, Y11S2), Y11S3 fixtures from `track/rec-timeline`, ArnaudCabello `Matchreplay/`, nicktstewart `defuser_detection/` | 60 | 3.33 GB | 2,704,706 | 0 | 0 |

So no golden and no real match changes. An order-of-magnitude estimate for the boundary case: per round, about 4 boundaries × matches × (pattern length − 1) / bytes. That is about 2e-4 per Y11 round and about 3% per dense Y8 fixture round, which is consistent with seeing zero in 102 rounds. The partial-match case needs the bytes just before a packet marker to repeat that marker's first bytes. The packet layout evidently never does that for today's markers.

Query-set facts, checked with a script over every pattern registered at bd6ccbe and on `fork/health-dbno` (`41 54 DC C4`, `E7 88 F6 A5 04`):
- No query is a prefix or a suffix of another. So no two matches can share an end offset, and the old `sort.Slice` never actually saw a tie.
- No query has a border (a proper prefix that is also a suffix). So two occurrences of one query cannot overlap, and the leftmost non-overlapping search finds every occurrence. Consequence: **old matches ⊆ new matches**, so any change the fix makes to output can only add matches.

## Approaches considered

**A. Per-query `bytes.Index` over the whole range, one goroutine per query, then a deterministic sort (chosen).**
- Pros: correct by construction, with no split and no state machine. `bytes.Index` uses SIMD (`IndexByte` on the first byte, then `bytealg.Index`). It is the smallest code (about 45 lines replace about 75). The fork has validated it on 22 matches.
- Cons: it makes one pass over the round per query (17 to 20 queries), so cost grows with the listener count, at about 10 ms per query for a 70 MB round. It spawns one goroutine per query.

**B. Keep the state machine, overlap the blocks by `maxLen-1` bytes, de-duplicate, and switch to KMP-style failure links.**
- Pros: one pass over the data for all queries.
- Cons: it fixes both bugs only with significantly more code (failure tables, overlap de-duplication). It is still slower than SIMD `bytes.Index` per byte. It is easy to get subtly wrong, which is how we got here.

**C. A single-pass multi-pattern matcher (Aho-Corasick, or a 2-byte prefix table plus `bytes.HasPrefix`).**
- Pros: the best asymptotic cost as the listener count grows (items 02, 09, 10, 11 and 12 all add listeners).
- Cons: new code or a new dependency. A pure-Go Aho-Corasick walks byte by byte and would likely lose to A at 17 to 20 queries. Premature.

**D. Sequential `bytes.Index` (A without goroutines).**
- Measured on a 76 MB Y11S3 round under the same load: about 200 ms sequential against about 130 ms parallel, and about 580 ms against about 280 ms on the dense Y8S1 fixture. The goroutines give about 1.6 to 2 times. They are cheap, and the old code was already parallel (5 workers), so A keeps parallel behaviour.

## Decision and rationale

**Approach A**, with these decisions (made autonomously, alternatives noted):

1. **Non-overlapping per query: resume at `i += k + len(query)`.** This matches what the old scanner did after a completed match. Because no current query has a border, it is also "every occurrence". The alternative, `i += k + 1` to report overlapping occurrences, would change semantics only for a periodic pattern, which nobody uses. It was rejected to keep parity.
2. **`match.offset` stays "index of the last byte".** Listeners get `r.offset = entry.offset + 1`, unchanged. No listener can tell the difference.
3. **Order is `(offset, listenerIndex)`.** Listener index is registration order (`Reader.Listen` appends; `MatchReader.Listen` registers after the built-ins). That is the most predictable tie-break, and it is what the fork chose. The alternative, order by start offset, would change dispatch order relative to today for no gain.
4. **One goroutine per query, each writing its own `found[j]` slice.** There is no channel and no mutex, so it is race-free by construction (verify with `-race`).
5. **Bounds guard: `start < 0 || end > len(r.b) || start >= end` returns `nil`.** This also removes the empty-body index-out-of-range panic in `worker` (item 01b's symptom) as a side effect. Item 01b still owns returning `ErrNoReplayData`.
6. **Remove `worker`, the `numWorkers` split and their debug logs.** Keep the existing `log.Debug().Int("matches", ...)` line.
7. **No golden regeneration is expected.** The plan still gives the procedure for when it does happen (for example, after rebasing onto a branch that adds queries).
8. **The scratch harness is not kept** (it needs a frozen copy of the old scanner). Correctness is pinned instead by a brute-force reference test on random data from a 3-letter alphabet, which makes partial matches very frequent.

## Interactions

- **`origin/track/rec-timeline` (PR #10, tip `fdaab61`).** `git diff origin/main...origin/track/rec-timeline -- dissect/reader.go` adds 3 Reader fields (`timelineEnabled`, `timeline`, `timelineErr`), a `r.settleTimelineOnStop(err)` line inside the dispatch loop, `stopDefuserRunAtRoundEnd`, `buildTimeline` after `applyLoadouts`, and `func (r *Reader) Data() []byte`. None of these touch the scan block. **Tested:** merging the prototype into the branch tip is clean, `go test ./...` passes, and `TestReader_Read -update` leaves the 15 goldens (including the 3 Y11S3 ones) unchanged.
- **`origin/fork/health-dbno` (PR #3, tip `c608cf8`).** This adds 2 `Listen` calls in `NewReader` and post-processing calls after `roundEnd`. **Tested:** the merge is clean, all tests pass, and the goldens are unchanged. Its two new queries have no prefix, suffix or border relation to the others.
- **`ElyasYassin/fix/deterministic-scoreboard-reconcile` (PR #12).** It does not touch `reader.go`.
- **Item 06 (zstd / decoder Close, same fork commit).** It edits `readChunkedData` only, a different function. The only shared spot is the `import` block: this plan adds `"cmp"` and `"slices"` and drops `"sort"`, and keeps `"math"` because `Float32` uses `math.Float32frombits`. Either order merges with at most a trivial import conflict.
- **Item 01 / 01b (incomplete match, empty body).** 01b's Task 3 inserts `if r.offset >= len(r.b) { return ErrNoReplayData }` "before `numWorkers := 5`", and this plan deletes that line. Resolution: put the guard first in `Read`, above `end := len(r.b)`. If 05 lands first, 01b's red step no longer panics. It fails by returning `nil` instead of `ErrNoReplayData`, and the 01b implementer should expect that.
- **Item 03 (listener EOF aborts rest of round).** It edits the dispatch loop (`if err = listener(r); err != nil { return }`) a few lines below this hunk. A merge conflict is not expected. Rebase whichever lands second.
- **Items that add listeners (02, 09, 10, 11, 12).** Each new query adds one `bytes.Index` pass, about 10 ms per 70 MB round. Ties at one offset now run in registration order, which is deterministic, so a new listener that needs to run before an existing one at the same byte must be registered first.
- **JSON output / R6Stats.** There is no schema change and no value change on the measured corpus. R6Stats (`api/r6stats/services/dissect.py`, `run_dissect` → `[str(bin), str(folder)]`, 600 s timeout) needs no change. It gets a faster parse.
- **Golden churn.** None at bd6ccbe, at `track/rec-timeline` + this change, or at `fork/health-dbno` + this change.

---

# Part 2: Implementation plan

## Global constraints

- Work on a branch from `origin/main` (bd6ccbe or later), for example `fix/pattern-search`.
- `export PATH=$HOME/.local/go/bin:$PATH`. Tests log zerolog debug JSON to stderr, so filter it with `2>&1 | grep -v '^{'`.
- Commit labels: these are code commits, so use `test(reader):`, `fix(reader):` or `perf(reader):`. Never `docs(...)`.
- Do not touch `readChunkedData`, `readNonChunkedData` or `NewReader` (item 06 and others).
- End commit messages with the attribution line in force for your session.

## Review focus

- The red tests must fail on bd6ccbe for the stated reason: empty hits, not a compile error or a panic.
- `findQueries` must be exactly equivalent to `referenceMatches` (Task 2), including at the range edges `start` and `end`.
- There is no shared mutable state between goroutines except the per-index `found[j]` writes. `go test -race` must be clean.
- The golden diff must be empty. If it is not, follow "Reviewing a changed golden" in Task 4 before accepting it.
- Input classes no golden exercises, each pinned by a test:
  - empty or inverted range (`start >= end`, `end > len(r.b)`, `start < 0`): `TestFindQueries_OutOfRange`;
  - a match ending on the very last byte of the range, or starting at `start`: covered by the random cases in `TestFindQueries_MatchesReference` (`start`/`end` are drawn at random);
  - `ReadPartial` (`end /= 3`): `scanHits` uses `readPartial: true`, so every `TestRead_*` test runs through it;
  - two queries ending on the same byte: `TestRead_TiedOffsetsRunInListenerOrder`;
  - single-byte and duplicate queries: covered by the random cases (query length 1 to 5, possibly equal).

## File structure

- Modify: `dissect/reader.go`. Remove `worker`, replace the scan block in `Read`, add `findQueries`, and update the imports.
- Create: `dissect/pattern_search_test.go`, the behavioural tests through `Read`, red today.
- Create: `dissect/pattern_reference_test.go`, a brute-force oracle plus a random test.
- Create: `dissect/pattern_bench_test.go`, benchmarks that build on both old and new code.

### Task 1: Failing tests that show the dropped matches

**Files:** Create `dissect/pattern_search_test.go` (package `dissect`, internal).

- [ ] **Step 1: Write the tests.**

```go
package dissect

import (
	"bytes"
	"reflect"
	"testing"
)

// hit is one listener call: which listener ran and where r.offset pointed.
type hit struct {
	listener int
	offset   int
}

// scanHits runs Read over b with one recording listener per pattern and
// returns the listener calls in the order Read made them.
func scanHits(t *testing.T, b []byte, patterns ...[]byte) []hit {
	t.Helper()
	r := &Reader{readPartial: true}
	var hits []hit
	for i, p := range patterns {
		i := i
		r.Listen(p, func(r *Reader) error {
			hits = append(hits, hit{i, r.offset})
			return nil
		})
	}
	// readPartial scans only the first third of r.b and skips the end-of-round
	// processing; pad to 3x so exactly b is scanned.
	r.b = append(append([]byte{}, b...), make([]byte, 2*len(b))...)
	if err := r.Read(); err != nil {
		t.Fatal(err)
	}
	return hits
}

// plant returns n zero bytes with p written at off.
func plant(n, off int, p []byte) []byte {
	b := make([]byte, n)
	copy(b[off:], p)
	return b
}

func TestRead_PatternAcrossWorkerBlockBoundary(t *testing.T) {
	p := []byte{0xA1, 0xB2, 0xC3, 0xD4}
	// bd6ccbe splits 1000 bytes into 5 worker blocks: [0,200], [201,400], ...
	// The pattern occupies 199..202, across the 200/201 boundary.
	got := scanHits(t, plant(1000, 199, p), p)
	want := []hit{{0, 203}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

func TestRead_PatternAfterRepeatedFirstByte(t *testing.T) {
	p := []byte{0xA1, 0xB2, 0xC3, 0xD4}
	// A1 A1 B2 C3 D4: the first A1 starts a partial match that fails on the second A1.
	got := scanHits(t, plant(1000, 10, append([]byte{0xA1}, p...)), p)
	want := []hit{{0, 15}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

func TestRead_SelfOverlappingPrefix(t *testing.T) {
	p := []byte{0xA1, 0xB2, 0xA1, 0xC3}
	// A1 B2 A1 B2 A1 C3: the match starts at the second A1, inside a failed partial.
	in := []byte{0xA1, 0xB2, 0xA1, 0xB2, 0xA1, 0xC3}
	got := scanHits(t, plant(1000, 10, in), p)
	want := []hit{{0, 16}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

func TestRead_TiedOffsetsRunInListenerOrder(t *testing.T) {
	short := []byte{0xC3, 0xD4}
	long := []byte{0xA1, 0xB2, 0xC3, 0xD4}
	// Both patterns end on the same byte; the listener registered first runs first.
	got := scanHits(t, plant(1000, 10, long), short, long)
	want := []hit{{0, 14}, {1, 14}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

func TestRead_RepeatedPrefixRun(t *testing.T) {
	// 22 22 22 07: each match starts at the second 22, inside a failed partial match.
	p := []byte{0x22, 0x22, 0x07}
	b := bytes.Repeat([]byte{0x22, 0x22, 0x22, 0x07, 0x00}, 200)
	got := scanHits(t, b, p)
	if len(got) != 200 {
		t.Fatalf("got %d hits, want 200", len(got))
	}
}
```

- [ ] **Step 2: Run them and see them fail.**
  Run: `go test ./dissect -run 'TestRead_' -count=1 -v 2>&1 | grep -v '^{'`
  Expected (verified on bd6ccbe): all 5 FAIL. Boundary: `hits = [], want [{0 203}]`. Repeated first byte: `hits = [], want [{0 15}]`. Self-overlap: `hits = [], want [{0 16}]`. Tie: `hits = [{1 14} {0 14}], want [{0 14} {1 14}]`. Run: `got 4 hits, want 200`.

- [ ] **Step 3: Commit.**
  `git add dissect/pattern_search_test.go && git commit -m "test(reader): show the pattern scanner dropping matches at block boundaries and after partial matches"`

  If the branch must stay green per commit, fold this commit into Task 2's commit instead.

### Task 2: Replace the scanner with `findQueries`

**Files:** Modify `dissect/reader.go`. Create `dissect/pattern_reference_test.go`.

**Interfaces:**
- Consumes: the existing `type match struct { offset, listenerIndex int }` and `r.queries [][]byte`.
- Produces: `func (r *Reader) findQueries(start, end int) []match`. It returns `nil` when the range is empty or invalid, and otherwise returns the matches sorted by `(offset, listenerIndex)`, where `offset` is the index of the pattern's last byte. Task 3's benchmarks and any later item (01b, 03) rely on this name and on `Read` calling it once.

- [ ] **Step 1: Write the reference test (it fails to build until Step 3).**

```go
package dissect

import (
	"bytes"
	"cmp"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
)

// referenceMatches is the brute-force definition findQueries must meet: for each
// query, every leftmost non-overlapping occurrence in b[start:end], reported by
// the index of its last byte, ordered by offset and then by query index.
func referenceMatches(b []byte, queries [][]byte, start, end int) []match {
	var out []match
	for j, q := range queries {
		for i := start; i+len(q) <= end; {
			if bytes.Equal(b[i:i+len(q)], q) {
				out = append(out, match{offset: i + len(q) - 1, listenerIndex: j})
				i += len(q)
			} else {
				i++
			}
		}
	}
	slices.SortFunc(out, func(a, b match) int {
		return cmp.Or(cmp.Compare(a.offset, b.offset), cmp.Compare(a.listenerIndex, b.listenerIndex))
	})
	return out
}

// randomCase draws bytes and queries from a 3-letter alphabet so that partial
// matches, repeated prefixes and self-overlapping queries are common.
func randomCase(rng *rand.Rand) ([]byte, [][]byte, int, int) {
	alphabet := []byte{0xA1, 0xB2, 0xC3}
	n := 1 + rng.IntN(400)
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[rng.IntN(len(alphabet))]
	}
	queries := make([][]byte, 1+rng.IntN(4))
	for i := range queries {
		q := make([]byte, 1+rng.IntN(5))
		for k := range q {
			q[k] = alphabet[rng.IntN(len(alphabet))]
		}
		queries[i] = q
	}
	start := rng.IntN(n)
	end := start + rng.IntN(n-start+1)
	return b, queries, start, end
}

func TestFindQueries_MatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 2026))
	for c := 0; c < 20000; c++ {
		b, queries, start, end := randomCase(rng)
		r := &Reader{b: b, queries: queries}
		got := r.findQueries(start, end)
		want := referenceMatches(b, queries, start, end)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("case %d: b=%X queries=%X [%d,%d)\n got %v\nwant %v", c, b, queries, start, end, got, want)
		}
	}
}

func TestFindQueries_OutOfRange(t *testing.T) {
	r := &Reader{b: []byte{1, 2, 3}, queries: [][]byte{{1}}}
	for _, c := range [][2]int{{0, 0}, {3, 3}, {2, 1}, {-1, 2}, {0, 4}} {
		if got := r.findQueries(c[0], c[1]); got != nil {
			t.Errorf("findQueries(%d, %d) = %v, want nil", c[0], c[1], got)
		}
	}
}
```

- [ ] **Step 2: Run it and see it fail.**
  Run: `go test ./dissect -run TestFindQueries -count=1 2>&1 | grep -v '^{'`
  Expected: a build failure, `r.findQueries undefined (type *Reader has no field or method findQueries)`.

- [ ] **Step 3: Implement.** In `dissect/reader.go`:
  1. Imports: add `"cmp"` after `"bytes"`, replace `"sort"` with `"slices"`, and keep `"math"` (`Float32` uses it) and `"sync"`.
  2. Replace the `match` struct and the whole `func (r *Reader) worker(...)` with:

```go
type match struct {
	offset        int // index of the last byte of the pattern; listeners start at offset+1
	listenerIndex int // index into r.queries / r.listeners
}

// findQueries returns every occurrence of every query in r.b[start:end],
// ordered by offset and then by listener index (registration order).
//
// Each query is searched for independently with bytes.Index over the whole
// range, so an occurrence is never lost to a block split or to a partial
// match of the same query that started a few bytes earlier. After an
// occurrence the search resumes past its last byte, so occurrences of one
// query never overlap (the old scanner behaved the same way after a
// completed match).
func (r *Reader) findQueries(start, end int) []match {
	if start < 0 || end > len(r.b) || start >= end {
		return nil
	}
	b := r.b[start:end]
	found := make([][]match, len(r.queries))
	var wg sync.WaitGroup
	for j, query := range r.queries {
		if len(query) == 0 {
			continue
		}
		wg.Add(1)
		go func(j int, query []byte) {
			defer wg.Done()
			var out []match
			for i := 0; i <= len(b)-len(query); {
				k := bytes.Index(b[i:], query)
				if k < 0 {
					break
				}
				i += k + len(query)
				out = append(out, match{offset: start + i - 1, listenerIndex: j})
			}
			found[j] = out
		}(j, query)
	}
	wg.Wait()
	n := 0
	for _, f := range found {
		n += len(f)
	}
	matches := make([]match, 0, n)
	for _, f := range found {
		matches = append(matches, f...)
	}
	slices.SortFunc(matches, func(a, b match) int {
		return cmp.Or(cmp.Compare(a.offset, b.offset), cmp.Compare(a.listenerIndex, b.listenerIndex))
	})
	return matches
}
```

  3. In `Read`, replace everything from `numWorkers := 5` through the closing `})` of `sort.Slice(...)` with:

```go
	end := len(r.b)
	if r.readPartial {
		end /= 3
	}
	matches := r.findQueries(r.offset, end)
```

  Leave `log.Debug().Int("matches", len(matches)).Msg("calling listeners")` and everything after it untouched.

- [ ] **Step 4: Run the new tests and see them pass.**
  Run: `go test ./dissect -run 'TestRead_|TestFindQueries' -count=1 -race -v 2>&1 | grep -E '^(---|ok|FAIL|PASS)'`
  Expected: 7 `--- PASS` lines and `ok`. No race report.

- [ ] **Step 5: Lint.** Run `gofmt -l dissect` (no output), `go vet ./...` (no output), and `staticcheck ./...` if it is installed (CI runs it; it should report no unused `math`/`sort`).

- [ ] **Step 6: Commit.**
  `git add dissect/reader.go dissect/pattern_reference_test.go && git commit -m "fix(reader): find every pattern occurrence with one bytes.Index pass per query"`
  In the body, credit Animuxs `2ad47cd` and state that output is byte-identical on the corpus (Task 4).

### Task 3: Benchmarks

**Files:** Create `dissect/pattern_bench_test.go`. It is written so that it also builds on bd6ccbe, which lets you measure the "before" numbers.

- [ ] **Step 1: Write the benchmarks.**

```go
package dissect

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

// benchRound is the round the benchmarks read: $BENCH_REC, or a committed fixture.
func benchRound() string {
	if p := os.Getenv("BENCH_REC"); p != "" {
		return p
	}
	return "test/data/replays/valid/Y11S2/custom_solid_snake.rec"
}

func loadBenchRound(b *testing.B) *Reader {
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	b.Cleanup(func() { zerolog.SetGlobalLevel(zerolog.TraceLevel) })
	f, err := os.Open(benchRound())
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	r, err := NewReader(f)
	if err != nil {
		b.Fatal(err)
	}
	return r
}

// BenchmarkReadScan times Read with every listener replaced by a no-op, so it
// measures the pattern search plus dispatch.
func BenchmarkReadScan(b *testing.B) {
	src := loadBenchRound(b)
	noop := func(*Reader) error { return nil }
	b.SetBytes(int64(len(src.b) - src.offset))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := &Reader{b: src.b, offset: src.offset, queries: src.queries,
			lastDefuserPlayerIndex: -1, pendingDefuserPlantIdx: -1,
			scoreboardIDToPlayer: map[[4]byte]int{}, scoreboardInitialKills: map[[4]byte]uint32{},
			scoreboardFinalKills: map[int]uint32{}, lastPlayerScores: map[int]uint32{},
			defuserPawnToSlot: map[[4]byte][4]byte{}}
		r.listeners = make([][]func(*Reader) error, len(src.queries))
		for j := range r.listeners {
			r.listeners[j] = []func(*Reader) error{noop}
		}
		if err := r.Read(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReadRound times NewReader + Read of one round end to end.
func BenchmarkReadRound(b *testing.B) {
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	b.Cleanup(func() { zerolog.SetGlobalLevel(zerolog.TraceLevel) })
	for i := 0; i < b.N; i++ {
		f, err := os.Open(benchRound())
		if err != nil {
			b.Fatal(err)
		}
		r, err := NewReader(f)
		if err != nil {
			b.Fatal(err)
		}
		if err = r.Read(); err != nil && !Ok(err) {
			b.Fatal(err)
		}
		f.Close()
	}
}
```

  If `NewReader` on the branch you land on initialises more maps (for example `healthToSlot` and `healthState` from `fork/health-dbno`), add them to the `Reader` literal in `BenchmarkReadScan`. A nil map write there shows up as a panic.

- [ ] **Step 2: Measure before and after, interleaved.** Build both test binaries once, then alternate them so machine load affects both equally:
  ```bash
  git stash -u; git checkout -q bd6ccbe -- dissect/reader.go; go test -c -o /tmp/before.test ./dissect   # with only the bench file added
  git checkout -q HEAD -- dissect/reader.go; git stash pop; go test -c -o /tmp/after.test ./dissect
  for k in 1 2 3 4 5; do for v in before after; do BENCH_REC=<round.rec> /tmp/$v.test -test.run '^$' -test.bench 'BenchmarkRead' -test.benchmem 2>/dev/null | grep ^Bench | sed "s/^/$v /"; done; done
  ```
  With no `BENCH_REC`, the default fixture `custom_solid_snake.rec` gave 281 ms before and 50 ms after at planning time (one sample each; the bench file builds unchanged on bd6ccbe, which was verified).
  Expected: `BenchmarkReadScan` is about 3 to 5 times faster on a Y11 round and about 2.5 times on `Y8S1/ranked_1.rec`. `BenchmarkReadRound` improves by about 10 to 30% (zstd decode dominates, which is item 06).

  Planning-time numbers (Ryzen 9 7950X under WSL2, load average about 100 from other agents, medians of 5 interleaved runs):

  | round | ReadScan before | ReadScan after | ReadRound before | ReadRound after |
  |---|---|---|---|---|
  | Y11S3 `Match-2026-10-18_21-07-35-14012-R01` (76 MB) | 427 ms | 88 ms | 1106 ms | 804 ms |
  | Y11S2 `Match-2026-08-25_19-56-00-32000-R08` (90 MB) | 462 ms | 97 ms | 1459 ms | 1264 ms |
  | Y8S1 `ranked_1.rec` (173 MB, 384 k matches) | 947 ms | 363 ms | 1432 ms | 1282 ms |

  CLI wall time per match folder, before → after: 11.7 → 9.2 s, 6.0 → 3.5 s, 11.5 → 9.2 s, 12.2 → 5.7 s, 6.2 → 3.0 s. Allocations: the scan allocates about +10% on dense rounds (per-query slices). The total per round is unchanged (742 MB, dominated by zstd).

- [ ] **Step 3: Commit.** `git add dissect/pattern_bench_test.go && git commit -m "test(reader): benchmark the pattern scan and a full round read"`

### Task 4: Golden and corpus parity

- [ ] **Step 1: Full suite.** Run `go test -race ./... 2>&1 | grep -v '^{' | grep -E '^(ok|FAIL|---)'`. Expected: every package `ok`.
- [ ] **Step 2: Regenerate the goldens and check that nothing moved.**
  Run `go test ./dissect/test -run TestReader_Read -update -count=1 && git status --short dissect/test/data`.
  Expected: no output from `git status` (verified at planning time on main, on `track/rec-timeline` + this change, and on `fork/health-dbno` + this change). **Do not commit regenerated goldens unless Step 4 accepts them.**
- [ ] **Step 3: CLI JSON on the real corpus.** Build `before` from bd6ccbe and `after` from the branch (`go build -o /tmp/r6-<v> .`), then for each match folder in `.../a0d4e668-.../scratchpad/replays/Match-2026-08-25_*` and `.../replays-new/Match-2026-10-18_*`:
  `/tmp/r6-before $m > /tmp/$n.before.json; /tmp/r6-after $m > /tmp/$n.after.json; cmp /tmp/$n.before.json /tmp/$n.after.json && echo "$n IDENTICAL"`
  Expected: all 5 are IDENTICAL (planning-time sizes: 81678, 72220, 53904, 91194 and 31006 bytes).
- [ ] **Step 4: Reviewing a changed golden (only if Step 2 or 3 shows a diff).** A diff means the old scanner had dropped a real match on that replay. Accept it only if every point holds:
  1. **Additions only.** Because no registered query has a border and none is a suffix of another, the old match set is a subset of the new one. A JSON entry or field that disappears, or two events at the same timestamp that swap order, is a regression. Stop and investigate.
  2. **Find the new match.** Put a temporary `_test.go` file (do not commit it) next to `reader.go` that holds a verbatim copy of bd6ccbe's `worker` plus block split, runs it and `findQueries` on `r.b` after `NewReader` for the changed fixture, and prints the matches only in the new set (offset, pattern hex, and the 16 bytes around it). Classify each one: a boundary miss if the match spans `offset + k*blockSize` / `+1`, and otherwise a partial-match miss.
  3. **Map it to the JSON delta.** The pattern names the listener (for example `59 34 E5 8B 04` → `readMatchFeedback` → one new `matchFeedback` entry; `1F 07 EF C9` → `readTime` → a changed `time`/`timeInSeconds` on later events; `1C D2 B1 9D` → `readScoreboardKills`). Every JSON delta must be explained by one of the new matches.
  4. **Check plausibility against independent data.** A new kill must have a known killer and target, a timestamp inside the round, and must not duplicate an existing entry, and the kill-feed totals must agree with the scoreboard totals (`stats` / `scoreboard`) at least as well as before. A new time sample must keep the clock monotonic.
  5. Record each accepted change (fixture, offset, pattern, cause, JSON delta) in the PR description. Then commit the regenerated golden in its own commit: `test(golden): <fixture> gains <event> the old scanner dropped at <offset>`.

### Task 5: Ship

- [ ] Open the PR with `/ship`. The title is `fix(reader): find every pattern occurrence (bytes.Index per query)`. The body holds the reproduction table from Task 1, the corpus parity result, the benchmark table, and credit to Animuxs `2ad47cd`. Mention the merge-order notes for items 01b, 03 and 06.

## Acceptance

- `go test ./dissect -run 'TestRead_|TestFindQueries' -count=1 -race` is red on bd6ccbe (5 FAIL) and green on the branch.
- `go test -race ./...` is green, and `go test ./dissect/test -run TestReader_Read -update` leaves `git status` clean.
- The CLI JSON for the five user matches is byte-identical before and after.
- `BenchmarkReadScan` on a Y11 round is at least 2 times faster than bd6ccbe when measured interleaved.
- `dissect/reader.go` has no `worker` func, no `numWorkers`, and no `sort` import.

## Risks / open questions

- **Cost grows with listeners.** Each added query is one more pass, about 10 ms per 70 MB round. At about 40 or more queries, revisit approach C (a 2-byte prefix table or Aho-Corasick). It is not needed now (17 to 20 queries).
- **A future periodic pattern** (one with a border, such as `AB AB`) would make "non-overlapping" and "every occurrence" differ. The reference test pins the non-overlapping choice. If such a pattern is ever added, decide explicitly.
- **Goroutine fan-out under R6Stats concurrency.** About 20 short-lived goroutines per round is negligible, and GOMAXPROCS bounds the real parallelism.
- **Benchmark noise.** The planning numbers were taken under heavy machine load (load average 100 to 400). The ratios held in every interleaved pair, but re-measure on a quiet machine for the PR.
- **Rare real-world hit.** The fix was not observed changing any real output. If a future corpus does show a golden change, Task 4 Step 4 is the gate.
