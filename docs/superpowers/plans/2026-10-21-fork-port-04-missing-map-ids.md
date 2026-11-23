# Fork port 04: missing map IDs (design + implementation plan)

> **Issue:** #17 · prestonwillis10/r6-dissect · fork survey: `docs/research/2026-10-21-fork-survey.md`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `Map(worldid).String()` (and so the CLI `--info` line and the JSON `header.map.name`) names Villa, Tower and Coastline (modernized versions) and Calypso Casino instead of printing `Map(<id>)`. An ID that is still unknown keeps its current output and also logs a warning.

**Architecture:** Add four `Map` constants to `dissect/header.go`, regenerate `dissect/map_string.go` with `stringer -type=Map`, add a `Map.Known()` helper, and log a warning in `readHeader` when the worldid is not a known constant. Nothing changes in the JSON shape.

**Tech Stack:** Go (module `github.com/redraskal/r6-dissect`), `golang.org/x/tools/cmd/stringer` v0.52.0 (installed at `~/go/bin/stringer`), zerolog.

**Spec:** this document (the design sections come before the plan).

Verified against: `origin/main` bd6ccbeb7b46af99638df5d650187069e849f925.

---

## Summary

Our `Map` enum has no constants for four world IDs that other forks added:

| worldid | name (this plan) | where it comes from |
|---|---|---|
| 409325881472 | `VillaY10` | nicktstewart `f12f424`, Animuxs `bf687ca`, atrejo26, upstream PRs #129/#131 |
| 423767322185 | `TowerY10` | Animuxs `8dc0307`, atrejo26, upstream PRs #129/#131 |
| 436375283234 | `CoastlineY10` | nicktstewart `f12f424`, Animuxs `8dc0307`, atrejo26, upstream PRs #129/#131 |
| 419965653950 | `CalypsoCasino` | nicktstewart `9061fc8` only |

A replay on one of these maps parses fine today, but its map shows as `Map(409325881472)` and so on. This is a pure table addition, plus a warning log so the next missing map shows up in worker logs instead of in the UI. No game-mode or match-type constant is missing on any current fork branch (details under Source). The corpus has no replay on any of the four maps, so the IDs are taken from the forks without replay confirmation (see Risks).

## Source

The constant sets were collected from every ref in the fork mirror (91 remote refs) with:

```bash
for r in $(git for-each-ref --format='%(refname:short)' refs/remotes); do
  git show $r:dissect/header.go 2>/dev/null \
    | grep -E '^\s+\w+\s+(Map|GameMode|MatchType)\s+= ' | awk -v r=$r '{print $2, $4, $1, r}'
done
```

That output was then diffed against `origin/main` on `(type, value)`.

**Map values that are in some fork but not in ours** (the number is how many refs carry it):

```
22 Map 409325881472 VillaY10
20 Map 423767322185 TowerY10
22 Map 436375283234 CoastlineY10
 1 Map 419965653950 CalypsoCasino
```

Every ref that carries one of these IDs gives it the same name. No ref gives a different value for a name we already have.

