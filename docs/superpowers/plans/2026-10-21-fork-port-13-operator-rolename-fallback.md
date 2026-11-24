# Fork port 13: operator name fallback from the header roleName (design + implementation plan)

> **Issue:** #27 (13b: no issue, see Decisions) · prestonwillis10/r6-dissect · fork survey: `docs/research/2026-10-21-fork-survey.md`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a replay contains an operator ID that is not in our operator table (a new season's operator), the JSON names it from the replay header's `roleName` (`"NOOR"` gives `{"name":"Noor","id":<id>}`) instead of `Operator(<id>)`. Known IDs keep their current output exactly.

**Architecture:** A new file `dissect/operator_fallback.go` holds a pure name-derivation function, `Operator.Known()`, `Player.OperatorName()`, and custom `MarshalJSON` methods for `Player` and `MatchUpdate`. The custom methods override only the `operator` key. A post-pass at the end of `Reader.Read` names `OperatorSwap` events to an unknown ID when that ID is the player's final operator. `PlayerRoundStats.Operator`, which feeds the Excel output, uses `Player.OperatorName()`. A per-player warning names each unknown ID so that adding the constant later is a copy and paste.

**Tech Stack:** Go (module `github.com/redraskal/r6-dissect`), `golang.org/x/text/unicode/norm` (already in `go.mod` as an indirect dependency; this change makes it direct), zerolog.

**Spec:** this document (the design sections come before the plan).

Verified against: `origin/main` bd6ccbeb7b46af99638df5d650187069e849f925.

**Hard dependency:** PR #3 (`fork/health-dbno`) commit `a17b9f4` ("fix: read gmSettings and the last header player on Y11S2+"). Without it, the 10th header player of every Y11S2+ replay has no `roleName`, so the fallback cannot name that player's operator. Implement this plan on `main` **after PR #3 has merged**. See Interactions.

---

## Summary

- Every season adds one or two operators. Until someone adds the new ID to the `Operator` constants in `dissect/header.go` and regenerates the stringer, our JSON prints the operator as `{"name":"Operator(<id>)","id":<id>}`. R6Stats shows that string as the operator name and aggregates operator stats under it (`api/r6stats/services/parser_contract.py`: `operator=_name_of(raw.get("operator"))`).
- The replay header already carries each player's `rolename`. Across the whole corpus it is the **English, upper-case display name of the operator the player ended the round on** ("ACE", "SOLID SNAKE", "CAPITÃO", "JÄGER"). It is not localized, and it is not the attack/defense role. In 470 of 470 Y11S2/Y11S3 player rows it agrees with the operator our stream parser found independently, once the 10th header player is read (PR #3). That includes 41 rounds where a player swapped operators during prep: `roleName` named the final pick every time.
- This plan uses that name when, and only when, the ID is unknown. The name is converted to the constant style ("SOLID SNAKE" becomes "SolidSnake", "CAPITÃO" becomes "Capitao"). That way, when the constant is added later, the name a consumer sees does not change and R6Stats stats do not split.
- **The attack/defense role needs no new inference.** Team roles already come from any player whose operator is known (`deriveTeamRoles`), and the site/spawn code already falls back to the team role. No per-operator role is guessed. nicktstewart's forced "Y11S3 = Defense" guess was wrong.
- **The operators CI check is not broken** (see the evaluation in Approaches). No work is needed there.

## Source

