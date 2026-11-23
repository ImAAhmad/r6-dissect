# Fork port 11: weapon per kill

> **Issue:** #28 (weapon per kill), #35 (reconcile pass order) · 11b weapon veto: no issue (would undo correct downer credits) · fork survey: `docs/research/2026-10-21-fork-survey.md`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every Kill event in the parser's JSON carries two new fields. `weaponID` is the asset ID of the item that dealt the finishing blow. `weaponSlot` is the slot that holds that item in the loadout of the player the kill packet names.

**Architecture:** The kill packet ends with a tagged field, `22 F8 6C DD 65 08 <uint64 LE>`, right after the headshot byte. We read it without moving the stream offset and store it on `MatchUpdate`, together with the kill packet's own killer. After `applyLoadouts`, we match the ID against that player's `Loadout`. The IDs are the same asset IDs the loadout already uses. A separate Task 3 (issue 11c) fixes a pass-ordering bug in the Y10S4+ scoreboard reconciliation that this investigation found.

**Tech Stack:** Go (`dissect` package), golden replay JSON tests.

**Spec:** this document (design and plan combined, as the fork-port brief requires).

Verified against `bd6ccbeb7b46af99638df5d650187069e849f925` (`origin/main`). The reconciliation analysis also used `origin/fork/health-dbno` at `c608cf8` (PR #3).

**Revision 2026-10-22.** The first version proposed issue 11b: veto a reconciliation move when the weapon does not belong to the new killer. The coordinator pointed out that the scoreboard credits the player who DOWNED the target, while the kill feed names the player who FINISHED them. Re-checked against PR #3's health state, 11b is withdrawn (NO-ISSUE; see "11b re-evaluation"). Task 2 now classifies against the kill packet's killer (`feedKiller`) instead of the credited `username`. Issue #28, already filed for 11a, needs that amendment.

## Global Constraints

- JSON keys are `weaponID` (number, omitted when 0) and `weaponSlot` (string, one of `primary`, `secondary`, `gadget`, `ability`; omitted when empty). They appear only on `Kill` events.
- `weaponID` and `weaponSlot` describe the finishing blow, from the kill packet. They do not change when reconciliation credits the kill to the downer.
- The weapon read never returns an error and never moves `r.offset`. A missing or unexpected tag gives `weaponID` 0, never an aborted round.
- No weapon-name dictionary in the parser.
- Commit labels: `feat(dissect): ...` for Tasks 1 and 2, `fix(dissect): ...` for Task 3. Never `docs(...)` for a code commit.

## Review Focus

1. **A kill packet that ends exactly at the end of the decompressed buffer.** The peek must bounds-check and return 0. It must not call `r.Bytes`/`r.Skip`, which return `io.EOF` and abort the round (item 03). Test: `TestReadMatchFeedback_KillWithoutWeaponField/truncated`.
2. **A tail with another field in the weapon position** (a future season). `weaponID` must be 0. Test: `.../other_field`.
3. **A kill made with another player's deployed gadget**, for example Kapkan credited with a kill by Goyo's Volcan (201523838452). `weaponID` is set and `weaponSlot` is empty. Test: `TestClassifyKillWeapons`, the weapon-99 case.
4. **A kill that reconciliation moved to the downer.** The slot must come from the finisher (the kill packet's killer), not from the credited `username`. Test: `TestClassifyKillWeapons`, the `feedKiller` case. In the corpus this keeps 567 of 570 kills classified, against 556 when classifying by `username`.
5. **The same asset ID in two slots.** In the Y11S3 corpus, Blackbeard's primary and ability are both 395972905309. The first match wins, in the order primary, secondary, gadget, ability. Test: `TestLoadoutSlot`.

---

## Summary

- **11a (#28, enhancement): FILE.** Emit `weaponID` and `weaponSlot` per kill.
  - The field is on 570 of 570 kills across Y8S1, Y8S2, Y9S1, Y11S1, Y11S2 and Y11S3.
  - The ID is in the kill packet killer's own loadout for 567 of 570 kills.
  - The primary/secondary/gadget/ability split needs no name dictionary.
  - #28 needs one amendment: classify against `feedKiller` (Task 2).
- **11b (weapon veto on reconciliation): NO-ISSUE.** Of the 19 distinct moves seen over 5 corpus runs:
  - 6 are legitimate downer credits: the scoreboard, the game's own count, keeps the credit in later rounds.
  - 13 are scoreboard-mapping errors, all in one Y11S1 match whose zip mapping is broken (#26).
  - None is unexplained.
  - A weapon veto would block all 6 correct moves.
- **11c (bug): FILE, or fold into PR #12.** `reconcileKillsFromScoreboard` runs its fallback pass for one over-credited player before trying the exact timing match for the others, and `break`s on an exhausted over-credited player. With two timing-matched pairs, the correct result came out in 0 of 400 runs.

## Source

- **Fork:** `lumina-r6/main`, commit `b5be04cde41065696afd0b6fdb3df5686ce10edc` ("feat(kill): decode field structure, expose WeaponID"), file `dissect/feedback.go`.
- **What they did:**
  - Documented the kill packet as a sequence of `[5-byte tag][1-byte size][value]` fields.
  - After the headshot byte, read `r.Bytes(5)`, compare it with `22 F8 6C DD 65`, then read `r.Uint64()`.
  - Expose the result as `WeaponID uint64 json:"weaponID,omitempty"`.
  - Validated on Y8S1, Y8S2 and Y9S1; no dictionary.
- **Flaws:**
  - `r.Bytes`/`r.Uint64` can return `io.EOF` and abort the rest of the round (item 03 class).
  - The size byte is not checked.
  - A raw ID without a slot means little to a consumer.

## Current behaviour (bd6ccbe)

`dissect/feedback.go` stops after the headshot flag (`headshot, err := r.Int()`) and never reads the packet's tail. `dissect/loadout.go` already has the asset IDs (`Primary   uint64 \`json:"primary,omitempty"\``), applied after `roundEnd` in `dissect/reader.go` (`r.roundEnd()` … `r.applyLoadouts()`). On Y10S4 and later, `roundEnd` reconciles kills against the scoreboard (`dissect/time.go`: `r.reconcileKillsFromScoreboard()`). That step reassigns kills (`dissect/scoreboard.go`: `u.Username = underUsername`).

### Reproduction: the weapon field (scratch clone at bd6ccbe, probe patch `probe11.diff`)

The corpus is 78 rounds:

- the main fixtures: Y8S1, Y8S2, Y9S1, Y11S2;
- the Y11S3 fixtures from `origin/track/rec-timeline`;
- the user's Y11S2 matches (2026-08-25) and Y11S3 matches (2026-10-18);
- Y11S1 rounds from `ArnaudCabello/reinforcements:Matchreplay/...` and `nicktstewart/main:.../defuser_detection/match{4,5}`, used as data only.

| Season (codeVersion) | Kills | Tag right after headshot byte, size 08 | ID in kill packet killer's loadout |
|---|---|---|---|
| Y8S1 (7422506, 7441957) | 21 | 21 | 21 |
| Y8S2 (7601998) | 33 | 33 | 33 |
| Y9S1 (8211379) | 7 | 7 | 7 |
| Y11S1 (9540659, 9578674) | 154 | 154 | 153 |
| Y11S2 (9838928) | 161 | 161 | 160 |
| Y11S3 (9950954) | 194 | 194 | 193 |
| **Total** | **570** | **570 (distance 0 every time)** | **567** |

- **Slot counts:** primary 454, secondary 86, gadget 20, ability 7, unclassified 3.
- **The 3 unclassified:**
  - `70077334926` on two kills. It is in no loadout; my guess is melee.
  - Goyo's Volcan, credited to Kapkan.
- **Consistency:** 140 (round, killer) groups have two or more kills. 110 of them use one ID. In 29 of the other 30, every ID is one of the killer's own slots (weapon swaps).
- **The rest of the kill packet** carries one of two field layouts, which differ by season:
  - Y11S2 and later: `419d9dd7`(8 bytes), `c43f6ab1`, `be1f97ea`;
  - Y11S1: `b592028f`, `84bcc45b`, `0548b241`.

  None of these fields correlates with whether the target was downed first.

### 11b re-evaluation (PR #3 `origin/fork/health-dbno`, c608cf8, with logging of kill packets, health states and scoreboard increments)

The question: does a reconciliation move credit the downer (correct by the game's definition) or come from a mapping error?

**Method.** Each round was parsed 5 times; moves are nondeterministic. The union is 19 distinct moves. For each move I recorded:

- (a) whether the target's health state entered DBNO (3) before the kill;
- (b) who downed them;
- (c) whether the new killer is on the killer's team;
- (d) a classification.

The check that decides (d): scoreboard kill counters are cumulative over a match, so each round's initial counter (`scoreboard_kill_header`) must equal the mapped player's kill-feed kills in earlier rounds. In a match with a correct mapping, a credited downer shows up as a persistent +1 for the downer and −1 for the finisher in every later round.

- **(b) cannot be answered independently.** PR #3 sets the DBNO's downer by copying the post-reconciliation Kill's killer (`resolveHealthEvents`: `d.Username = u.Username`), so it simply repeats the move. No packet at the time of the down carries an attacker or a weapon.
- **(c):** in all 19 moves the new killer is on the killer's team.
- **User matches.** The mapping is consistent: initial counters match kill-feed history except for the persistent +1/−1 left by the moves below. The 6 moves:

| Round | Move (feed killer → credited) | Target health | (d) |
|---|---|---|---|
| 08-25 19-56 R05 | player71 → player79 | DBNO and death at 105 s | downer credit, confirmed: player79 +1 and player71 −1 on the scoreboard in R06–R09 |
| 08-25 20-53 R06 | player24 → player03 | DBNO and death at 32 s | downer credit, confirmed: Ark +1 in R07–R09 |
| 10-18 19-08 R02 | player90 → player74 | DBNO 75 s, finished 83 s | downer credit, confirmed: player74 +1 in R03–R12 |
| 10-18 21-07 R02 | player61 → player76 | DBNO 113 s, death 114 s; player76 (Smoke) died at 104 s | downer credit, confirmed: player76 +1 in R03–R04; a dead downer, likely by gadget |
| 08-25 19-56 R10 | player65 → player24 | DBNO and death at 126 s | likely downer credit (last round, no later check) |
| 10-18 19-08 R06 | player90 → player35 | DBNO and death at 140 s | likely downer credit (Mako not mapped in later rounds) |

- **nicktstewart match4 (Y11S1), 13 moves in R02–R04.** These are mapping errors (#26). The evidence:
  - The initial counters disagree with kill-feed history in R02, R03, R04 and R05.
  - Counters mapped to players who were already dead keep counting: `player50` died at 15 s yet is credited at 164 s and 175 s; `player07` died at 32 s yet is credited at 57 s.
  - The counter `4c49…` mapped to player27 increments at player50's kills 4 times.
  - Two moves in this match also look like downer credits on their own (R02 player33 downed at 57 s; R04 player68 downed at 51 s). They cannot be judged until #26 fixes the mapping.

**Conclusion.** The coordinator's alternative is right. The weapon of a moved kill is the finisher's by design, so a weapon veto would undo exactly the correct credits. A narrower guard would also be sound: "no move when the target never entered DBNO", since without a down there is no separate downer. Every move that guard blocks in the corpus is in the broken-mapping match. It needs PR #3's health state and no weapon at all, so it belongs in #26 or PR #12 as an acceptance oracle, not in this item.

### Reproduction: the `break` in reconciliation (11c)

A scratch test at bd6ccbe:

- two over-credited players O1 and O2;
- two under-credited players U1 and U2;
- the kill `O1>E1` has `scoreboardKiller: "U1"` and `O2>E2` has `scoreboardKiller: "U2"`.

Over 400 runs the outcomes were `O1 U1` 69, `U1 O2` 274, `U2 O2` 57. The correct `U1 U2` never occurred. Two things cause this, both in `dissect/scoreboard.go`:

- `for pass := 0; pass < 2; pass++` sits inside the loop over each (under, over) pair, so the fallback pass 1 runs for the first pair before pass 0 (the exact timing match) has been tried for the others;
- `if underCredited[ui].diff <= 0 || overCredited[oi].diff <= 0 { break }` stops at the first exhausted over-credited player.

PR #12's sort makes the result deterministic, `U1 O2`, but still wrong. Under the downer reading, every dropped move is a lost correct credit.

## Approaches considered

**A. Where to read the weapon.**
1. Port lumina as is: adds EOF-abort paths.
2. **Peek with a bounds check, using the 6-byte tag including the size byte.** Chosen.
3. Search forward: unnecessary, since the tag sat at distance 0 every time. It could also match a later packet.

**B. Who classifies the slot.**
1. Raw ID only, with R6Stats joining it to the loadout. R6Stats ingests no loadouts and cannot see the kill packet's killer.
2. **The parser emits `weaponID` and `weaponSlot`, classified against the kill packet's killer (`feedKiller`).** Chosen.
3. Classify against the credited `username`. Rejected: it loses the slot for every downer credit (556 of 570 classified, against 567).
4. A name dictionary in the parser. Rejected: names are a data concern; the IDs are stable across seasons.

**C. Reconciliation (formerly 11b).**
1. Weapon veto: rejected, it blocks correct downer credits (see above).
2. Fix the mapping: #26.
3. **Fix the pass ordering and the `break`: issue 11c, Task 3.**

## Decision and rationale

Decisions (autonomous, per brief):

- **Scope.** 11a (#28) is Tasks 1–2. 11b is withdrawn. 11c is Task 3, independent of 11a.
- **Read method:** peek (A.2). The tag includes `08`, so a resized field reads as "not found".
- **The slot follows the finisher.** An unexported `MatchUpdate.feedKiller` is set to the packet's killer when the kill is parsed. `classifyKillWeapons` uses it, falling back to `Username`. `weaponID` and `weaponSlot` therefore always describe the finishing blow.
- **Not emitted here: `finishedBy`.** The parser does not emit `finishedBy` for moved kills in this item, although R6Stats' contract already reads `finishedBy` (`api/r6stats/services/parser_contract.py`: `finished_by=_opt_str(raw, "finishedBy")`) and it would be the natural home for the finisher's name. That belongs with PR #3 or #26, which own downer and finisher semantics. It is recorded under Interactions.
- **Slot order:** primary, secondary, gadget, ability. The first match wins.
- **Empty slot for items outside the finisher's loadout.** No `"other"` value.
- **No `stats.go` aggregation and no Excel column.**
- **11c fix:**
  - make `pass` the outermost loop, so pass 0 runs over every pair before pass 1;
  - `continue` past an exhausted over-credited player;
  - `break` only when the under-credited player is satisfied.

## Interactions

- **PR #3 `origin/fork/health-dbno`:**
  - Textual conflicts only, in the `MatchUpdate` struct and the end of `Read()`. Keep `classifyKillWeapons()` last.
  - PR #3's DBNO downer is copied from the reconciled Kill. So, once 11c restores dropped timing-matched moves, more DBNOs will name the right downer.
  - The same branch is the right place for a `finishedBy` key on reconciled kills, which can reuse `feedKiller`.
- **PR #10 `origin/track/rec-timeline`:**
  - Textual conflicts only: `r.addUpdate(u)` and new unexported fields.
  - Its Y11S3 fixture goldens will churn too.
- **PR #12 (deterministic reconcile):**
  - Composes with 11c. Both touch the same function, so land PR #12 first and rebase 11c on it.
  - Alternatively, fold 11c into the PR #12 review as a requested change.
- **#26 (item 09, scoreboard entity link):** this is the cause of the 13 match4 moves. After #26, two things should hold on that match:
  - the round-initial counters should match kill-feed history (the cumulative check above, `scripts11/cum.py`);
  - no move should touch a target that never entered DBNO.
- **Item 03:** Task 1 adds no EOF path.
- **JSON / R6Stats:**
  - The new keys are additive and `omitempty`. `parser_contract.py` reads keys explicitly, so extra keys are ignored.
  - R6Stats follow-up: `weapon_id` and `weapon_slot` columns on `RoundEvent`, plus the Alembic migration, the `ingest.py` mapping and `EventData` fields. R6Stats vendors the parser, so it also needs a vendor bump.
  - Neither reference site shows weapon stats.
- **Golden churn:**
  - Task 2: 11 of the 12 main fixture JSONs change. 78 kills gain the two keys (66 primary, 7 secondary, 5 gadget).
  - Task 3: no golden change.

## Implementation plan

### Task 1: Read the weapon ID from the kill packet

**Files:**
- Modify: `dissect/feedback.go`: the struct `MatchUpdate`, the `killIndicator` var block, and `readMatchFeedback` right after `u.Headshot = headshotPtr`.
- Create test: `dissect/feedback_weapon_test.go`.

**Interfaces:**
- Produces:
  - `MatchUpdate.WeaponID uint64 \`json:"weaponID,omitempty"\``;
  - unexported `MatchUpdate.feedKiller string`, set to the packet's killer username for every Kill;
  - `var killWeaponField = []byte{0x22, 0xF8, 0x6C, 0xDD, 0x65, 0x08}`;
  - `func peekKillWeapon(r *Reader) uint64`. When `r.b[r.offset:r.offset+6]` equals the tag and the bounds hold, it reads `r.b[r.offset+6:r.offset+14]` little-endian. Otherwise it returns 0. It never moves `r.offset`.

- [ ] **Step 1: Write the failing tests** in `dissect/feedback_weapon_test.go` (package `dissect`):

```go
// killPacket builds a Y9S1Update3+ kill packet as it appears right after the
// match feedback listener pattern (59 34 E5 8B 04).
func killPacket(killer, target string, headshot byte, tail []byte) []byte {
	p := make([]byte, 38) // skipped header
	p = append(p, 0x00)   // size 0: kill
	p = append(p, killIndicator...)
	p = append(p, byte(len(killer)))
	p = append(p, killer...)
	p = append(p, make([]byte, 15)...)
	p = append(p, byte(len(target)))
	p = append(p, target...)
	p = append(p, make([]byte, 56)...)
	p = append(p, headshot)
	return append(p, tail...)
}

func weaponTail(id uint64) []byte {
	return binary.LittleEndian.AppendUint64(append([]byte(nil), killWeaponField...), id)
}

func killReader(b []byte) *Reader {
	return &Reader{b: b, Header: Header{CodeVersion: Y11S2, Players: []Player{
		{Username: "atk", TeamIndex: 0}, {Username: "def", TeamIndex: 1},
	}}}
}
```

  - **`TestReadMatchFeedback_KillWeaponID`.** Input: `b := append(killPacket("atk","def",1, weaponTail(1366019496)), 0x00)`. The extra byte avoids `Skip`'s EOF at the exact end of the buffer. Expect: no error, one update, `WeaponID == 1366019496`, `feedKiller == "atk"`.
  - **`TestReadMatchFeedback_KillWithoutWeaponField`**, with two subtests, each with `0x00` appended. Expect: no error, one kill, `WeaponID == 0`.
    - `other_field`: tail `{0x22,0x41,0x9D,0x9D,0xD7,0x08,1,2,3,4,5,6,7,8,0}`;
    - `truncated`: tail `weaponTail(1366019496)[:10]`.
- [ ] **Step 2: Run them and check they fail.** `go test ./dissect/ -run 'KillWeaponID|KillWithoutWeaponField'` fails to build: `undefined: killWeaponField`.
- [ ] **Step 3: Implement.**
  - Add the fields, each with a comment: the asset ID of the finishing item, the same IDs as `Loadout`, possibly another player's gadget.
  - Add the tag var, with a comment listing the verified seasons, and add `peekKillWeapon`.
  - In `readMatchFeedback`, right after `u.Headshot = headshotPtr`, set `u.WeaponID = peekKillWeapon(r)` and `u.feedKiller = username`.
  - When the ID is 0, log at debug: `"kill weapon field not found"`.
  - Import `encoding/binary`.
- [ ] **Step 4: Run them and check they pass.** The same command passes.
- [ ] **Step 5: Commit** `feat(dissect): read the weapon asset ID from each kill packet`. Credit lumina-r6 b5be04c in the commit body.

### Task 2: Classify the weapon by the finisher's loadout slot, regenerate goldens

**Files:**
- Modify:
  - `dissect/loadout.go`: append the type, the method and the reader pass;
  - `dissect/feedback.go`: the struct field;
  - `dissect/reader.go`: in `Read()`, call the pass after `r.applyLoadouts()`.
- Test: `dissect/feedback_weapon_test.go`, and the goldens under `dissect/test/data/replays/valid/**.rec.json`.

**Interfaces:**
- Consumes: `MatchUpdate.WeaponID` and `MatchUpdate.feedKiller` (Task 1).
- Produces:
  - `type WeaponSlot string`, with consts `PrimarySlot = "primary"`, `SecondarySlot = "secondary"`, `GadgetSlot = "gadget"` and `AbilitySlot = "ability"`;
  - `func (l *Loadout) Slot(asset uint64) WeaponSlot`. It is nil-safe and returns `""` for asset 0 or no match. It checks Primary, Secondary, Gadget, Ability, in that order;
  - `MatchUpdate.WeaponSlot WeaponSlot \`json:"weaponSlot,omitempty"\``;
  - `func (r *Reader) classifyKillWeapons()`. For every `Kill`, it takes `killer := u.feedKiller`, or `u.Username` when `feedKiller` is empty. It then sets `u.WeaponSlot = r.Header.Players[PlayerIndexByUsername(killer)].Loadout.Slot(u.WeaponID)`, or `""` when that player is not found.

- [ ] **Step 1: Write the failing tests.**
  - **`TestLoadoutSlot`.** With `l := &Loadout{Primary: 1, Secondary: 2, Gadget: 3, Ability: 4}`, expect:
    - `Slot(1)` to `Slot(4)` return primary, secondary, gadget and ability;
    - `Slot(5)` and `Slot(0)` return `""`;
    - `(*Loadout)(nil).Slot(1)` returns `""`;
    - `(&Loadout{Primary: 7, Ability: 7}).Slot(7)` returns `PrimarySlot`.
  - **`TestClassifyKillWeapons`.** Players are `atk` (Loadout `{10,20,30,40}`) and `def` (no loadout). Feedback and expected slots:
    - Kill atk>def, weapon 20 → `SecondarySlot`;
    - Kill atk>x, weapon 99 → `""`;
    - Kill def>atk, weapon 10 → `""`;
    - Death y → `""`;
    - Kill `Username: "def"`, `feedKiller: "atk"`, weapon 30 → `GadgetSlot`. This is the reconciled downer credit.
- [ ] **Step 2: Run them and check they fail.** `go test ./dissect/ -run 'LoadoutSlot|ClassifyKillWeapons'` fails to build: `undefined: WeaponSlot`.
- [ ] **Step 3: Implement.** Add the type, the method, the field and `classifyKillWeapons`. In `Read()`, call `r.classifyKillWeapons()` as the last statement inside `if !r.readPartial {`, after `r.applyLoadouts()`. It must run after `roundEnd`; with `feedKiller` the order no longer matters for correctness, but keep it last.
- [ ] **Step 4: Run them and check they pass.**
- [ ] **Step 5: The golden test is red.** `go test ./dissect/test -run TestReader_Read` reports `MatchFeedback mismatch` naming `WeaponID` / `WeaponSlot`.
- [ ] **Step 6: Regenerate the goldens and inspect them.**
  - Run `go test ./dissect/test -run TestReader_Read -update`.
  - `git diff --stat -- dissect/test/data` should show 11 files.
  - Every Kill gains exactly the two keys and nothing else changes. Check with `jq -r '.matchFeedback[]|select(.type.name=="Kill")|.weaponSlot' dissect/test/data/replays/valid/*/*.rec.json | sort | uniq -c`, which should give `66 primary`, `7 secondary` and `5 gadget`.
- [ ] **Step 7:** `go vet ./... && go test ./...` passes.
- [ ] **Step 8: Commit** `feat(dissect): weaponSlot per kill from the finisher's loadout`, with the goldens in the same commit.

### Task 3 (issue 11c): Reconciliation tries every exact timing match before the fallback

**Files:**
- Modify: `dissect/scoreboard.go` (`reconcileKillsFromScoreboard`, the nested loops after `if len(overCredited) == 0 || len(underCredited) == 0 {`).
- Create test: `dissect/reconcile_pairs_test.go`.

**Interfaces:** none new. It works on `scoreboardKiller` (existing).

- [ ] **Step 1: Write the failing test** `TestReconcileKillsFromScoreboard_TwoPairsTimingMatched`. Repeat the following 200 times, because the over/under lists come from map iteration:
  - a `Reader` with `scoreboardIDToPlayer: {{1}:0,{2}:1,{3}:2,{4}:3}`, all initial kills 0, and `scoreboardFinalKills: {0:0,1:0,2:1,3:1}`;
  - players `O1`, `O2`, `U1`, `U2` (team 0) and `E1`, `E2` (team 1);
  - kills `{Kill, "O1", "E1", scoreboardKiller: "U1"}` and `{Kill, "O2", "E2", scoreboardKiller: "U2"}`;
  - after `reconcileKillsFromScoreboard()`, the killers must be `U1` and `U2`.
- [ ] **Step 2: Run it and check it fails.** `go test ./dissect -run TwoPairs` fails with `killers "U1", "O2"; want U1, U2` (or another wrong pair).
- [ ] **Step 3: Implement.**
  - Hoist `for pass := 0; pass < 2; pass++` to be the outermost loop around `for ui := range underCredited`, with a comment that pass 0 runs over every pair before the fallback.
  - In the `for oi` loop, split the guard into two checks:
    - `if underCredited[ui].diff <= 0 { break }`;
    - `if overCredited[oi].diff <= 0 { continue }`.
  - Keep the inner kill loop and its log line unchanged.
- [ ] **Step 4: Run it and check it passes.** Then run `go test ./...`, which passes with no golden change.
- [ ] **Step 5: Commit** `fix(dissect): try every exact scoreboard timing match before the fallback`.

## Acceptance

- **11a (#28):**
  - `go test ./dissect/ -run 'KillWeaponID|KillWithoutWeaponField|LoadoutSlot|ClassifyKillWeapons'` passes; today it does not build.
  - Every Kill in the goldens has a non-zero `weaponID` and a non-empty `weaponSlot`.
  - Corpus (scratch prototype `r6-11a-v2`): 570 of 570 kills have a `weaponID` and 567 have a `weaponSlot`, with reconciliation active.
- **11c:**
  - `TestReconcileKillsFromScoreboard_TwoPairsTimingMatched` is red at bd6ccbe and green after Task 3. This was verified in scratch.
  - In the corpus the prototype restores timing-matched moves: 15 moves against 13–14 at bd6ccbe.

## Risks / open questions

- **Y9S2–Y10S4 are untested.** There is no replay for those seasons. The tag check makes a changed layout read as 0, never as garbage.
- **`70077334926`** is in no loadout (2 kills). It is probably melee; leave it unclassified until a labelled kill confirms it.
- **Downer versus finisher semantics** are now explicit: `username` is the credited (scoreboard) killer and `weaponSlot` is the finisher's. R6Stats can count kills per weapon slot correctly only for kills that were not reconciled, unless a `finishedBy` key is added (PR #3 / #26).
- **11c and fallback moves.** 11c changes which kill a fallback (pass 1) move picks. Pass-1 moves stay a guess by construction: in the corpus, 21-07 R02 was a pass-1 move whose credit count the later scoreboard confirms, but whose choice of kill is not verifiable.
- **The weapon name dictionary** remains an R6Stats data task. The IDs are stable across seasons.

Prototype artifacts (scratch, not committed). All paths are under `<scratch>/`:

- `wt11-11a-v2.diff`: Tasks 1–2 with `feedKiller`, plus goldens;
- `fix11c.diff` and `wt-11/dissect/reconcile_pairs_test.go`: Task 3;
- `wt-11b/`: PR #3 plus probe logging;
- `scripts11/dbno.py`, `cum.py` and `tail.py`: the 11b re-evaluation scripts;
- `scripts11/moves_run*.json`: the 5 runs.