- **nicktstewart `f12f424`** ("[Add] CoastlineY10 VillaY10", 2026-03-22): adds `VillaY10 Map = 409325881472` and `CoastlineY10 Map = 436375283234` to `dissect/header.go` and regenerates `dissect/map_string.go`. It has no test.
- **nicktstewart `9061fc8`** ("[Update] Y11S2 Calypso Casino added", 2026-06-13): adds `CalypsoCasino Map = 419965653950` and also `Y11S2 int = 9718747` in `dissect/version.go`. The version constant is a separate concern and is not ported here. nicktstewart's `codex-docs/Y11S2_format_overhaul.md` mentions a CalypsoCasino match (`test-files/Match-2026-07-06_20-13-55-24040`) that was reviewed by hand, but that replay is not committed. Note that nicktstewart/main has **no** `TowerY10`.
- **Animuxs `bf687ca`** (julio208920, 2026-09-25, a 32-file "Pending changes exported from your codespace" commit) adds `VillaY10`. **Animuxs `8dc0307`** (Rey T, 2026-09-25, "...fix Y11S3 replay parsing", 23 files) adds `TowerY10`, `FortressY10` and `CoastlineY10`. The same constants are in atrejo26/main and in upstream PRs #129 (`fccb1f6`) and #131 (`0c7c0b0`). These are large, mixed commits, so only the constant lines are relevant. No test pins the IDs.
- **ArnaudCabello/main** (`dfdc96d`, 2026-04-01) names 418126004176 `ConsulateY11`. Ours, and upstream's, call it `ConsulateY10`, from upstream `1962829` (Scott Liu, 2025-09-12, "feat: add Y10S3 modernized maps"). The ArnaudCabello rename sits inside a position-tracking/web-viewer commit and gives no evidence for the name.
- Old upstream branches `*/feat/imhex` name 2609221242 `Consulate`, where we have `ConsulateY7`. That is a 2023 name that upstream main later changed, so ignore it.

Flaws in the sources: none of them adds a test, and none commits a replay on the new maps. Animuxs mixes the constants into very large unrelated commits, so cherry-picking is not an option. The constants have to be retyped, which is trivial.

**GameMode:** no fork has a value that we lack.
**MatchType:** only the 2023 `*/feat/imhex` and `*/feat/loadout` branches differ (`CustomGameLocal = 7`, `CustomGameOnline = 8`, `Unranked = 12`). They predate upstream main's renumbering, which we have (`CustomGameLocal = 3`, `Standard = 8`, `Unranked = 9`). There is nothing to port. One side finding, out of scope here: three Y8S1 build 7457292 replays committed on `Animuxs-lab/feat/loadout` (`dissect/test/data/replays/valid/Y8S1/LIQ_MAG/Match-2023-03-25_14-24-02-118-R0{1,6,8}.rec`) parse with `Match Type: MatchType(7)` on our parser.
**Operator:** no fork's `dissect/header.go` has an `Operator` value that we lack.

## Current behaviour

`dissect/header.go` at bd6ccbe:

- the type: `type Map int`
- generation: `//go:generate stringer -type=Map`
- the last constant in the block: `FortressY10        Map = 398899676157` (no Villa/Tower/Coastline Y10 constants, no CalypsoCasino)
- parsing: `n, err = strconv.Atoi(props["worldid"])` … `h.Map = Map(n)`. There is no check of the value and no log.
- JSON: `func (i Map) MarshalJSON()` emits `stringerIntMarshal{Name: i.String(), ID: int(i)}`.

`dissect/map_string.go` (generated): `func (i Map) String() string` returns `"Map(" + strconv.FormatInt(int64(i), 10) + ")"` when the value is not in `_Map_map`.

`dissect/print.go`: `log.Info().Msgf("Map:              %s", r.Header.Map)`.

**Reproduction (ran at bd6ccbe, in a scratch clone):**

```
$ go test -run TestMapNames ./dissect     # the test from Task 1, below
--- FAIL: TestMapNames (0.00s)
    map_test.go:25: Map(409325881472).String() = "Map(409325881472)", want "VillaY10"
    map_test.go:39: Map(409325881472) JSON = {"name":"Map(409325881472)","id":409325881472}, want name "VillaY10" id 409325881472
    map_test.go:25: Map(423767322185).String() = "Map(423767322185)", want "TowerY10"
    ...
    map_test.go:25: Map(419965653950).String() = "Map(419965653950)", want "CalypsoCasino"
FAIL
```

**What the CLI and JSON emit today for an unknown map:** `r6-dissect --info` prints `Map:              Map(409325881472)`, and the JSON has `"map":{"name":"Map(409325881472)","id":409325881472}`. Nothing is logged.