- **nicktstewart `031e187`** ("[Fix] Operator detection", 2026-07-10), files `dissect/operator_fallback.go`, `dissect/header.go`, `dissect/player.go`, `dissect/header_integration_test.go`:
  - `operatorNameKey` folds accents with a hand-written rune switch, and `operatorByDisplayName` matches the folded `RoleName` against **known** names in `_Operator_map`. `resolveHeaderOperators()` applies it only when `p.Operator == 0`, and only for `CodeVersion >= Y11S2`. So it recovers a known operator for a player whose stream ID was missing. It does nothing for a genuinely new operator, because a new operator's name is not in `_Operator_map` by definition.
  - For unknown IDs it uses season-keyed invented names: `fallbackNameForGameVersion`, with `case "Y11S3": return "Y11S3NewDefender"`. It also forces roles: `RoleForGameVersion`, with `case "Y11S3": return Defense, true`. Both are wrong: the Y11S3 operator was Noor. The parent commit even mapped every unknown Y11S2 ID to `Dokkaebi.String()`.
  - Its `Player.MarshalJSON` re-declares every `Player` field in a shadow struct. That is fragile, because every new `Player` field must be copied there by hand.
  - `player.go`: `if p.Operator != 0 { r.Header.Players[i].Operator = p.Operator }`. Not ported; see Risks (Operator 0).
  - nicktstewart `7b9d9a1` ("[Fix] Header index out of range") also flushes the last header player (`if playerData { players = append(players, currentPlayer) }`). We get the same fix from our own PR #3.
- **Animuxs `scripts/parser.py`**, `_operator_name` (around line 207): `if name.startswith("Operator(") and isinstance(role, str) and role.isascii() and role.strip(): return role.strip().title()`. This is the same idea on the consumer side, and its docstring cites `"NOOR" -> "Noor"`: real-world evidence that `roleName` held the new operator's name before the constant existed. Flaws: `.title()` gives "Solid Snake" rather than our "SolidSnake", and `isascii()` drops "CAPITÃO" and the like.
- **aa-sasha `bfcff23`** ("flush 10th player") and **ItaloRossySantiago `b81ada4`** ("Role() nao trava mais em operador desconhecido"). The second removes a `Role()` panic that we never had: our `Role()` returns `_operatorRoles[i]`, which is the zero value. The same Italo commit makes the claim that the Ubisoft page "foi reconstruida em React e nao responde mais a um cliente HTTP puro" (was rebuilt in React and no longer answers a plain HTTP client), and skips the operator network tests. **That claim is wrong for us**, as shown below.

## Current behaviour (bd6ccbe)

`dissect/header.go` stores the field and never reads it again:

```go
RoleName     string   `json:"roleName,omitempty"`
...
case "rolename":
	currentPlayer.RoleName = v
```

`Operator.MarshalJSON` (`dissect/header.go`) uses only the stringer:

```go
func (i Operator) MarshalJSON() (text []byte, err error) {
	return json.Marshal(stringerIntMarshal{
		Name: i.String(),
```

and `dissect/operator_string.go` falls back to `return "Operator(" + strconv.FormatInt(int64(i), 10) + ")"`. `dissect/stats.go` uses `Operator:  p.Operator.String(),` for `PlayerRoundStats` (Excel output). `deriveTeamRoles` (`dissect/header.go`) warns `"unknown operator, cannot derive team role from it"` and then `continue`s to the next player, so team roles survive as long as one player has a known operator.

