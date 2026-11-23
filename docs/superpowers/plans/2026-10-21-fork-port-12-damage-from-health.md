# Fork port 12: damage taken from the health stream (Implementation Plan)

> **Issue:** #25 · prestonwillis10/r6-dissect · fork survey: `docs/research/2026-10-21-fork-survey.md`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On Y10S4+ replays, every round stats row and every match stats row carries `damageTaken`: the HP each player lost that round, read from the player's own health component.

**Architecture:** One more listener on the health component that PR #3 (`fork/health-dbno`) already links to player slots. It records every current-health value (`25 26 76 C9`). After the round is read, a pure function turns each player's series into damage taken. It starts from the spawn value, sums the drops, skips the 1 HP/s decay of boost health, and adds the remaining HP when the player died. `PlayerStats()` exposes the result as an optional field.

**Tech Stack:** Go 1.x (module `github.com/redraskal/r6-dissect`), existing `Reader.Listen` pattern matching, `go test`.

**Spec:** this document (design and plan in one, per the fork-port brief).

**Base:** `origin/fork/health-dbno` (PR #3, tip `c608cf8`). **Dependency: PR #3 merges first.** That branch forks from `2df8f2c`, not from main `bd6ccbe`, so it needs a rebase onto main before or as it merges. Implement this item on the post-merge main, or stack it on `fork/health-dbno` if PR #3 is still open.

## Global Constraints

- Only Y10S4+ (`r.Header.CodeVersion >= Y10S4`) registers the new listener, the same gate PR #3 uses for `healthLinkIndicator`.
- JSON field name `damageTaken`, an integer, on round `stats[]` and match `stats[]`. It is **omitted** (not `0`) when the round has no health data, so older seasons are unchanged and R6Stats' `_opt_int` reader sees "unknown", not "took no damage".
- No damage-dealt field. The replay has no attacker link (see Decision D3).
- `TestReader_Read` golden JSON compares only `Header` and `MatchFeedback`, so it must not change. Do not run `-update`.
- Fork code (lumina-r6) is reference only. Do not copy it.

## Review Focus

1. **Attackers' stale round-start snapshot.** Each round file starts with every health component's value from the previous round (e.g. `!0.0:66` then the prep reset `p-1.0:125`). Counting from the snapshot invents damage, which is lumina's bug. Pinned by `TestDamageTaken_AttackerStaleSnapshot`.
2. **Kills whose final HP update is never written.** In 61 of the 316 deaths in the user's corpus (19%), the last HP value is above 0. The remainder must still count. Pinned by `TestDamageTaken_DeathWithoutZero`.
3. **Finka / boost health decaying 1 HP/s above spawn HP.** This is not damage. Pinned by `TestDamageTaken_BoostDecay`.
4. **Revive and Rook armor increases** (0→20, +25) must not reduce damage, and later drops still count. Pinned by `TestDamageTaken_ReviveAndRook`.
5. **Defender spawn samples that arrive before the player's slot is known** (10 per round in every replay checked). Samples are stored by health-component id and resolved after the round. Pinned by `TestHealthValue_ResolvedAfterPlayersRead`.

---

## Summary

lumina-r6 decodes the per-entity HP property `25 26 76 C9` and emits damage dealt and taken. Our PR #3 already decodes the same health component with a slot link written by the game itself (no ID-proximity guess). It documents the HP property but does not listen to it. I dumped the HP series of every player in the user's 5 real Y11S2/Y11S3 matches (42 rounds, 420 player-rounds, 1,522 HP records), plus 6 Y11S1 fork matches and the committed Y11S2 fixture.

What the dumps show:
- The stream is clean: every `25 26 76 C9` occurrence parses and belongs to a linked health component.
- It is complete between hits. A sibling "previous health fraction" property matches the previous record in 448 of 451 checks, and the 3 misses are a revive and a Rook pickup.
- Every DBNO has a 0 sample.

**Damage taken is trustworthy** with three rules: a role-aware spawn baseline, boost-decay exclusion, and the death remainder. **Damage dealt is not available:** the health component has no instigator property, and lumina's "credit victim's start HP to the killer" is a proxy, not a measurement. Verdict: FILE, damage taken only.

## Source

- Fork: `lumina-r6/main`. Commits `3c2298a` ("feat(damage): decode HP stream; emit damage dealt/taken per player per round") and `ea662d3` ("feat(damage): HP timeline + per-round damage ranking"). Files: `dissect/health.go` (new), `dissect/reader.go`, `dissect/stats.go`, `dissect/feedback.go`.
- What they did: they listen on `25 26 76 C9`, check the `23 <entity> 00000000` envelope, and store `Min`/`Max`/timeline per raw entity id. They map an entity to the player whose `DissectID` is the nearest one ≥ the entity id within 64 (`playerForEntity`). `DamageTaken = Max - Min`, or `Max` on death. `DamageDealt` credits each killer with the victim's `Max`.
- Flaws, each confirmed on our corpus with our parser:
  1. **Stale snapshot counted.** An attacker whose round file opens with a leftover value (`!0.0:35`), then `0`, then the reset `100`, gets `Max-Min = 100` with no damage taken. Example: Y11S2 match `19-56-00`, round 9, player71 (Dokkaebi): lumina 100, real 25. On the Y11S2 fixture `custom_solid_snake`, player79 gets lumina 110 vs real 100.
  2. **No heals.** `Max-Min` ignores revives and Rook pickups. Example: `20-53-53` round 2, player03 (Ela): 110→82→54→0 (DBNO), revived to 20, Rook armor to 45, then 22 and 0. Real damage is 155; lumina gives 110.
  3. **Boost decay counted as damage** if Max is the boost value. It also misreports a boosted death as `Max`, e.g. 130 instead of 121.
  4. **ID-proximity mapping** is a guess. Ours uses the game's own slot→health link (`1B <slot> 00000000 41 54 DC C4 <health>`).
  5. **Damage dealt** is victim spawn HP per kill. It misses all non-lethal damage, and it credits a full victim HP to whoever lands the last bullet.

## Current behaviour

- At `bd6ccbe` there is no health decoding at all. `git grep -n -i "damage\|0x25, 0x26, 0x76" bd6ccbe -- dissect` returns nothing. Stats rows are `dissect/stats.go`: `type PlayerRoundStats struct { Username ... Kills ... Died ... Assists ... Headshots ... HeadshotPercentage ... OneVx ... ClutchDetail }` and `type PlayerMatchStats struct { ... Rounds ... Kills ... Deaths ... Assists ... Headshots ... HeadshotPercentage }`.
- Reproduction at `bd6ccbe` (CLI built from that commit, Y11S2 match `Match-2026-08-25_20-53-53-41172`):
  ```
  match stats keys: ['assists', 'deaths', 'headshotPercentage', 'headshots', 'kills', 'rounds', 'username']
  round stats keys: ['assists', 'died', 'headshotPercentage', 'headshots', 'kills', 'score', 'username']
  any damage key: False
  ```
- On `origin/fork/health-dbno`, `dissect/health.go` documents the property but registers no listener for it: `//	25 26 76 C9  uint32  current health (100/110/125 at spawn, 20 after a revive, 0 when downed or dead)`. Only `r.Listen(healthLinkIndicator, readHealthLink)` and `r.Listen(healthStateIndicator, readHealthState)` are registered in `dissect/reader.go`. Ownership of a property record is resolved by `func (r *Reader) healthRecordOwner(start int) (owner [4]byte, ok bool)`. That function already handles both the `23 <health id> 00000000 <hash>` first-property form and the `22 <hash>` continuation form.

### What the prototype measured

The prototype is a scratch-only test file. It registers `25 26 76 C9 04` through `MatchReader.Listen` and reuses `healthRecordOwner`/`playerIndexByHealth`.

| Check | Result (user's 5 Y11S2/Y11S3 matches) |
|---|---|
| `25 26 76 C9` occurrences vs. owned `…04 <u32>` records | equal in every round (e.g. 33/33, 38/38); `unowned=0` |
| Samples owned by a health component whose slot is not yet a known player | exactly 10 per round (the pre-clock snapshot written before `readPlayer` sets `slotID`); all resolve after the round |
| Spawn values | 100 / 110 / 125; 135 / 150 with Rook armor (Y11S2: +25; Y11S1: +20) |
| DBNO events with a 0 HP sample within ±1 s | 73 / 73 |
| Deaths (Kill target or Death) whose last HP sample is > 0 | 61 / 316: the final update is often not written. Damage still adds up with the remainder rule |
| HP back to 0 without a death | 1, a player downed at round end (correct: 110 taken) |
| Previous-fraction property `DD 7B 52 C9` equals the prior record's `08 BF 9D FB` | 448 / 451 (misses: 2 revives, 1 Rook pickup changing max HP) → no missed intermediate HP updates |
| Increases during the action phase | revive 0→20 (3), Rook armor +25 (9), nothing else |

The Y11S1 fork matches (ArnaudCabello, nicktstewart) add Finka Adrenal Surge. All teammates get +20 at the same tick (110→130). Health above spawn value then decays exactly 1 HP per second back to spawn: player77 (Blitz) goes `41:130 42:129 … 61:110`. Thatcher, Thermite and Finka show the same. Skopos showed one body-swap jump (84→100).

Worked examples. These are the expected values the tests use, from the prototype.
- Fixture `Y11S2/custom_early_plant.rec`: player41 (Rook, `!125 p150 108 48 0`) → 150; player54 (survived, `p110 70`) → 40; player71 (untouched) → 0.
- Fixture `Y11S2/custom_solid_snake.rec`: player44 (attacker, stale `!66`, reset `p125`, …, 0) → 125; player79 (`!110 p0 p100 53 6 0`) → 100; player71 → 0.
- `20-53-53` r2 player03 → 155. player41 (`150 90 30 0 | 20 0` with a no-killer Death) → 170.
- Y11S1 `Match-2026-03-29_17-32-21-6900` r3 player77 → 121. The naive sum of drops would be 150, because it counts 29 HP of decay.

## Approaches considered

1. **Port lumina's Min/Max per entity.** Small. But it has the stale-snapshot bug, ignores heals, and uses the ID-proximity guess. Rejected: wrong on 1 in 20 player-rounds of our corpus, in both directions.
2. **Health series on our slot link, sum of drops with three rules (chosen).** About 120 lines plus tests. Exact for every case we could observe; the remaining risk is untested heal sources.
3. **Read the fraction properties `08 BF 9D FB` (current / max) and `DD 7B 52 C9` (previous fraction) and compute drops as `(trail - frac) × max`.** This gives per-hit deltas directly. But it needs max HP, which changes with Rook pickups (`11 49 A6 72`), and floats round. It adds nothing over the integer series, and is kept only as an internal check (see Risks).
4. **Also derive "damage dealt".** We could attribute each enemy's drops to whoever later killed them, or to the nearest-in-time kill by a player. Rejected: that is a guess dressed as a measurement (see D3).

## Decision and rationale (autonomous; alternatives recorded)

- **D1: Approach 2.** It is the only one that is exact on every player-round we checked, and it sits on the slot link PR #3 already validated.
- **D2: Definition.** `damageTaken` = HP removed from the player's health pool in the round. This includes prep-phase, fall, self, teammate and Rook-armor HP, and is clamped (overkill is not visible). Alternatives: "enemy damage only" is not separable without an instigator; "net of heals" hides revives. Clamped gross loss is what the stream measures.
- **D3: No damage dealt.** I enumerated every property written on the health component records in the corpus. Values are all scalars or flags, never an entity reference: `25 26 76 C9` HP, `08 BF 9D FB` fraction, `DD 7B 52 C9` previous fraction, `E7 88 F6 A5` state, `72 5E 99 F9` bleed-out, `91 5F DF 38` downed flag, `11 49 A6 72` max HP, `01 3F D2 DA` max-with-boost, `99 FC 61 D9`, and `80 AD CF 7B`. A real "dealt" stat needs a hit/instigator record. The `dissect/wire` decoders on `track/rec-timeline` (PR #10) are the place to look later.
- **D4: Spawn baseline is role-aware.** Attackers use their last pre-action sample (the prep reset at ~-1 s, or the snapshot if unchanged). Defenders use their first sample (the snapshot is fresh for them, and their prep drops are real: player80 fell 110→78 at -29 s). Role comes from `r.Header.Teams[p.TeamIndex].Role`. If the role is empty, use the defender rule. Alternative considered: a role-free "last increase before action" rule. It breaks when a stale snapshot is higher than spawn (`!125 p110` seen for player71).
- **D5: Decay rule.** A drop of exactly 1 HP while the previous value is above the spawn baseline is decay, not damage. Alternative: track max HP from `11 49 A6 72`. That is more exact, but the property is rare and a 1 HP real hit above spawn HP is negligible.
- **D6: Death remainder.** If the player is a `Kill` target or a `Death` username in the round feed and their last sample is > 0, add the last sample.
- **D7: Storage.** Store raw samples on `Reader` keyed by health-component id, and compute in `PlayerStats()`. `Read()` sets `r.b = nil`, so nothing needs the buffer later. Alternative: compute in `Read()` and store a map. That works too, but keeping the computation in a pure function makes it testable without bytes.
- **D8: JSON.** `DamageTaken *int \`json:"damageTaken,omitempty"\`` on both stats structs. A match row's value is the sum over rounds where it is non-nil, and nil if no round had it.
- **D9: Labels.** `enhancement,research`. Heal sources with no sample in the Y11S2+ corpus (Doc stims, Thunderbird Kóna, Finka on Y11S2+, Skopos swaps) need one more replay pass.

## Interactions

- **PR #3 `fork/health-dbno`:** hard dependency (health link, `healthRecordOwner`, `playerIndexByHealth`, DBNO). It needs a rebase onto `bd6ccbe` regardless.
- **PR #10 `track/rec-timeline`:** independent. Its ms clock would sharpen timestamps but is not needed. Its `--timeline` sidecar could later emit the HP series. Out of scope here. Its Y11S3 fixtures, once merged, are a free extra acceptance input.
- **PR #12 (scoreboard reconcile):** none.
- **Item 01 (incomplete-match crash):** a truncated round gives partial damage. The field stays correct for what was read.
- **Item 05 (pattern search across worker-block boundaries):** an HP record split by a worker boundary is missed today, like any listener's. That loses at most one drop in rare cases. Item 05's fix covers it, and no change is needed here.
- **Item 11 (weapon ID per kill, also lumina):** independent. A future "damage dealt" would want both.
- **Item 14 (per-player trades):** none. Survey item 14 also comes from lumina, and its eval is at `docs/research/2026-10-21-fork-eval-14-per-player-trades.md`.
- **JSON / R6Stats consumer:** additive, optional key. `api/r6stats/services/parser_contract.py` builds rows with `_opt_int(...)` per optional field and ignores unknown keys, so ingestion is unaffected. Storing and showing the stat is a separate R6Stats change (add `damage_taken=_opt_int(raw, "damageTaken")` to `_stats_row`). File it as a follow-up, not part of this item.
- **Golden-test churn:** none. `TestReader_Read` compares `Header` and `MatchFeedback` only.

## File structure

- Modify `dissect/health.go`: the `healthValueIndicator` var, the `healthSample` type, the `readHealthValue` listener, and a pure `damageTaken(...)` function. These go here because they extend the health component this file owns.
- Modify `dissect/reader.go`: add a `healthSamples []healthSample` field, and register the listener inside the existing `if r.Header.CodeVersion >= Y10S4 {` block.
- Modify `dissect/stats.go`: add `DamageTaken *int` to `PlayerRoundStats` and `PlayerMatchStats`, fill it in `(*Reader).PlayerStats` and `(*MatchReader).PlayerStats`.
- Modify `dissect/health_test.go`: byte-level listener tests and pure-function tests.
- Modify `dissect/stats_test.go`: aggregation test.
- Modify `dissect/test/acceptance_test.go`: real-replay invariants.
- Modify `dissect/test/reader_test.go` (or a new `dissect/test/damage_test.go`): exact values on the committed Y11S2 fixtures.
- Modify `README.md` (or wherever PR #3 documented DBNO output): one line on `damageTaken`.

## Implementation plan

### Task 1: Record HP samples per health component

**Files:**
- Modify: `dissect/health.go`, `dissect/reader.go`
- Test: `dissect/health_test.go`

**Interfaces:**
- Produces:
  - `var healthValueIndicator = []byte{0x25, 0x26, 0x76, 0xC9, 0x04}`
  - `type healthSample struct { health [4]byte; hp uint32; started bool; phase RoundPhase; elapsed float64 }`
  - `func readHealthValue(r *Reader) error`
  - `Reader.healthSamples []healthSample` in read order

- [ ] **Step 1: Write the failing tests** in `dissect/health_test.go`. Reuse `healthReplay` and `newHealthReader`. `healthReplay` already writes `23 <health> 00000000 25 26 76 C9 04 <u32>`, and its HP value starts at the 4 bytes after the `04`. Add a helper param or a second builder `healthValueReplay(slot, health []byte, hp uint32) (b []byte, linkOffset, valueOffset int)`.
  - `TestHealthValue_Recorded`: link, then set `r.clock.started = true`, `phase = ActionPhase`, `r.offset = valueOffset`, `readHealthValue(r)` with hp 87. Assert `len(r.healthSamples) == 1`, `.hp == 87`, `.health == [4]byte{0x38,0xbb,0x2b,0xf0}`, `.started == true`.
  - `TestHealthValue_RecordedBeforeClock`: same with `r.clock.started = false`. Assert the sample **is** recorded, with `started == false`. Unlike `readHealthState`, pre-clock values matter: the defender spawn snapshot.
  - `TestHealthValue_ResolvedAfterPlayersRead`: the reader's `Header.Players[0].slotID` is nil while the sample is read, and is set to `slot` afterwards. Assert the sample was kept (keyed by health id), and that `r.playerIndexByHealth(sample.health) == 0` after `slotID` is set.
  - `TestHealthValue_UnlinkedIgnored`: no `readHealthLink` call. Assert `len(r.healthSamples) == 0`.
- [ ] **Step 2: Run** `go test ./dissect -run TestHealthValue -v`. Expected: FAIL, `undefined: readHealthValue`.
- [ ] **Step 3: Implement.** In `readHealthValue`: `start := r.offset - len(healthValueIndicator)`. Bounds-check `r.offset+4`, read the LE `uint32`, then `owner, ok := r.healthRecordOwner(start)`. If not ok, return nil. Append the sample. Do **not** require `r.clock.started` and do **not** resolve the player here. Register `r.Listen(healthValueIndicator, readHealthValue)` next to `readHealthState` in `NewReader`. Update the property comment block in `health.go` to say the value is now read, and add the observed spawn values 135/150 (Rook armor).
- [ ] **Step 4: Run** `go test ./dissect -run 'TestHealth' -v`. Expected: PASS, including the existing `TestHealthState*` / `TestResolveHealth_*`.
- [ ] **Step 5: Commit** `feat: record player health values (Y10S4+)`.

### Task 2: Damage taken from a health series

**Files:**
- Modify: `dissect/health.go`
- Test: `dissect/health_test.go`

**Interfaces:**
- Consumes: `healthSample`, `Reader.healthSamples` (Task 1)
- Produces: `func (r *Reader) damageTaken() map[int]int`. Keys are player indices that have ≥ 1 sample; values are HP lost. Plus the pure core `func damageTakenFromSeries(series []healthSample, attacker, died bool) int`.

Algorithm (D4–D6). Each series is one player's samples in read order.
```
base := 0
if attacker { base = index of the last sample with !started || phase == PrepPhase (0 if none) }
dmg := 0
for i := base+1; i < len(series); i++ {
    a, b := series[i-1].hp, series[i].hp
    if b >= a { continue }                           // heal, revive, Rook armor, boost
    if a-b == 1 && a > series[base].hp { continue }  // boost health decaying 1 HP/s
    dmg += int(a - b)
}
if died && series[len(series)-1].hp > 0 { dmg += int(series[len(series)-1].hp) }
```
`damageTaken()` groups `r.healthSamples` by `r.playerIndexByHealth(s.health)`, dropping -1. `attacker` is `r.Header.Teams[p.TeamIndex].Role == Attack`. `died` is true when the player is the `Target` of a `Kill` or the `Username` of a `Death` in `r.MatchFeedback`. That is the same rule `PlayerStats` uses for `Died`.

- [ ] **Step 1: Write the failing tests.** These are table tests on `damageTakenFromSeries`. A helper `ser(started bool, phase RoundPhase, hps ...uint32)` keeps them short, or build the slices inline. The values are the real series from the corpus:
  - `TestDamageTaken_Survivor`: defender `[!110, A70]`, died=false → **40**.
  - `TestDamageTaken_Untouched`: attacker `[!0, P125]` → **0**.
  - `TestDamageTaken_AttackerStaleSnapshot`: attacker `[!66, P125, A101, A77, A53, A29, A0]`, died → **125**. Also attacker `[!110, P0, P100, A53, A6, A0]`, died → **100**. And attacker `[!125, P110, A0]`, died → **110** (stale value higher than spawn).
  - `TestDamageTaken_DeathWithoutZero`: defender `[!110, A87]`, died → **110**. Defender `[!110]`, died → **110** (one-shot, no update at all).
  - `TestDamageTaken_ReviveAndRook`: defender `[!110, A82, A54, A0, A20, A45, A22, A0]`, died → **155**. Defender with prep Rook `[!125, P150, A90, A30, A0, A20, A0]`, died → **170**.
  - `TestDamageTaken_BoostDecay`: attacker `[!0, P110, A130, A129, …(1-steps)…, A110, A130, A129, …, A121, A94, A67, A40, A13, A0]`, died → **121**. Write the 1-steps out with a loop.
  - `TestDamageTaken_DefenderPrepDamage`: defender `[!110, P78, A18, A0]`, died → **110** (the prep fall counts).
  - `TestDamageTaken_RealOneHPHitBelowSpawn`: defender `[!110, A50, A49]`, alive → **61** (a 1-HP drop at or below spawn is damage).
  - `TestReaderDamageTaken_RoleAndDeath`: a `Reader` with two players (team 0 Attack, team 1 Defense), `healthToSlot` / `slotID` set, `healthSamples` for both, and a `Kill` targeting the defender. Assert the map is `{0: <attacker value>, 1: <defender value incl. remainder>}`, and that a third player with no samples is absent.
- [ ] **Step 2: Run** `go test ./dissect -run 'TestDamageTaken|TestReaderDamageTaken' -v`. Expected: FAIL, undefined.
- [ ] **Step 3: Implement** both functions in `dissect/health.go` as specified above.
- [ ] **Step 4: Run** the same command. Expected: PASS.
- [ ] **Step 5: Commit** `feat: damage taken per player from the health series`.

### Task 3: Expose `damageTaken` in round and match stats

**Files:**
- Modify: `dissect/stats.go`
- Test: `dissect/stats_test.go`, `dissect/test/damage_test.go` (new) or `dissect/test/reader_test.go`

**Interfaces:**
- Consumes: `(*Reader).damageTaken() map[int]int` (Task 2)
- Produces: `PlayerRoundStats.DamageTaken *int \`json:"damageTaken,omitempty"\``, and `PlayerMatchStats.DamageTaken *int \`json:"damageTaken,omitempty"\``

- [ ] **Step 1: Write the failing tests.**
  - `dissect/stats_test.go` `TestPlayerStats_DamageTakenOmittedWithoutHealth`: a Reader with players and no `healthSamples`. Every `PlayerStats()[i].DamageTaken == nil`, and `json.Marshal` of a row does not contain `"damageTaken"`.
  - `dissect/stats_test.go` `TestMatchPlayerStats_DamageTakenSums`: a `MatchReader{rounds: []*Reader{r1, r2}}` where r1 gives player A 40 and r2 gives player A 110, and player B has data only in r2 (r1 nil). Expect A → 150 and B → its r2 value. A player who is nil in all rounds → nil.
  - `dissect/test/damage_test.go` `TestDamageTaken_Y11S2Fixtures`: read each file with `dissect.NewReader`/`Read`, then call `PlayerStats()`. Assert by username:
    - `data/replays/valid/Y11S2/custom_early_plant.rec`: player41 150, player03 150, player31 110, player40 135, player24 150, player44 100, player80 110, player71 0, player79 100, player54 40.
    - `data/replays/valid/Y11S2/custom_solid_snake.rec`: player41 110, player03 125, player31 100, player40 110, player24 110, player44 125, player80 110, player71 0, player79 100, player54 110.
  - Same file, `TestDamageTaken_OlderSeasonsOmitted`: every `.rec` under `data/replays/valid/Y8S1`, `Y8S2` and `Y9S1` has `DamageTaken == nil` for all rows.
- [ ] **Step 2: Run** `go test ./dissect/... -run 'DamageTaken' -v`. Expected: FAIL (unknown field `DamageTaken`).
- [ ] **Step 3: Implement.** In `(*Reader).PlayerStats`, call `dt := r.damageTaken()` once. For each player index `i` with `v, ok := dt[i]`, set `stats[i].DamageTaken = &v`, taking a fresh variable per iteration. In `(*MatchReader).PlayerStats`, when `p.DamageTaken != nil`, add it to `stats[i].DamageTaken`, allocating it on first use.
- [ ] **Step 4: Run** `go test ./...`. Expected: PASS, with `TestReader_Read` unchanged (no `-update`).
- [ ] **Step 5: Commit** `feat: damageTaken in round and match stats`.

### Task 4: Real-replay acceptance invariants and docs

**Files:**
- Modify: `dissect/test/acceptance_test.go`, `README.md` (output section)

- [ ] **Step 1: Extend `checkRound`.** For rounds with `CodeVersion >= dissect.Y10S4`, check that every `PlayerStats()` row has a non-nil `DamageTaken`. Also check `0 <= *DamageTaken <= 400`, and `*DamageTaken > 0` whenever `Died`. Log the total and the per-round mean next to the existing DBNO/Revive summary (`t.Logf("damage taken: %d over %d player-rounds", ...)`).
- [ ] **Step 2: Run** on the user's corpus:
  ```
  R6_DISSECT_REPLAY_DIRS=$(ls -d <scratch>/replays/Match-* <scratch>/replays-new/Match-*[0-9] | tr '\n' ':' | sed 's/:$//') go test ./dissect/test -run TestAcceptance_Replays -v
  ```
  Expected: PASS, with 420 player-rounds over the 5 matches. If the scratch paths are gone, use whatever real Y11S2+ match folders are available, and record which ones.
- [ ] **Step 3: Spot-check two values by CLI.** `go run . <Match-2026-08-25_20-53-53-41172>` → round 2 `stats[]`: player03 `damageTaken` 155 and player41 170; and `<Match-2026-08-25_19-56-00-32000>` round 9: player71 25.
- [ ] **Step 4: Document** in README's JSON output section: `damageTaken` (Y10S4+) is HP lost that round from any source, including self, fall, teammates and Rook armor HP. Boost-health decay is excluded, the value is clamped at the player's remaining HP, and it is absent before Y10S4.
- [ ] **Step 5: Commit** `test: damage taken acceptance on real replays; docs`.

### Task 5 (research, can run after merge): heal sources not in the Y11S2+ corpus

- [ ] Get one Y11S2+ match each with Doc (stims on self/ally, overheal), Thunderbird (Kóna), Finka, and Skopos swapping shells. Candidates: the user's next uploads, or any new fork fixtures.
- [ ] Dump the series with a scratch test like the prototype: register `healthValueIndicator` and print per-player `elapsed:hp`. Check that boost/overheal health decays in 1 HP steps above spawn HP. If Doc's overheal decays differently (step ≠ 1), widen D5 to "any drop while above baseline with no matching `DD 7B 52 C9` trail change". If Skopos swaps produce false drops, exclude Skopos rows. To tell a swap from a hit, look for drops with no change in the previous-fraction property, or for any shell-entity signal.
- [ ] Record the results in the plan's Risks section, and close the `research` label.

## Acceptance

- `go test ./...` passes on the branch. The new `TestDamageTaken_*`, `TestHealthValue_*` and `TestDamageTaken_Y11S2Fixtures` tests are red today, because the symbols and field do not exist.
- The CLI output for any Y11S2+ match has `damageTaken` on every round `stats[]` row and on every match `stats[]` row. Y8/Y9 fixture output is byte-identical to before.
- `TestAcceptance_Replays` passes on the user's 5 real matches.

## Risks / open questions

- **Untested heal sources** (Task 5): Doc, Thunderbird, Y11S2+ Finka, Skopos. The decay rule is validated only on Y11S1 Finka (8 boosts, all exact 1 HP/s).
- **Post-round damage:** I did not separate damage after the round-end event. None was seen in the corpus. If it appears, cap samples at the round's end tick.
- **Coarse timing:** samples are stamped with the 1 s clock. That is irrelevant to a sum, but limits any later per-hit timeline (PR #10's ms clock would fix it).
- **Overkill is invisible:** a 110 HP player one-shot by a 180-damage headshot shows 110. This is by definition (D2), and the README says so.
- **What would change the verdict on damage dealt:** finding, for example in PR #10's TLV object graph, a hit or damage record carrying an instigator entity that can be linked to a player pawn. Item 11 (weapon id per kill) may surface related kill-record fields.
- **Internal check:** if a future season changes the HP encoding, the `DD 7B 52 C9` == previous `08 BF 9D FB` invariant is a cheap diagnostic. Over 99% of records matched in Y11S2/Y11S3.