**Corpus scan (ran `r6-dissect --info` from bd6ccbe on every replay available):**
- 53 files: the fixtures on main (Y8S1, Y8S2, Y9S1, Y11S2) plus the user's real Y11S2 (`Match-2026-08-25_*`) and Y11S3 (`Match-2026-10-18_*`) matches. Maps found: Chalet, House, Bank, Oregon, ClubHouse, ClubHouseY10, FortressY10, LairY10, NighthavenLabsY10.
- 66 unique `.rec` blobs committed on any fork ref (ArnaudCabello/main 25, nicktstewart/Y11S1-changes 21, Animuxs-lab 15, ElyasYassin, including the Y11S3 fixtures of `track/rec-timeline`). Maps found: ClubHouseY10, FortressY10, KafeDostoyevskyY10, BorderY10, ChaletY10, ConsulateY10, NighthavenLabsY10, LairY10, plus Y8/Y9 maps.
- **No replay carries 409325881472, 423767322185, 436375283234 or 419965653950.** No replay prints an unknown `Map(...)`.

**R6Stats consumer, as deployed (`/home/prest/R6Stats-deploy`, 826dc36):**
- `api/r6stats/services/parser_contract.py`: `map_name = _name_of(raw["map"])` raises `ParserContractError("map.name")` when the name is empty. A `Map(<id>)` name is accepted and stored as the game's map.
- `web/src/components/series/seriesLabel.ts`: `const YEAR_TAG = /Y\d+(S\d+)?$/`, and `mapChipText` appends `game_version` when the name has no year tag. So an unknown map today renders as `Map(409325881472)Y11S2`. After the fix it renders `VillaY10`, and `CalypsoCasino` renders `CalypsoCasinoY11S2`.
- `api/r6stats/services/matching.py`: `same_map = game.map is None or game.map.lower() == segment.map.lower()`. The map name is used as an identity when segments are matched to games.

## Approaches considered

1. **Add the four constants and regenerate stringer; keep the unknown fallback unchanged.** This is the smallest change and copies the forks exactly. Its weakness: the next new map is again noticed only in the UI.
2. **Approach 1, plus a `Known()` helper and a `log.Warn` in `readHeader` for an unknown worldid (chosen).** It costs about 8 more lines. The worker's stderr then names the missing ID, and `--info` shows it too. This follows the existing precedent in the same file: `log.Warn()...Msg("unknown operator, cannot derive team role from it")`.
3. **Change the unknown JSON name** to `""`, `"Unknown"` or `null`. Rejected. An empty name makes R6Stats reject the whole upload (`ParserContractError("map.name")`). `"Unknown"` would merge every unknown map into one identity in `matching.py` and in the per-map stats. The current `Map(<id>)` name is unique for each ID and keeps the ID, which is the safer failure.
4. **Load the map table from data (JSON or Ubisoft) at runtime**, the way `genops.go` fetches operators. Rejected. No public Ubisoft source for world IDs is known, it adds a network or data dependency, and the table changes a few times a year.

## Decision and rationale

**Decisions (autonomous; alternatives recorded):**