**What `roleName` contains** (run with our parser at bd6ccbe over 56 rounds: every `valid/` fixture Y8S1–Y11S2, the 3 Y11S3 fixtures from `track/rec-timeline`, and all of the user's 2026-08-25 (Y11S2) and 2026-10-18 (Y11S3) match folders; 560 player rows):

| game version | rows | roleName present and equal to the parsed operator (accent-folded) | roleName missing | mismatches |
|---|---|---|---|---|
| Y11S2_Alpha04 | 210 | 189 | 21 (the 10th header player, every round) | 0 |
| Y11S3_Alpha04 | 260 | 234 | 26 (the 10th header player, every round) | 0 |
| Y8S1 / Y8S2 / Y9S1 | 90 | 44 | 46 (these headers list only 5 players with roleName) | 0 |

The distinct values are upper-case English names with accents preserved: `ACE … CAPITÃO … JÄGER … NOOR … RAUORA … SKOPÓS, SOLID SNAKE … TUBARÃO …`. With the header loop patched to flush the last player (the same fix as PR #3 `a17b9f4`), Y11S2 and Y11S3 are **470/470 present, 0 mismatches**. The raw header keys of the 10th player (`rolename="GLAZ"` and so on) are present before `teamscore0`/`teamscore1`. Our loop ends at `teamscore1` without appending that player, so `readPlayer` later appends a stream-only copy without `roleName`, `heroName` or `alliance`.

Prep swaps: 150 `OperatorSwap` events in the corpus. In 41 of them the swap target differs from the operator the player ended on (for example `player31 swap-> Deimos … final= Zofia roleName= ZOFIA`). `roleName` always equals the final operator. So the header is written, or finalized, after prep, and `roleName` is the round's operator, not the first pick.

**Reproduction of the symptom.** In a scratch clone at bd6ccbe, delete Deimos (`374667787816`) from `_Operator_map` (`dissect/operator_string.go`) and `_operatorRoles` (`dissect/operator_roles.go`) to simulate a new operator. Then parse `Y11S3/custom_fortress_plant.rec`:

```
[('player41', {'name': 'Operator(374667787816)', 'id': 374667787816}, 'DEIMOS')]   # player, operator, roleName
['Attack', 'Defense']                                                             # team roles still derived
```

With the prototype from this plan applied, the same run prints `{'name': 'Deimos', 'id': 374667787816}`.

**Acceptance test, red today** (`dissect/operator_fallback_test.go`, exported API only):

```
--- FAIL: TestPlayerJSON_UnknownOperatorNamedFromRoleName (0.00s)
    got {"username":"u","teamIndex":0,"operator":{"name":"Operator(123)","id":123},"alliance":0,"roleName":"NOOR"}
```

## Approaches considered

1. **roleName name fallback in the parser (chosen).** Name unknown IDs from the header, convert to constant style, and leave known IDs alone. It needs no network access and works the day a season ships, and R6Stats needs no change. Cost: two small `MarshalJSON` overrides. Risk: a non-English client could write a localized `roleName`. Every corpus replay is English (see Risks).
2. **Map roleName onto known constants only (nicktstewart).** This helps only when Ubisoft re-IDs an existing operator, or when the stream ID is 0. It does nothing for the real problem, a new operator. Rejected as the main mechanism. Approach 1 already covers the re-ID case by name: a re-ID'd Deimos would print "Deimos" with its new ID.
3. **Consumer-side fallback in R6Stats (Animuxs style).** R6Stats would check `name.startswith("Operator(")` and use `roleName`. That leaves every other consumer of the CLI with the bad name, and it would duplicate the name-style rules in Python. Rejected. R6Stats gets the fix for free through the unchanged `operator.name` key.
4. **Make the operators CI check catch new operators sooner (the survey's alternative).** It is already in place and working:
   - `.github/workflows/operators.yaml` runs `go test -v -run 'Test_operators' ./dissect/test/` weekly (`cron: "0 12 * * 1"`) with `R6_DISSECT_NETWORK_TESTS: "1"`.
   - Run locally at bd6ccbe today: `--- PASS: Test_operatorsMissing (0.96s)`, `--- PASS: Test_operatorsRedundant (0.28s)`. The fetched page (330 KB) still contains `window.__PRELOADED_STATE__`, with 78 slugs.
   - `gh run list -R prestonwillis10/r6-dissect --workflow operators.yaml`: `completed success … workflow_dispatch … 2026-10-22T02:52:03Z`.
   - The break Italo saw was an empty external `<script>` before the data script, not React. We already fixed it in `2102bce` ("fix(ubi): skip non-data script tags when scraping operators", 2026-10-18).
   - Even working perfectly, the check only says *a name is missing*. The ID has to come from a replay. So the check complements approach 1: the CI tells you to add "Noor", and the warning from approach 1 gives you `id=… roleName=NOOR`. **No issue for the CI check.**
5. **Verbatim roleName (`"NOOR"`) or `.title()` (`"Solid Snake"`) as the fallback name.** Rejected. Either one changes the name when the constant is later added, which splits R6Stats' per-operator aggregates (`operator_rounds` keyed by name) across the two parser versions.

## Decisions

- **D1, which name.** Use the constant-style conversion of `roleName`: NFKD-decompose and drop combining marks, map `ø`/`Ø` to `o`, map `ß` to `ss`, treat any non-letter/digit as a word break, and title-case each word ("SOLID SNAKE" → "SolidSnake"). Verified in the corpus: 64 of the 65 distinct roleNames convert to exactly our constant name. The exception is `IQ`, whose constant is all caps. That only matters if a future operator's name is an acronym. Accepted.
- **D2, when.** Only when `!p.Operator.Known()` and the converted name is non-empty. A known ID always uses the table, even if `roleName` disagrees. The table is authoritative, and `roleName` agreed 100% anyway.
- **D3, role.** No new role inference. Team roles already come from other players (`deriveTeamRoles`), and `dissect/site.go` already sets spawns by `r.Header.Teams[p.TeamIndex].Role == Defense`. `Player` JSON has no per-player role field. No per-season role guessing (nicktstewart's mistake).
- **D4, keep the raw ID.** `operator.id` stays the replay's ID, and `Player.Operator` is not rewritten. Consumers that key by ID keep working, and the warning shows the ID to add.
- **D5, JSON shape.** Override only the `operator` key, using an embedded alias type (`type plain Player`) plus an outer `Operator` field that shadows it. No field list is duplicated. Side effect: the `operator` key moves to the end of the player and event objects. The JSON stays semantically identical (checked with `json.load` equality over all 56 rounds), and the golden `.rec.json` files are compared after unmarshalling, so there is no golden churn.
- **D6, swap events.** Name an `OperatorSwap` to an unknown ID only when it equals that player's final `Operator`, because `roleName` names the final pick. An earlier swap to a different unknown ID stays `Operator(<id>)`. This runs as a post-pass in `Reader.Read`, because the final operator is known only at the end.
- **D7, maintainer signal.** Log one warning per unknown-operator player: `log.Warn().Uint64("op", …).Str("roleName", …).Str("username", …).Msg("operator id not in the operator table; add it to dissect/header.go")`. This goes in `deriveTeamRoles`, before the role loop.
- **D8, dependency.** Use `golang.org/x/text/unicode/norm` rather than a hand-written accent table. It is already in the module graph (`golang.org/x/text v0.43.0 // indirect`), and `go mod tidy` moves it to the direct block.
- **D9, scope.** One mechanism: the name fallback. The 10th-player flush is PR #3's. Operator-0 header players are out of scope (Risks).

## Interactions

- **PR #3 `fork/health-dbno`:** it rewrites `readHeader` into `splitHeaderEntries`, which ends with `if playerData { players = append(players, currentPlayer) }` (commit `a17b9f4`). That is the prerequisite for full coverage. Its `deriveTeamRoles` keeps the same warning line (`dissect/header.go:610` on that branch), which is where Task 3 edits. Its `dissect/stats.go` changes do not touch the `Operator:` line. **Order: PR #3 first, then this.**
- **`track/rec-timeline` (PR #10):** `dissect/feedback.go` gains `offset`/`derived` unexported fields on `MatchUpdate` and `r.addUpdate(u)`. Adding one more unexported field (`operatorName`) and a `MatchUpdate.MarshalJSON` merges cleanly. Unexported fields are not marshalled either way. If PR #10 lands first, the post-pass still runs at the end of `Read`.
- **PR #12 (ElyasYassin):** no overlap found in `header.go`, `player.go`, `feedback.go` or `stats.go` in the `MarshalJSON` sense.
- **Item 02 (Y11S2 build 9734089 markers):** aa-sasha's branch also flushes the 10th player (`bfcff23`). If item 02 ports that, it duplicates PR #3. Whoever lands second should drop it.
- **Item 03 (listener EOF aborts the round):** today a listener error `return`s from `Read` before the end-of-read block. The swap post-pass is placed after that block, so it is skipped on that error path, just like `roundEnd`. Item 03's fix makes it run.
- **Item 11 (weapon per kill), item 12 (damage taken):** no interaction.
- **JSON / R6Stats:** `operator.name` changes only for unknown IDs, from `Operator(<id>)` to the converted name. R6Stats needs no change: `_name_of` reads `name`. The `operator` key order inside `players[]` and `matchFeedback[]` changes, which R6Stats does not depend on (it uses `json` parsing). There is no new field.
- **Golden tests:** no churn from this plan. The custom `MarshalJSON` is not used by the deep comparison, and `Player`/`MatchUpdate` round-trip through `Operator.UnmarshalJSON`, which reads only `id`. PR #3 brings its own churn (the 10th player's `heroName`/`alliance`/`roleName`/`roleImage`/`rolePortrait` and header `id`).

## Implementation plan

### File structure

- Create `dissect/operator_fallback.go`: `Operator.Known`, `operatorNameFromRoleName`, `Player.OperatorName`, `Player.MarshalJSON`, `MatchUpdate.MarshalJSON`, `(*Reader).nameUnknownSwapOperators`.
- Create `dissect/operator_fallback_test.go`: unit tests (package `dissect`).
- Create `dissect/test/operator_fallback_test.go`: corpus property test (package `test`).
- Modify `dissect/feedback.go`: add the unexported field `operatorName string` to `MatchUpdate`.
- Modify `dissect/reader.go`: call the post-pass in `Read`.
- Modify `dissect/stats.go`: use `p.OperatorName()`.
- Modify `dissect/header.go`: the warning in `deriveTeamRoles`.
- Modify `go.mod`/`go.sum` via `go mod tidy`.

## Global Constraints

- Base: `main` that contains PR #3 (`git merge-base --is-ancestor a17b9f4 HEAD` exits 0). If not, stop and report.
- `export PATH=$HOME/.local/go/bin:$PATH`. All tests: `go test ./...`.
- No change to the output of known operator IDs. No new JSON keys.
- Commit labels: code-path commits are `feat(...)`/`fix(...)`/`test(...)`, never `docs(...)`.

## Review Focus

1. **A `roleName` that names a different known operator than the unknown ID** (a desync). Expected: we still print the converted name (D2 trusts `roleName` only for unknown IDs). There is no test for a known-name collision. A reviewer should accept this as designed, not "fix" it by matching known names.
2. **Empty or whitespace `roleName`** (Y8–Y9 headers list only 5 players). Expected: `Operator(<id>)`. Pinned by `TestPlayerOperatorName` (no roleName) and `TestOperatorNameFromRoleName` (`"  "`).
3. **`MatchUpdate` with `Operator == 0`** (every non-swap event). Expected: no `operator` key at all, as today with `omitempty`. Pinned in `TestNameUnknownSwapOperators` (the Kill event has no operator).
4. **A double `operator` key or lost fields from the alias shadowing.** Expected: exactly one `operator` key and all other fields present. Pinned in `TestPlayerJSON_UnknownOperatorNamedFromRoleName`.
5. **A non-ASCII name that NFKD cannot decompose** (ø, ß). Pinned in `TestOperatorNameFromRoleName` (`"NØKK"`).

---

### Task 1: name derivation and `Player` JSON

**Files:**
- Create: `dissect/operator_fallback.go`
- Test: `dissect/operator_fallback_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces: `func (i Operator) Known() bool` (`_, ok := _Operator_map[i]`); `func operatorNameFromRoleName(roleName string) string`; `func (p Player) OperatorName() string`; `func (p Player) MarshalJSON() ([]byte, error)`.

- [ ] **Step 1: Write the failing tests** in `dissect/operator_fallback_test.go` (package `dissect`):

```go
func TestOperatorNameFromRoleName(t *testing.T) {
	for in, want := range map[string]string{
		"NOOR": "Noor", "SOLID SNAKE": "SolidSnake", "CAPITÃO": "Capitao", "JÄGER": "Jager",
		"NØKK": "Nokk", "SKOPÓS": "Skopos", "TUBARÃO": "Tubarao", "": "", "  ": "",
	} {
		if got := operatorNameFromRoleName(in); got != want {
			t.Errorf("operatorNameFromRoleName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlayerOperatorName(t *testing.T) {
	// known id ignores roleName; unknown id uses it; unknown id without roleName keeps the stringer
	cases := []struct{ p Player; want string }{
		{Player{Operator: Ace, RoleName: "NOOR"}, "Ace"},
		{Player{Operator: 123, RoleName: "NOOR"}, "Noor"},
		{Player{Operator: 123}, "Operator(123)"},
	}
	// loop: c.p.OperatorName() == c.want
}

func TestPlayerJSON_UnknownOperatorNamedFromRoleName(t *testing.T) {
	b, _ := json.Marshal(Player{Username: "u", Operator: 123, RoleName: "NOOR", TeamIndex: 1})
	// assert: contains `"operator":{"name":"Noor","id":123}`; strings.Count(s, `"operator"`) == 1;
	//         contains `"roleName":"NOOR"` and `"username":"u"`
	// assert: json.Unmarshal(b, &back) gives back.Operator == 123 && back.RoleName == "NOOR"
}
```

- [ ] **Step 2: Run them and confirm they fail.** Run `go test ./dissect -run 'OperatorName|UnknownOperatorNamed'`. Expected: a build failure (`undefined: operatorNameFromRoleName`, `p.OperatorName undefined`). If only the JSON test is kept, it fails with `got {…"operator":{"name":"Operator(123)","id":123}…}`.

- [ ] **Step 3: Implement** in `dissect/operator_fallback.go`:
  - `operatorNameFromRoleName`: iterate `norm.NFKD.String(roleName)`. Skip `unicode.Is(unicode.Mn, r)`. Map `Ø`/`ø` to `o`. Write `ss` for `ß`. Any rune that is not `unicode.IsLetter`/`IsDigit` sets "next is upper" and is dropped. Write letters upper-case at a word start and lower-case otherwise.
  - `OperatorName`: if `!p.Operator.Known()` and the converted name is non-empty, return it. Otherwise return `p.Operator.String()`.
  - `MarshalJSON`, the shadowing shape (D5):

```go
func (p Player) MarshalJSON() ([]byte, error) {
	type plain Player
	return json.Marshal(struct {
		plain
		Operator stringerIntMarshal `json:"operator"`
	}{plain(p), stringerIntMarshal{Name: p.OperatorName(), ID: int(p.Operator)}})
}
```

  - Run `go mod tidy`. `golang.org/x/text` moves to the direct `require` block.

- [ ] **Step 4: Run the tests and confirm they pass.** Run `go test ./dissect -run 'OperatorName|UnknownOperatorNamed' -v`. Expected: 3 PASS. Then run `go test ./...`. Expected: `ok` for both packages; the golden test is unaffected.

- [ ] **Step 5: Commit**: `feat(dissect): name unknown operator ids from the header roleName`.

### Task 2: swap events and round stats

**Files:**
- Modify: `dissect/operator_fallback.go`, `dissect/feedback.go` (`MatchUpdate` struct), `dissect/reader.go` (`Read`), `dissect/stats.go` (`PlayerStats`)
- Test: `dissect/operator_fallback_test.go`

**Interfaces:**
- Consumes: `Operator.Known`, `operatorNameFromRoleName` (Task 1).
- Produces: the `MatchUpdate` field `operatorName string` (unexported, with the comment "names an Operator id newer than the operator table, from the header roleName"); `func (r *Reader) nameUnknownSwapOperators()`; `func (u MatchUpdate) MarshalJSON() ([]byte, error)`.

- [ ] **Step 1: Write the failing test** `TestNameUnknownSwapOperators`:

```go
r := &Reader{
	Header: Header{Players: []Player{{Username: "a", Operator: 123, RoleName: "NOOR"}}},
	MatchFeedback: []MatchUpdate{
		{Type: OperatorSwap, Username: "a", Operator: 456}, // not the final pick
		{Type: OperatorSwap, Username: "a", Operator: 123},
		{Type: OperatorSwap, Username: "a", Operator: Ace},
		{Type: Kill, Username: "a", Target: "b"},
	},
}
r.nameUnknownSwapOperators()
b, _ := json.Marshal(r.MatchFeedback)
// contains `"operator":{"name":"Operator(456)","id":456}`, `"operator":{"name":"Noor","id":123}`,
//          `"operator":{"name":"Ace","id":104189664390}`; strings.Count(s, `"operator"`) == 3
```

Also add `TestPlayerStats_UnknownOperatorName`: `r := &Reader{Header: Header{Players: []Player{{Username: "a", Operator: 123, RoleName: "NOOR"}}}}` and `r.PlayerStats()[0].Operator == "Noor"`.

- [ ] **Step 2: Run them and confirm they fail.** Run `go test ./dissect -run 'NameUnknownSwap|PlayerStats_Unknown'`. Expected: a build failure (`r.nameUnknownSwapOperators undefined`), and then `Operator(123)` for the stats test.

- [ ] **Step 3: Implement**
  - `nameUnknownSwapOperators`: for each `OperatorSwap` event with `Operator != 0 && !Known()`, find the header player with the same `Username` whose `Operator == u.Operator`, then set `r.MatchFeedback[i].operatorName = operatorNameFromRoleName(p.RoleName)` (only if non-empty).
  - `MatchUpdate.MarshalJSON`: the same shadowing as Task 1, but the outer field is `Operator *stringerIntMarshal \`json:"operator,omitempty"\``. It is nil when `u.Operator == 0` (this keeps today's `omitempty`). Its name is `u.operatorName` if set, else `u.Operator.String()`.
  - `dissect/reader.go` `Read`: call `r.nameUnknownSwapOperators()` right after the `if !r.readPartial { … r.applyLoadouts() }` block and before `r.b = nil`.
  - `dissect/stats.go` `PlayerStats`: `Operator:  p.OperatorName(),`.

- [ ] **Step 4: Run the tests and confirm they pass.** Run `go test ./dissect -v -run 'NameUnknownSwap|PlayerStats_Unknown'`. Expected: PASS. Then run `go test ./...`. Expected: `ok`.

- [ ] **Step 5: Confirm the CLI output is unchanged for known operators.** Build `go build -o /tmp/r6d-new .` and the base `/tmp/r6d-base` (the parent commit), then for every `dissect/test/data/replays/valid/*/*.rec` compare `python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))==json.load(open(sys.argv[2])))' a.json b.json`. Expected: `True` for every file (only key order differs).

- [ ] **Step 6: Commit**: `feat(dissect): name swaps to unknown operators and use the fallback in round stats`.

### Task 3: maintainer warning and corpus property test

**Files:**
- Modify: `dissect/header.go` (`deriveTeamRoles`)
- Test: `dissect/test/operator_fallback_test.go`

**Interfaces:**
- Consumes: `Player.OperatorName`, `Operator.Known` (Task 1).

- [ ] **Step 1: Write the test** `TestOperatorFallbackMatchesConstants` (package `test`). Glob `data/replays/valid/*/*.rec`, open each with `dissect.NewReader`, and call `r.Read()`, accepting `dissect.Ok(err)`. For every header player with `RoleName != ""` and `Operator != dissect.IQ`, assert `dissect.Player{Operator: 1, RoleName: p.RoleName}.OperatorName() == p.Operator.String()`. Fail if no player was checked. Log the count. On the prototype it passed: `checked 64 players`, about 10 s. After PR #3 the count is higher, and with the Y11S3 fixtures from PR #10 higher still.

- [ ] **Step 2: Run it.** Run `go test ./dissect/test -run TestOperatorFallbackMatchesConstants -v`. Expected: PASS. It is a regression guard for D1 and passes as soon as Task 1 is in. If it fails, a fixture roleName converts to something other than its constant. Report it; do not special-case it.

- [ ] **Step 3: Add the warning.** At the top of `deriveTeamRoles`, after `r.Header.Players = players`, add a loop over the players: for `p.Operator != Recruit && !p.Operator.Known()`, log `log.Warn().Uint64("op", uint64(p.Operator)).Str("roleName", p.RoleName).Str("name", p.OperatorName()).Str("username", p.Username).Msg("operator id not in the operator table; add it to dissect/header.go")`. Keep the existing role-loop warning unchanged.

- [ ] **Step 4: Run the full suite.** Run `go test ./...` and `go vet ./...`. Expected: `ok`, and vet is clean.

- [ ] **Step 5: Commit**: `test(dissect): pin the roleName fallback to the operator constants`.

## Acceptance

- `go test ./dissect -run TestPlayerJSON_UnknownOperatorNamedFromRoleName` fails at bd6ccbe (verified: `got {…"name":"Operator(123)"…}`) and passes after Task 1.
- `go test ./...` is green, with no golden-file changes from this plan.
- Simulation: in a throwaway clone, delete `374667787816` from `_Operator_map` and `_operatorRoles`, then run the CLI on any Deimos round (for example the Y11S3 `custom_fortress_plant.rec`). `player41` prints `"operator":{"name":"Deimos","id":374667787816}` (`Operator(374667787816)` before), team roles are still `Attack`/`Defense`, and the warning names `op=374667787816 roleName=DEIMOS`.
- The JSON for every fixture is semantically equal before and after (Task 2, Step 5).

## Risks / open questions

- **Localization (open).** Every replay seen, ours and the forks', has English `roleName`s. Operator names are mostly language-invariant proper nouns, but a Russian, Chinese, Japanese or Korean client *might* write a native-script name. NFKD plus title-casing would then produce a non-Latin name, which is still better than `Operator(<id>)` but would split aggregates. Revisit if a non-English replay shows up. A cheap guard would be to require the converted name to be ASCII and fall back to `Operator(<id>)` otherwise. Not done now, because it would drop no real case today.
- **Acronym names.** "IQ"-style names would come out as "Iq" against an eventual `IQ` constant. That is rare, and it is visible in the warning.
- **Header written after prep.** The 41/41 evidence comes from Y11S2/Y11S3 only. If a future build writes the header before prep, `roleName` would name the first pick, and an unknown first pick that was swapped away would be named for a known final op. D2 prevents a wrong name for known IDs. For unknown IDs, the `Player` name could be wrong in that case. Low risk: the evidence shows the header reflects the final pick.
- **Operator-0 header players (out of scope).** `Y8S2/attacker_recorder_site_test.rec` has a header player `MUSTARD` with `roleName "ORYX"` and stream operator 0, whom `deriveTeamRoles` drops with `"operator id was 0, removing from list"`. nicktstewart resolves such players from `roleName` to a known constant. That is a different mechanism (a player missing from the stream), and it would need its own fixture-backed study. Not filed here.
- **Key order.** The `operator` key moves to the end of the `players[]` and `matchFeedback[]` objects. That is harmless for JSON consumers. Anyone diffing raw CLI output byte-for-byte across versions will see it.
