# Fork survey (2026-10-21)

A survey of every fork of redraskal/r6-dissect, looking for features and fixes worth
bringing into this fork. Verified against `main` at bd6ccbe.

## Method

- Listed the 33 direct forks plus forks of forks (ElyasYassin, Animuxs/r6-replay-lab,
  Gipson62, ArnaudCabello). For each branch we counted the commits upstream doesn't have.
  Branches that only copy upstream's `feat/imhex`, `feat/loadout` or `feat/movement`
  were left out.
- Fetched every fork with unique commits into one mirror. Fork code was read, never run.
- Seven agents reviewed the forks in parallel. The headline claims were then
  re-checked against our code.
- Not reachable: Sehouz/r6-dissect and onlYtheLAST/r6-dissect (404).

## Forks reviewed

| Fork | What it is | Outcome |
|---|---|---|
| Animuxs/r6-stats-app (and atrejo26/r6-dissect-stats, the same code) | Collegiate stats app on a 2025-09 upstream base | Pattern search, memory, abandoned rounds, late joiners, scoreboard entity link, map IDs |
| cliffside-git | Y11S2/Y11S3 compatibility | Player-id marker detection (Y11S2 build 9734089) |
| aa-sasha | Production fork for r6tv.online | Alpha03 uiID marker; listener-EOF resilience; header-only info mode |
| defiancer6s-hub | Engine for analysing lost rounds | Crash on incomplete match folders; per-player trades |
| nicktstewart | Heuristic Y11S1/Y11S2 work | Map IDs; listener EOF; roleName fallback; labelled Y11S1 replays |
| lumina-r6/siege-dissect | Stats for coaches | Weapon ID per kill; damage from HP |
| ArnaudCabello (fork of ImAAhmad) | Positions, reinforcements, viewer | Reinforcement counter `67DE20F8`; position-marker hypotheses |
| ItaloRossySantiago, r6tenmans, Gipson62, noughname #121 | Small patches | Nothing new |
| ImAAhmad | Source of upstream #126 | Nothing after the 58062b0 commit we merged |
| Talent-League-EU | Upload and deploy wrapper; Go code never changed | Bans entered by hand at upload (idea for R6Stats) |
| DarHa1531212, BadCoder1337, developer-ken | Old or superseded | Nothing |
| ElyasYassin (fork of ours) | Author of PR #12 | Nothing beyond PR #12 |

## Corrections to the survey

- Two agents said `Operator.Role()` panics on unknown operators. It doesn't on our
  `main`: it returns `""` and `deriveTeamRoles` logs a warning. The fork fixes for this
  (LookupRole, an Unknown role) are not needed.
- Gaps no fork closes: banned operators (#85/#127 upstream) and language-independent
  site IDs (#56).

## Items

Each item has a plan (`docs/superpowers/plans/2026-10-21-fork-port-NN-*.md`) or an
evaluation note (`docs/research/2026-10-21-fork-eval-NN-*.md`), and a GitHub issue
labelled `fork-port`. The issue numbers are listed in the index below once filed.

Order notes: land #23/#24 (crashes) and #31 (listener EOF) first; R6Stats#17 must ship
before R6Stats picks up #19; #21/#22/#25/#27 wait on PR #3 or PR #10, as each plan says.

| # | Item | Verdict | Issue(s) | Doc |
|---|---|---|---|---|
| 01 | Incomplete match folder crashes the CLI; a `.rec` cut before its first zstd frame panics | FILE (bug) | #23, #24 | plans/…-fork-port-01-incomplete-match-crash.md |
| 02 | Y11S2 build 9734089 player-id/uiID markers | FILE (bug, research) | #34 | plans/…-fork-port-02-y11s2-alpha03-markers.md |
| 03 | A listener EOF drops the rest of the round | FILE (bug) | #31 | plans/…-fork-port-03-listener-eof-resilience.md |
| 04 | Missing map IDs (Villa/Tower/Coastline Y10, Calypso Casino) | FILE | #17 | plans/…-fork-port-04-missing-map-ids.md |
| 05 | Pattern scanner misses matches at worker-block edges (latent); 3–5× faster scan | FILE | #33 | plans/…-fork-port-05-pattern-search-boundaries.md |
| 06 | Round buffers kept alive: a 10-round match peaks at about 1.5 GB | FILE (bug) | #32 | plans/…-fork-port-06-zstd-decode-memory.md |
| 07 | Undecided round awarded to team 1 | FILE (bug) | #19, R6Stats#17 | plans/…-fork-port-07-abandoned-round-winner.md |
| 08 | Late joiner gets no scoreboard row | FILE (bug) | #18 | plans/…-fork-port-08-late-joiner-scoreboard.md |
| 09 | Scoreboard entity link `EB219B38` replaces the zip mapping | FILE (bug) | #26 | plans/…-fork-port-09-scoreboard-entity-link.md |
| 10 | Reinforcement counter `67DE20F8`; who placed each one | FILE | #21, #22 (research) | plans/…-fork-port-10-reinforcement-counter.md |
| 11 | Weapon per kill; reconciliation pass order. A weapon veto was rejected | FILE | #28, #35 | plans/…-fork-port-11-weapon-per-kill.md |
| 12 | Damage taken from the health component | FILE (research) | #25 | plans/…-fork-port-12-damage-from-health.md |
| 13 | Unknown operator named from the header roleName. The operators CI check works (no issue) | FILE / NO-ISSUE | #27 | plans/…-fork-port-13-operator-rolename-fallback.md |
| 14 | Per-player trades in the parser | NO-ISSUE (R6Stats owns trades) | — | research/…-fork-eval-14-per-player-trades.md |
| 15 | Y11S1 regression corpus (manifest + fetch, not committed) | FILE | #29 | plans/…-fork-port-15-y11s1-fixtures.md |
| 16 | R6Stats manual bans (already shipped): validation and entry from the upload screen | FILE | R6Stats#15, R6Stats#16 | R6Stats `docs/superpowers/plans/2026-10-21-manual-ban-entry.md` |

Found along the way:
- #30: a zero-event round emits `"matchFeedback": null`, so R6Stats rejects the upload.
- R6Stats#18: in a mutual kill, R6Stats counts each victim as their own avenger.

Leads recorded in the plans but not filed:
- a late joiner is always put on team 1 (plan 08, Risks);
- reordering win conditions (KilledOpponents before DefusedBomb) would relabel 8/8 DefusedBomb rounds (plan 07);
- R6Stats should turn `skippedRounds` and `warnings` into upload warnings once #23 and #31 land.