- **D1, the constants:** `VillaY10 = 409325881472`, `TowerY10 = 423767322185`, `CoastlineY10 = 436375283234`, `CalypsoCasino = 419965653950`, appended after `FortressY10` in the `const` block of `dissect/header.go`. All forks that carry an ID agree on its value.
- **D2, the naming convention:** the `Y10` suffix marks the **"Siege X" modernized version of an existing map** (the generation that started in Y10S2), not the season it shipped in. Precedent: upstream gives `Y10` to both the Y10S2 batch (`f38c5c5` "feat: Y10S2 maps") and the Y10S3 batch (`1962829` "feat: add Y10S3 modernized maps"), and to `FortressY10`, which was added 2025-12 (Y10S4). So `VillaY10`/`TowerY10`/`CoastlineY10` follow the convention even if those reworks shipped in Y11, and they match the forks and upstream PRs #129/#131 byte for byte, which keeps our JSON names in parity if upstream merges them. A brand-new map has no suffix (`CalypsoCasino`, like `EmeraldPlains`). Alternatives: season-accurate suffixes (`VillaY11`). Rejected: we cannot confirm the ship season from data, and the names would diverge from every other fork.
- **D3, Consulate:** keep `ConsulateY10` for 418126004176. It comes from upstream's "Y10S3 modernized maps" commit, which fits D2. ArnaudCabello's `ConsulateY11` has no evidence behind it and would rename a value that R6Stats has already stored (`game.map`), which would break `matching.py` name equality between old and new segments. Alternative: rename. Rejected for those reasons.
- **D4, unknown maps:** keep the JSON and CLI output (`{"name":"Map(<id>)","id":<id>}`) unchanged. Add `func (i Map) Known() bool` and a `log.Warn().Int("worldid", n).Msg("unknown map id; add it to the Map constants in header.go")` in `readHeader`. `Known` tests `!strings.HasPrefix(i.String(), "Map(")` rather than reading the generated `_Map_map`, so the hand-written code does not depend on how stringer lays out its output.
- **D5, no `version.go` change:** nicktstewart's `Y11S2 int = 9718747` is a version-gating concern and belongs with items 02/09, not here.

## Interactions

