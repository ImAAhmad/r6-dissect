# Fork eval 14: per-player trade stats (`per-player-trades`)

Date: 2026-10-21. Verified against `bd6ccbeb7b46af99638df5d650187069e849f925` (our `main`) and
`origin/fork/health-dbno` at `c608cf8` (PR #3).

**Verdict: NO-ISSUE.** R6Stats already computes trades, traded deaths, kills traded back and the
traded opening flags itself, from the parser's kill feed. It uses a better definition than either
the fork's or ours: non-adjacent, team-aware, teamkills excluded, and a configurable 5 s window. The
parser's JSON has no trade output at all. Adding `trades` / `tradedDeaths` / `traded` to the parser
would give us a second definition that disagrees with the one the website shows (3.5 s against
5 s). It would also put a stats-policy knob into a parse step, which cannot be recomputed without
re-parsing.

## 1. Source

- Fork: `defiancer6s-hub/main`, commit `17feeb3` ("feat: monotonic round clock, 3.5s trades,
  reliable win conditions"). The trade changes are in `dissect/stats.go` and `dissect/feedback.go`.
- What it does:
  - `const TradeWindowSeconds = 3.5`
  - `tradePairs()`: for every Kill `a`, scan forward over later Kills `b` until
    `b.TimeElapsed - a.TimeElapsed > TradeWindowSeconds`; the first `b` with `b.Target == a.Username`
    pairs with `a`. The scan is not limited to adjacent entries.
  - `markTrades()` stamps `Traded *bool` (`json:"traded,omitempty"`) on every Kill.
  - `PlayerRoundStats` and `PlayerMatchStats` gain `Trades` (the avenger's count) and
    `TradedDeaths` (the avenged victim's count).
  - It adds its own monotonic `TimeElapsed`. We already have `ElapsedSeconds` for that.
- Flaws:
  - **No team check and no teamkill exclusion.** If X teamkills teammate V and an enemy then kills
    X, the fork records a "trade" credited to the enemy, and V gets a traded death. If X's own
    teammate teamkills X after X killed V, the enemy's death V counts as traded by X's teammate.
  - The forward scan stops early (`break`) on the window, which assumes the feedback is time-ordered.
    We only guarantee that once health-dbno sorts the Stop events.
  - The window is a compile-time constant, a policy choice baked into the parser. The fork's own
    commit note admits the 1 s clock resolution makes 3.5 the same as 3.

## 2. Current behaviour (ours)

### Parser

`dissect/stats.go` at bd6ccbe:

```go
var samePlayers = (previous.Target == a.Username || previous.Username == a.Target)
var withinThreshold = (a.ElapsedSeconds - previous.ElapsedSeconds) <= 3
if a.Type == Kill && samePlayers && withinThreshold {
```

- `previous` is the previous feedback event of **any** type. At bd6ccbe that includes
  `DefuserDisableStop` and `DefuserPlantStop`.
- `origin/fork/health-dbno` (PR #3, C2) keeps only Kill events (`if a.Type != Kill || a.TeamKill {
  continue }`). The adjacency and the 3 s window are unchanged.
- **The only consumer is the xlsx export.** In `dissect/match.go`, `trades := r.Trades()` fills
  the "Trades" columns of the per-round sheet. `Trades()` is not serialized to JSON. Nothing in the
  JSON output carries trades.
- **What `previous.Target == a.Username` means:** "the victim of the previous kill now makes a
  kill". A dead player cannot normally kill, so this matches only two cases:
  1. A mutual kill in the same second (`A->B`, then `B->A`). Here the other clause,
     `previous.Username == a.Target`, matches too, so this clause adds nothing.
  2. A posthumous gadget or grenade kill by a dead player. That is not a trade by any definition.

  On the corpus below this clause never matched on its own (`A_c1only = 0`). It is wrong but has
  had no effect so far.

### R6Stats (the consumer)

`api/r6stats/services/facts.py` on R6Stats `origin/main`:

```python
A kill is traded when someone kills its killer, no earlier than the kill and no later than
`window_s` after it (the bound is inclusive), and the avenger is on the victim's team.
```

- `_find_trades(kills, clock, window_s, team_of)` runs over `kills`, which are all non-teamkill
  Kill events sorted by `(elapsed, seq)`. It is non-adjacent.
- It produces the per-round facts `trade_kill`, `traded_death`, `kill_traded_back`,
  `opening_kill_traded` and `opening_death_traded`.
- `api/r6stats/config.py` sets `TRADE_WINDOW_S: float = 5.0`.
- Spec `docs/superpowers/specs/2026-10-18-r6stats-design.md`, §8.2 Trade row: "a death is
  **traded** if a teammate kills the killer within **5 s** of game clock (my call; configurable
  constant `TRADE_WINDOW_S`; r6-dissect's own helper uses 3 s ...)".
- The stats engine (plan `2026-10-18-r6stats-03-stats-engine.md`) aggregates these facts into
  `trade_kills`, `traded_deaths`, `kills_traded_back`, `trade_rounds_pct`, KOST (`... or
  traded_death`) and the rating's `entry_term(untraded_ok, untraded_od, rounds)`.
- `parser_contract.py` reads `elapsedSeconds` from the fork and derives it from the countdown for
  upstream output. The facts layer therefore works the same on both parsers. A parser-side trade
  flag would exist on the fork only.

## 3. Evidence (runs)

Corpus: the user's 5 real matches (42 rounds) and the 11 single-round fixtures.

### Run 1: bd6ccbe probe

Throwaway test `dissect/zz_trades_probe_test.go` in the scratch worktree; output saved in
`scratchpad/wt-14-tools/probe-bd6ccbe.txt`. On the real matches:

```
TOTAL Trades()=25 bogus(teamkill or non-kill first)=3 nonadjacent3s=28 nonadjacent5s=38
  BOGUS DefuserDisableStop player03->@205 | Kill player54->player03@206
  BOGUS DefuserDisableStop player65->@193 | Kill player54->player65@193
  BOGUS DefuserPlantStop player22->@143 | Kill player76->player22@143
```

At bd6ccbe, 3 of the 25 pairs are bogus: a Stop event is paired with a kill. PR #3 C2 already
fixes this.

### Run 2: health-dbno comparison

1. Built the CLI at c608cf8.
2. Dumped JSON for every match.
3. Ran `scratchpad/wt-14-tools/trades_cmp.py`. It runs A, the health-dbno `Trades()` logic, and
   B(W), a re-implementation of R6Stats `_find_trades`, over `matchFeedback`.

| metric (42 real rounds, 312 kills, 2 teamkills) | count |
|---|---|
| A: health-dbno `Trades()` pairs | 23 |
| A pairs matched only by the `previous.Target == a.Username` clause | 0 |
| B, 3 s (non-adjacent) traded kills | 28 |
| B, 3.5 s | 28 (same as 3 s: integer clock) |
| B, 5 s (R6Stats today) | 38 |
| B, 10 s | 62 |
| in B 3 s but missed by A (adjacency) | 5 |
| in A but not in B 3 s | 0 |
| B 5 s self-avenge (mutual same-second kills) | 4 |

Fixtures: A 6, B 3 s 6, B 5 s 9.

### Hand-inspected cases

- **Interleaved trades** (`Match-2026-10-18_18-19-54`, round 4). Feed: `player75->player41@137`,
  `player22->player09@139`, `Mako->player75@140`, `Ren->player22@142`. These are two real 3 s
  trades and A finds neither. B finds both.
- **Double trade** (same match, round 3). Feed: `player22->drew@167`,
  `player22->player75@168`, `Ren->player22@169`. Ren trades both deaths. A credits only the
  adjacent one. A teamkill (`player75->player84@167`) sits in between; health-dbno skips it correctly.
- **5 s-only examples** (4 to 5 s gaps). Two of them: `player79->player41@81` answered by
  `player65->player79@85`, and `player43->player22@66` answered by `player74->player43@71`.
  Both are plausible refrags.
- **Mutual kills** (`19-08-03` r0: `player20<->player74@66`; `21-07-35` r2:
  `player41<->player06@157`). R6Stats marks both kills of each pair as traded. Each victim gets a
  traded death and a trade kill, because `avenger_team == team_of(victim)` holds when the avenger
  *is* the victim. See section 6.

**Conclusion.** The adjacency bug is real: 5 of 28 trades (18 %) are missed at a matching 3 s
window. It only affects the xlsx export, though. The trades R6Stats shows already come from a
correct non-adjacent definition.

## 4. Definition (documented for the record)

Standard esports definition, adopted by R6Stats spec §8.2:

> A non-teamkill kill K (killer X, victim V, at time t) is **traded** if a non-teamkill kill on X by
> a player on V's team happens at time t' with 0 <= t' - t <= W, on the monotonic action-phase
> clock (`elapsedSeconds`). The avenger records a **trade kill**, V a **traded death**, X a **kill
> traded back**.

Window W = 5 s. This is R6Stats' existing configurable `TRADE_WINDOW_S`, and also the common
CS/HLTV-style value.

| window | where it is used | effect |
|---|---|---|
| 3 s | our parser helper | |
| 3.5 s | defiancer fork | the same as 3 s on our integer-second clock |
| 10 s | some community tools | inflates trades heavily: 62 against 38 on the corpus |

Kill time is the confirmed-kill event, not the earlier DBNO. A down that is finished later counts
from the kill. This keeps the definition independent of DBNO detection quality.

## 5. Approaches considered

1. **Port the fork: parser computes `traded` per kill and `trades`/`tradedDeaths` per player
   (rejected).**
   - Duplicates R6Stats facts with a fixed window that differs from the site's.
   - Fork-only, so R6Stats would still need its own path for upstream output.
   - Changing the window would mean re-parsing every replay, where R6Stats can recompute from its
     stored `round_events` and facts.
   - Adds golden-file churn across every fixture.
   - Goes against the fork's direction, "raw stats before positions": the parser emits observed
     facts, consumers derive metrics.
2. **Parser becomes the single source of truth and R6Stats reads its flag (rejected).** Same costs
   as option 1, plus a contract change in `parser_contract.py` and a regression for upstream-format
   input. R6Stats' definition is already the better one.
3. **R6Stats stays the source of truth; the parser emits only the timeline (chosen, no work
   needed).** Our parser already writes `elapsedSeconds`, `teamKill` (health-dbno) and the
   kill/DBNO feed, which is everything R6Stats needs.
4. **Optional low-value cleanup, not filed:** rewrite `Reader.Trades()` (xlsx only) to the section 4
   definition with W = 3 to keep the sheet's meaning. That means non-adjacent, team-aware, and
   dropping the `previous.Target == a.Username` clause. It is about 20 lines plus a unit test. Not
   filed: nobody in our stack reads the xlsx, and PR #3 already removed the bogus pairs. If the
   xlsx ever gets a user, do it on top of health-dbno (`dissect/stats.go` `Trades`, tests next to
   `TestTrades_SkipsTeamKill` in `dissect/stats_test.go`).

## 6. Found along the way (for the dispatcher, outside this item)

**R6Stats self-avenge on mutual kills.** In `facts.py` `_find_trades` the check
`avenger_team != team_of.get(kill.target ...)` lets the victim himself count as the avenger. In a
same-second mutual kill (A->B, B->A), both kills are then traded: both players get `trade_kill`,
`traded_death` and `kill_traded_back`. That happened 4 times in 42 rounds.

The spec text says "a **teammate** kills the killer", so the code is looser than the spec. Whether a
1-for-1 mutual kill should count as a trade is a policy call. If it should not, add
`answer.username != kill.target` to the guard. This is an R6Stats nit, not an r6-dissect item.

## 7. What would change the verdict

- A consumer that needs trades from the parser directly: someone besides R6Stats reading our JSON,
  or the xlsx gaining real users. Then do approach 4 (or 1 with the section 4 definition and
  W = 5, team-aware) on top of health-dbno.
- R6Stats moving its stats logic into the parser as policy, which contradicts the current design.