- **Open branches:** the change was applied with `git apply --3way` on top of `origin/fork/health-dbno` (PR #3, which edits `dissect/header.go` in the `readHeaderMagic`/`readHeader` region, hunk `@@ -343,90 +343,132 @@`) and on top of `origin/track/rec-timeline` (PR #10). It applied cleanly, and `go vet ./dissect && go test ./...` was green on both. PR #12 does not touch `header.go` or `map_string.go`. Whichever lands second needs no rebase work beyond a possible stringer re-run if PR #3 changes the constant block (it does not today).
- **Other items:** item 13 (operator fallback from header roleName) and item 02 (Y11S2 build markers) also edit `dissect/header.go` (the Operator block and the version/player code). Those are textually separate regions. If two land together and `map_string.go` conflicts, re-run stringer (Task 1, Step 3) rather than resolving it by hand. No other item touches `Map`.
- **JSON / R6Stats:** additive only. A replay on the four maps now emits a real name, and every other replay is unchanged. Games already stored in R6Stats with a `Map(<id>)` map name keep it until they are re-parsed. Out of scope here: if any exist in production, the R6Stats re-parse path, or a one-off `UPDATE games SET map=... WHERE map='Map(409325881472)'`, handles them. The web chip already handles `...Y10` names (`seriesLabel.test.ts`: `mapChipText({ map: "NighthavenLabsY10", ... })`).
- **Golden tests:** no churn. No fixture is on these maps; `go test ./dissect/test -run TestReader_Read` stays green without `-update`.

---

## Implementation plan

I'm using the writing-plans skill to create the implementation plan.

### Global Constraints

- Base: `origin/main` bd6ccbe. Branch name: `fork-port/04-missing-map-ids`.
- Go: `export PATH=$HOME/.local/go/bin:$PATH`.
- Regenerate `dissect/map_string.go` **only** with stringer, never by hand: `cd dissect && ~/go/bin/stringer -type=Map` (golang.org/x/tools/cmd/stringer v0.52.0, which is what produced the current file; with no header change it reproduces the file byte for byte). If `~/go/bin/stringer` is missing: `go install golang.org/x/tools/cmd/stringer@v0.52.0`.
- **Do not run `go generate ./...`.** `dissect/header.go` also has `//go:generate go run ./genops.go -type=Operator ...`, which fetches operator data from Ubisoft over the network (`ubi.GetOperatorMap()`) and rewrites the operator files. Run only the `Map` stringer line.
- Constant names: exactly `VillaY10`, `TowerY10`, `CoastlineY10`, `CalypsoCasino`. The values are in D1.
- Commit labels: `feat(dissect): ...` and `test(dissect): ...`. Never use `docs(...)` for a commit that changes code.

### Review Focus

1. An unknown worldid must still parse, with JSON `{"name":"Map(<id>)","id":<id>}` and a non-empty name. R6Stats rejects an empty name. Covered by `TestMapUnknown`.
2. JSON round-trip of a new map (`MarshalJSON` → `UnmarshalJSON`) keeps both name and id. Covered by `TestMapNames`.
3. `map_string.go` is consistent with `header.go`: if a value is mistyped or stringer is not re-run, the generated `_ = x[VillaY10-409325881472]` guard fails the build, and `TestMapNames` catches a wrong name.
4. Existing names (`ConsulateY10`, `FortressY10`) must not change. `TestMapNames` pins them.
5. A known map must not warn. Covered by the manual `--info` run in Task 2, Step 5.

### File structure

- Modify `dissect/header.go`: the four constants in the `Map` const block; the `Known` method beside `func (i Map) MarshalJSON`; the warning after `h.Map = Map(n)`; the `"strings"` import.
- Regenerate `dissect/map_string.go` (stringer).
- Create `dissect/map_test.go` (package `dissect`, internal test, next to `defuse_test.go`).

### Task 1: Map constants for Villa/Tower/Coastline Y10 and Calypso Casino

**Files:**
- Modify: `dissect/header.go` (the `const` block, after `FortressY10        Map = 398899676157`)
- Regenerate: `dissect/map_string.go`
- Create: `dissect/map_test.go`

**Interfaces:**
- Produces: `VillaY10`, `TowerY10`, `CoastlineY10`, `CalypsoCasino` of type `Map`.

- [ ] **Step 1: Write the failing test** in `dissect/map_test.go` (package `dissect`, imports `encoding/json`, `testing`). Use raw ints, not the new constants, so the test fails instead of breaking the build:

```go
// TestMapNames pins worldid -> name for the modernized and new maps.
func TestMapNames(t *testing.T) {
	cases := []struct {
		id   int
		name string
	}{
		{398899676157, "FortressY10"},
		{418126004176, "ConsulateY10"},
		{409325881472, "VillaY10"},
		{423767322185, "TowerY10"},
		{436375283234, "CoastlineY10"},
		{419965653950, "CalypsoCasino"},
	}
	for _, c := range cases {
		m := Map(c.id)
		// assert m.String() == c.name
		// json.Marshal(m), unmarshal into struct{Name string `json:"name"`; ID int `json:"id"`},
		// assert Name == c.name && ID == c.id
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run TestMapNames ./dissect`
Expected: FAIL, with `Map(409325881472).String() = "Map(409325881472)", want "VillaY10"` and the same for the other three new IDs. The FortressY10 and ConsulateY10 rows pass.

- [ ] **Step 3: Add the constants and regenerate**

In `dissect/header.go`, after `FortressY10        Map = 398899676157`, add the four lines from D1 in this order: VillaY10, TowerY10, CoastlineY10, CalypsoCasino. Align them with gofmt. Then:

```bash
cd dissect && ~/go/bin/stringer -type=Map && cd .. && gofmt -l dissect
```

Expected: `gofmt -l` prints nothing. `git diff --stat` shows `dissect/header.go | 4 ++++` and `dissect/map_string.go | 20 ++++++++++++------` (four new `_ = x[...]` guards, a longer `_Map_name`, and re-indexed `_Map_map` entries).

- [ ] **Step 4: Run the tests**

Run: `go test -run TestMapNames -v ./dissect && go test ./...`
Expected: `--- PASS: TestMapNames`, and every package `ok`. `dissect/test` (golden `TestReader_Read`) passes without `-update`.

- [ ] **Step 5: Commit**

```bash
git add dissect/header.go dissect/map_string.go dissect/map_test.go
git commit -m "feat(dissect): add VillaY10, TowerY10, CoastlineY10 and CalypsoCasino map IDs

Ported from nicktstewart f12f424/9061fc8 and Animuxs bf687ca/8dc0307."
```

### Task 2: Warn on an unknown map ID

**Files:**
- Modify: `dissect/header.go` (import block; new method beside `func (i Map) MarshalJSON`; `readHeader` after `h.Map = Map(n)`)
- Test: `dissect/map_test.go`

**Interfaces:**
- Consumes: the `Map` type and its stringer `String()`.
- Produces: `func (i Map) Known() bool`. It is true when `i` is a declared constant, and is computed as `!strings.HasPrefix(i.String(), "Map(")`.

- [ ] **Step 1: Write the failing test** `TestMapUnknown` in `dissect/map_test.go`:
  - `Map(1).Known()` is `false`
  - `json.Marshal(Map(1))` equals exactly `{"name":"Map(1)","id":1}`
  - `Map(398899676157).Known()` is `true`

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run TestMapUnknown ./dissect`
Expected: build failure `Map(1).Known undefined (type Map has no field or method Known)`.

- [ ] **Step 3: Implement**
  - Add `"strings"` to the imports in `dissect/header.go`.
  - Add `Known` with a doc comment saying that an unknown map still parses and its name prints as `Map(<id>)`, which is stringer's fallback.
  - In `readHeader`, right after `h.Map = Map(n)`:

```go
	if !h.Map.Known() {
		log.Warn().Int("worldid", n).Msg("unknown map id; add it to the Map constants in header.go")
	}
```

- [ ] **Step 4: Run all tests and vet**

Run: `go vet ./... && go test ./...`
Expected: every package `ok`, and `TestMapUnknown` and `TestMapNames` pass.

- [ ] **Step 5: Confirm that a known map does not warn**

Run: `go build -o /tmp/r6d . && /tmp/r6d --info "$(find dissect/test/data/replays/valid/Y11S2 -name '*.rec' | head -1)"`
Expected: a `Map:              NighthavenLabsY10` (or LairY10) line, and no `WRN` line.

- [ ] **Step 6: Commit**

```bash
git add dissect/header.go dissect/map_test.go
git commit -m "feat(dissect): warn when the replay header carries an unknown map id"
```

## Acceptance

- `go test -run 'TestMapNames|TestMapUnknown' -v ./dissect` is green. At bd6ccbe, `TestMapNames` fails on the four new IDs.
- `go test ./...` and `go vet ./...` are green, with no golden `-update`.
- Running `cd dissect && ~/go/bin/stringer -type=Map` again on the finished branch gives a `git diff` with no changes (the generated file is fresh).
- When a replay on Villa, Tower, Coastline or Calypso Casino becomes available, `r6-dissect --info <file>` prints the named map. This is the replay validation still owed (see Risks).

## Risks / open questions

- **No replay confirms the IDs (likely correct, but unverified).** No replay in the 53-file corpus or in the 66 fork-committed `.rec` blobs is on these maps. The evidence is that 2 to 3 independent fork authors agree. For Calypso Casino it is one author (nicktstewart), who describes reviewing a real Calypso match by hand. A wrong ID would mislabel only that one ID's map, and the warning in D4 does not help in that case. Mitigation: the first time the user plays one of these maps, run `--info` on it and compare.
- **Suffix semantics (guess).** D2 reads `Y10` as a generation marker. If the user would rather have season-accurate names, renaming later changes stored R6Stats `game.map` values. That is why D3 avoids a rename now.
- **Stored `Map(<id>)` rows in R6Stats** from replays uploaded before the fix keep the numeric name until they are re-parsed. This is an R6Stats follow-up and is not tracked by this issue.
- **Side findings for other items, not this issue:** a `MatchType(7)` is seen on Y8S1 build 7457292 replays (Animuxs-lab/feat/loadout `LIQ_MAG`). No operator-ID gaps between the forks' `header.go` and ours.
