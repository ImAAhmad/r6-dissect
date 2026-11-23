# Run log: fork-port lane (r6-dissect), 2026-10-22

Wave-ship lane that implements the 2026-10-21 fork-survey issues. Repo `prestonwillis10/r6-dissect`.

- **Track:** `track/fork-ports`, cut from `origin/main` at `1437695`. The plans branch `docs/fork-port-plans` (PR #36) is merged in, so the spec is on the track (`1352567`).
- **Spec:** `docs/research/2026-10-21-fork-survey.md` (the index) and `docs/superpowers/plans/2026-10-21-fork-port-NN-*.md`, with prototypes in `docs/research/fork-port-prototypes/`.
- **Companion lane:** R6Stats `track/fork-ports`, run log `docs/runs/2026-10-22-fork-ports.md` in that repo (R6Stats #15–#18).
- **Mode:** autonomous slot merges into the track. The landing PR (track → `main`) is gated on the operator's go, as with the rec-timeline and ci-linux landings. The operator gave that go on 2026-10-22: run `/deduce-pr` on the landing PR until a merge verdict, then merge (re-sync with `main` first).
- **Models:** Sonnet implementers (Opus for #26 and #33, which need judgment beyond the plan). Opus reviewers via `/deduce-pr`, named `rev-<PR#>`.

## Gate zero (2026-10-22)

`origin/main` moved from `bd6ccbe`, where the plans were verified, to `1437695`. That is PR #16 (`docs/contributing`).

```
$ git diff --stat bd6ccbe origin/main
 .github/pull_request_template.md | 21 +++++++++++++++++++
 CONTRIBUTING.md                  | 44 ++++++++++++++++++++++++++++++++++++++++
 README.md                        |  3 +++
```

No Go file changed, so every code anchor the plans cite at `bd6ccbe` holds at `1437695`. The reproductions recorded in the plans (PR #36) carry over unchanged. Every issue is OPEN, and no open PR other than #36 references #17–#35.

| Issue | Claim | Status at 1437695 |
|---|---|---|
| #23/#24 | nil round panics / empty body panics | CONFIRMED (code identical; reproduced in plan 01) |
| #17 | 4 map IDs missing | CONFIRMED (`dissect/header.go` has no `409325881472`) |
| #29 | no Y11S1 / match-level acceptance | CONFIRMED |
| #32 | round buffers kept alive | CONFIRMED (code identical; measured in plan 06) |
| #33 | scanner drops boundary matches | CONFIRMED (latent; plan 05 tests) |
| #34 | 9734089 markers | CONFIRMED (simulated; no real replay exists — `research`) |
| #19 | undecided round → team 1 | CONFIRMED |
| #30 | zero-event round → `matchFeedback: null` | CONFIRMED (reproduced 2026-10-21 on nicktstewart match2 R05) |
| #31 | listener EOF drops rest of round | CONFIRMED |
| #18 | late joiner has no scoreboard row | CONFIRMED |
| #26 | zip mapping shifts after a leaver | CONFIRMED |
| #28 | weapon per kill | CONFIRMED |
| #21/#22 | reinforcements | DEFERRED: needs PR #10 (`track/rec-timeline`) on main |
| #25 | damage taken | DEFERRED: needs PR #3 (`fork/health-dbno`) on main |
| #27 | roleName fallback | DEFERRED: needs PR #3's 10th-player fix (`a17b9f4`) |
| #35 | reconcile pass order | DEFERRED: same function as external PR #12; operator's call on #12 first |

**Policy constraint from PR #16 (`CONTRIBUTING.md`):** "only commit replays you recorded or have permission to share". Plans 07 and 09 propose committing fixtures cut from nicktstewart's replays, which are third-party. Those slots use synthetic unit tests instead. Real-replay acceptance runs locally through #29's env-gated corpus, and nothing from it is committed.

## Waves

The graph follows the shared files. `dissect/reader.go` is the hotspot, edited by nine items. Hunks in distinct functions may run in parallel, and every wave after the first is re-validated against the current track before merge (Hard rule 13).

| Wave | Slot | Issue(s) | Plan | Owns (others must not touch) | After |
|---|---|---|---|---|---|
| W1 | S23 | #23, #24 | 01 | `dissect/match.go` read loop + `Data`/`WriteExcel` guards, `main.go` exit policy, `exports/exports.go`, `dissect/error.go`, the guard at the top of `Reader.Read` | — |
| W1 | S17 | #17 | 04 | `dissect/header.go` Map block, `dissect/map_string.go` | — |
| W1 | S32 | #32 | 06 | `dissect/decompress.go` (new), `readChunkedData`/`readNonChunkedData`, `readPlayer`'s DissectID line | — |
| W1 | S29 | #29 | 15 | `dissect/test/acceptance_match_test.go`, `scripts/corpus/` | — |
| W2 | S33 | #33 | 05 | `Reader.Read` worker/scan + sort, `reader.go` import block | S23 |
| W2 | S34 | #34 | 02 | `readPlayer` marker seeks, `dissect/player_markers.go`, `version.go` comment | S32 |
| W2 | S19 | #19 | 07 | `roundEnd` winner/condition (`time.go`), 1vX in `stats.go` | S23 |
| W2 | S30 | #30 | — (issue body) | `NewReader`'s `MatchFeedback` init | S23 |
| W3 | S31 | #31 | 03 | `Read`'s listener loop, `Seek` fast path, `Warnings` (`warning.go`), `round.warnings` in `Data` | S33 |
| W3 | S18 | #18 | 08 | `scoreboardRow` + the four score/assist writers, `deriveTeamRoles` row filter | S19, S17, S34 |
| W4 | S26 | #26 | 09 | `ensureScoreboardMapping` / the entity-link listener | S18 |
| W5 | S35 | #35 | 11 (Part B, 11c) | the reconcile pass order in `reconcileKillsFromScoreboard`; credits closed PR #12's sort and its `break` note | S26 |
| W4 | S28 | #28 | 11 (Part A) | `MatchUpdate` weapon fields (`feedback.go`), `classifyKillWeapons` at the end of `Read` | S31 |

### Results (all slot PRs merged into the track; verdict history in the event log)

| Slot | Issue(s) | PR | Merge commit |
|---|---|---|---|
| S17 | #17 | #37 | `60f6616` |
| S23 | #23, #24 | #38 | `05cf447` |
| S29 | #29 | #39 | `b81ade4` |
| S32 | #32 | #40 | `74de332` |
| S30 | #30 | #42 | `da2264b` |
| S19 | #19 | #43 | `9af84a4` |
| S34 | #34 | #44 | `43a747a` |
| S33 | #33 | #45 | `999e2af` |
| S18 | #18 | #46 | `7e976ae` |
| S27 | #27 | #47 | `f30b152` |
| S25 | #25 | #48 | `8bd9741` |
| S31 | #31 | #49 | `5eab788` |
| S26 | #26 | #50 | `95cdcab` |
| S28 | #28 | #51 | `6491fa7` |
| S35 | #35 | #52 | `e98eb5e` |

Deferred: #21, #22 (need PR #10 on `main`). PR #3 merged to `main` (`7d37745`) and into the track (`24024f6`).

## Worktree ledger

worktree: /home/prest/r6-dissect-worktrees/fork-ports branch=track/fork-ports for=orchestrator
worktree: /home/prest/r6-dissect-worktrees/fp-s23 branch=fp/s23-incomplete-match for=S23
worktree: /home/prest/r6-dissect-worktrees/fp-s17 branch=fp/s17-map-ids for=S17
worktree: /home/prest/r6-dissect-worktrees/fp-s32 branch=fp/s32-decode-memory for=S32
worktree: /home/prest/r6-dissect-worktrees/fp-s29 branch=fp/s29-match-corpus for=S29
worktree: /home/prest/r6-dissect-worktrees/rev-37 branch=(detached 75cce63) for=rev-37
worktree: /home/prest/r6-dissect-worktrees/rev-38 branch=(detached 1a15d3d) for=rev-38
worktree: /home/prest/r6-dissect-worktrees/rev-3-b branch=(detached ce05e79) for=rev-3-b (PR #3)
worktree: /home/prest/projects/R6Stats/r6-dissect-dbno branch=fork/health-dbno for=PR #3 (pre-existing; used for the main merge and the fix worker)
worktree: /home/prest/r6-dissect-worktrees/val branch=(detached) for=orchestrator validation merges
worktree: /home/prest/r6-dissect-worktrees/rev-39 branch=(detached 7bc5821) for=rev-39
worktree: /home/prest/r6-dissect-worktrees/rev-39-r2 branch=(detached 1fea036) for=rev-39 round 2
worktree: /home/prest/r6-dissect-worktrees/rev-40 branch=(detached d21b0c4) for=rev-40
worktree: /home/prest/r6-dissect-worktrees/ci-main-only branch=ci/pr-into-main-only for=PR #41
worktree: /home/prest/r6-dissect-worktrees/rev-40-r2 branch=(detached 3d4ae8e) for=rev-40 round 2
worktree: /home/prest/r6-dissect-worktrees/rev-3-b-r4 branch=(detached 2fbc240) for=rev-3-b round 4
worktree: /home/prest/r6-dissect-worktrees/fp-s33 branch=fp/s33-pattern-scan for=S33
worktree: /home/prest/r6-dissect-worktrees/fp-s34 branch=fp/s34-alpha03-markers for=S34
worktree: /home/prest/r6-dissect-worktrees/fp-s19 branch=fp/s19-undecided-round for=S19
worktree: /home/prest/r6-dissect-worktrees/fp-s30 branch=fp/s30-feedback-array for=S30
worktree: /home/prest/r6-dissect-worktrees/rev-42 branch=(detached 6be291d) for=rev-42
worktree: /home/prest/r6-dissect-worktrees/rev-43 branch=(detached 6b3617d) for=rev-43
worktree: /home/prest/r6-dissect-worktrees/rev-44 branch=(detached 138274f) for=rev-44
worktree: /home/prest/r6-dissect-worktrees/rev-45 branch=(detached a2246f8) for=rev-45
worktree: /home/prest/r6-dissect-worktrees/fp-s31 branch=fp/s31-listener-eof for=S31
worktree: /home/prest/r6-dissect-worktrees/fp-s18 branch=fp/s18-late-joiner for=S18
worktree: /home/prest/r6-dissect-worktrees/fp-s25 branch=fp/s25-damage-taken for=S25
worktree: /home/prest/r6-dissect-worktrees/fp-s27 branch=fp/s27-operator-fallback for=S27
worktree: /home/prest/r6-dissect-worktrees/rev-46 branch=(detached 00383f6) for=rev-46
worktree: /home/prest/r6-dissect-worktrees/rev-47 branch=(detached 015fb76) for=rev-47
worktree: /home/prest/r6-dissect-worktrees/rev-48 branch=(detached 8e883c8) for=rev-48
worktree: /home/prest/r6-dissect-worktrees/rev-47-r2 branch=(detached 3beca42) for=rev-47 round 2
worktree: /home/prest/r6-dissect-worktrees/fp-s26 branch=fp/s26-scoreboard-link for=S26
worktree: /home/prest/r6-dissect-worktrees/rev-49 branch=(detached ce8c0bd) for=rev-49
worktree: /home/prest/r6-dissect-worktrees/rev-50 branch=(detached 81debed) for=rev-50
worktree: /home/prest/r6-dissect-worktrees/fp-s28 branch=fp/s28-weapon-per-kill for=S28
worktree: /home/prest/r6-dissect-worktrees/fp-s35 branch=fp/s35-reconcile-order for=S35
worktree: /home/prest/r6-dissect-worktrees/rev-49-r2 branch=(detached 3724f63) for=rev-49 round 2
worktree: /home/prest/r6-dissect-worktrees/rev-51 branch=(detached a47e4e6) for=rev-51
worktree: /home/prest/r6-dissect-worktrees/rev-51-r2 branch=(detached c802a5d) for=rev-51 round 2
worktree: /home/prest/r6-dissect-worktrees/rev-52 branch=(detached 9618bb6) for=rev-52
worktree: /home/prest/r6-dissect-worktrees/rev-53 branch=(detached 230c43d) for=rev-53 (landing)
worktree: /home/prest/r6-dissect-worktrees/rev-53-r2 branch=(detached 0bc65e5) for=rev-53 round 2

## Event log

- 2026-10-22 gate zero written; track cut (`1352567`); run log `5fdfc3d`.
- 2026-10-22 W1 dispatched: S23, S17, S32, S29 (Sonnet).
- 2026-10-22 operator directives: close external PR #12 (done, comment points at CONTRIBUTING.md and at #35/#26); merge docs PRs (r6-dissect #36 → `389ef14`, R6Stats #19 → `0ea6b0d`); bring PR #3 up to date. #35 is unblocked by #12's closure (credit #12's sort and its `break` note when S35 lands).
- 2026-10-22 PR #3: rev-3 round-2 verdict harvested from the previous session's transcript (`agent-aa162b2d7b15ed0e1.jsonl`): NEEDS-CHANGES at `c608cf8`, F4 BLOCKING FIX-INTRODUCED (`insertByElapsed` tie rule only looks at `at-1`), F5 ADVISORY. `origin/main` merged into `fork/health-dbno` cleanly; build, c-shared, gofmt, vet, tidy and staticcheck PASS.
- 2026-10-22 env: local govulncheck exits 3 on GO-2026-6809 (crypto/x509, go1.27.2; fixed in 1.27.3). CI's `go-version: "1.27"` resolves the patched release, so CI vulncheck is authoritative; the local toolchain is not swapped mid-lane.
- 2026-10-22 S17 → PR #37 (head `75cce63`); base track/fork-ports, worktree clean; ci-settle armed.
- 2026-10-22 PR #37 CI SETTLED PASS at `75cce63`; preflight 14 pass / 3 warn / 0 fail; rev-37 dispatched (Opus).
- 2026-10-22 PR #3: merge of origin/main pushed (`6d8ce77`); -race tests and goldens PASS locally; Sonnet fix worker dispatched for F4/F5 in `/home/prest/projects/R6Stats/r6-dissect-dbno`.
- 2026-10-22 rev-37 round 1 MERGE-AFTER-MECHANICAL (F1 EVIDENCE: fork provenance missing, Deviations said None); body patched (byte-equal read-back), checks `4`/`0`, strict preflight 0 FAIL; round 2 MERGE at `75cce63`; PR #37 merged into the track.
- 2026-10-22 S23 → PR #38 (head `1a15d3d`); CI SETTLED PASS; preflight 16 pass / 1 warn / 0 fail; rev-38 dispatched (Opus).
- 2026-10-22 watchdog fix: transcripts are symlinks, so silence is measured with `stat -L`.
- 2026-10-22 operator: "you have the helm" — landings go through `/deduce-pr` to a MERGE verdict and then merge to main; re-sync with main before each landing (other lanes merge too). Coordination message sent to peer session r6stats-3b.
- 2026-10-22 operator: CI is local — slot pushes carry `[skip ci]` on the head commit; local gates are the evidence; the orchestrator re-runs the full suite at merge; full GitHub CI only on landing PRs and PR #3's fix push. Briefs updated; running implementers told.
- 2026-10-22 host memory incident: `go test -race ./dissect/test` peaked at 16 GB (S29) and 15 GB (S32), swap 27/31 GB. Both killed by pid (S29's by hand, S32's by `lane/memguard.sh`, a lane-scoped 12 GB guard). Rule: implementers run `-race` only on `./dissect ./exports .` with `GOMAXPROCS=2 -p=1`; the full race suite is an orchestrator gate, one at a time.
- 2026-10-22 PR #3: fix worker pushed `a923a40` + `ce05e79` (F4 in `insertByElapsed`, F5 tests + a third pinning test); CI SETTLED PASS at `ce05e79`; body patched (round 2 section, main sync, pseudonyms; byte-equal read-back); strict preflight 0 FAIL; rev-3-b (fresh Opus, delta + final) dispatched in `/home/prest/r6-dissect-worktrees/rev-3-b`.
- 2026-10-22 rev-38 round 1 MERGE at `1a15d3d` (4 ADVISORY: F1 red intermediate commits `33ed51d`/`dc7589d` → slot is squash-merged into the track; F2 `RoundAt` out-of-range panic pre-existing; F3 R6Stats `skippedRounds` consumer unfiled; F4 absolute host path in `skippedRounds[].error`). Local re-validation of #38 merged onto the track (`60f6616`) running in `/home/prest/r6-dissect-worktrees/val` via `lane/gates.sh` (race suite at `-parallel=1`).
- 2026-10-22 S29 → PR #39 (head `7bc5821`, `[skip ci]`); offline preflight 11 pass / 5 warn / 0 fail; rev-39 dispatched (Opus).
- 2026-10-22 operator check-in: slot PRs use merge commits (the attempted squash of #38 was rejected); `[skip ci]` approved; CI to run only on PRs into main → PR #41 (r6-dissect) and R6Stats #23 opened (`ci: run CI only on pull requests into main`). PR #38 merged into the track with `--merge` (`05cf447`).
- 2026-10-22 rev-3-b round 3 MERGE-AFTER-MECHANICAL at `ce05e79` (N1 floor rule unpinned, N2 body names `1437695` not `389ef14`, N3 long comment). Applied by the orchestrator: `f69f093` (two tests; floor-deleted mutant fails both) and `2fbc240` (rewrap); body draft ready; local CI running before push.
- 2026-10-22 rev-39 round 1 MERGE-AFTER-MECHANICAL at `7bc5821` (F1: `matchJSON` collides with #38's type; F2–F8 ADVISORY). Rename applied by the orchestrator (`1fea036`, `[skip ci]`); local CI on track(05cf447)+#39 green (race suite 181 s); rev-39 round 2 charged.
- 2026-10-22 S32 → PR #40 (`d21b0c4`); preflight evidence FAILs fixed in the body (fenced mutation runs, three citations); offline preflight 0 FAIL; rev-40 dispatched.
- 2026-10-22 wave 2 held until PR #3 lands on main (it reshapes `reader.go`/`stats.go`/`header.go`); then main is merged into the track and S33/S19/S30 (+S34 after #40, S25/S27 in W3, S35 in W5) are cut.
- 2026-10-22 PR #39: rev-39 round 2 MERGE-WITH-NITS (F6 evidence nit applied in the body; strict preflight 0 FAIL) → merged into the track (`b81ade4`). Candidates F2+F3 (expected-failure matching) and F4+F5 (fetch.sh symlink/shallow fetch) recorded below.
- 2026-10-22 PR #41 (CI trigger) and R6Stats #23: preflight premises fixed; rev-41 MERGE, rev-r6s-23 MERGE-WITH-NITS (body paren fixed).
- 2026-10-22→27 session paused ~5 days; on resume both `main`s and tracks were unchanged. rev-r6s-21/22 had stalled on background monitors; resumed with foreground-only charges.
- 2026-10-27 PR #41 merged into main (`b9edf5a`); main merged into the track (`22822c7`) so slot PRs no longer start CI (rev-41 F1).
- 2026-10-27 PR #40: rev-40 round 1 MERGE-AFTER-MECHANICAL (F1 pin guard Y8S2-only) → patch applied (`3d4ae8e`, slotID mutant fails the Y11S2 subtest); local CI on `b81ade4`+`3d4ae8e` green (race `dissect/test 73.713s`); round 2 MERGE-WITH-NITS (stale evidence prose fixed, `1d310a8`) → merged into the track (`74de332`). **Wave 1 complete: #37, #38, #39, #40.**
- 2026-10-27 PR #3: round-3 fixes pushed (`f69f093`, `2fbc240`); local CI + GitHub CI green; strict preflight 0 FAIL; rev-3-b round 4 charged.
- 2026-10-27 rev-3-b round 4 MERGE at `2fbc240` → **PR #3 merged into main** (`7d37745`; operator approval for MERGE verdicts; main had not moved past `b9edf5a`). Main merged into the track (`24024f6`): no conflicts; local CI green (race `dissect/test 70.915s`); PR #3's intended golden diffs; acceptance on the user's 5 matches `rounds 42, DBNO 73 (69 credited), Revive 3 (2 credited)`; corpus: the same 5 expected failures, 0 unexpected. #25 and #27 unblocked.
- 2026-10-27 W2 dispatched from `24024f6`: S33 (#33, Opus), S34 (#34), S19 (#19; third-party fixture replaced by a synthetic test; must not edit `knownMatchFailures`), S30 (#30; owns the `knownMatchFailures` edits this wave). Slots run `[skip ci]` pushes and the memory rule.
- 2026-10-27 S30 → PR #42 (`6be291d`; preflight mutation-proof FAIL fixed by pasting the slot log's fenced mutant block); S19 → PR #43 (`6b3617d`; the 21916 score-drop entry is NOT #19's: round 5 is decided 2→3 with a missing scoreboard — relabel at reconciliation); S34 → PR #44 (`138274f`; citation `player.go:10` → `:11` fixed). Offline preflights 0 FAIL. rev-42/43/44 dispatched (Opus). S33 still implementing.
- 2026-10-27 rev-42 MERGE → local CI on #42's head green (race `dissect/test 70.769s`) → #42 merged (`da2264b`). rev-44 MERGE-WITH-NITS (N2 test comment, N4 stale `:63` cite → `9bfa55a`, `b0bd37b`, body patched). rev-43 MERGE-WITH-NITS (F2 README scope → `e307b07`; F3 → candidate 6). S33 → PR #45 (`a2246f8`; `Seek` also rewritten on `bytes.Index`; benchmark scan 80→17 ms solid_snake); preflight 0 FAIL; rev-45 dispatched.
- 2026-10-28 validation-tree incident: the `val` worktree still held #40's uncommitted validation merge, so the first #44 run executed on a stale tree (detected from the `VAL` header, discarded). `lane/val.sh` now refuses a dirty or mid-merge tree and always aborts its merge.
- 2026-10-28 W2 merges, each after `lane/val.sh` on the current track: #44 (`43a747a`, race `dissect/test 65.064s`), #43 (`9af84a4`, `61.850s`), #45 (`999e2af`, `44.347s` — the new scanner). Corpus expected failures unchanged throughout. **Wave 2 complete: #42, #43, #44, #45.**
- 2026-10-28 reconciliation (`ef23785`): the 21916 `score-drop` entry relabelled from #19 to candidate 6 (snapshot rows dropped), with a comment.
- 2026-10-28 W3 dispatched from `fe94907`: S31 (#31, Opus; Task 3 superseded by #45, Task 5 out of scope), S18 (#18), S25 (#25; Task 5 research out of scope), S27 (#27).
- 2026-10-28 S18 → PR #46 (`00383f6`), S27 → PR #47 (`015fb76`; preflight citation FAILs fixed: `dissect/` prefixes, two anchors), S25 → PR #48 (`8e883c8`; its slot log named league players → pseudonymised). Privacy sweep: run log + s19 slot log + PR #43 body named fork-corpus players → pseudonymised (`03ff06c`); brief gained a privacy rule. rev-46/47/48 dispatched.
- 2026-10-28 rev-46 MERGE-WITH-NITS (3 ADVISORY): F1 stale index-keyed state after a non-last drop (pre-existing; root `readPlayerOrder`) and F2 the untested outer `idx < len(Header.Players)` guard → carried into S26's (#26) prompt as acceptance cases; F3 optional (row ID) skipped. Validating on `03ff06c`.
- 2026-10-28 #46 validated on `03ff06c` (race `dissect/test 48.280s`, corpus unchanged) → merged (`7e976ae`).
- 2026-10-28 rev-47 round 1 MERGE-AFTER-MECHANICAL (F1: the operator key moved for every object — a fast path keeps default bytes unless renamed) → applied by the orchestrator `11c9e5c` (+ test; mutant red; RAW-IDENTICAL on the user's matches vs `fe94907`) and `3beca42` slot log; body rewritten; strict preflight 0 FAIL; round 2 charged.
- 2026-10-28 rev-48 round 1 MERGE-AFTER-MECHANICAL: F1 (merge-gating) pushed commit `f717f1f` carries league usernames in the s25 slot log's history — merge commits would carry it into the track; F2 the pseudonym sweep mangled a parenthetical; F3–F6 ADVISORY (empty-role fallback, README caveat, match total undercount, pseudonym-key exposure) → candidates. F2 fixed and the two slot-log commits squashed locally (`307ff04`; history greps 0/0; code diff vs `8e883c8` empty). **The force-push (`--force-with-lease=…:8e883c8`) was DENIED by the auto-mode classifier [Git Destructive]**; not worked around. #48 HELD pending the operator: allow the force-push, squash-merge this one PR, or merge as is.
- 2026-10-28 W4 part 1 dispatched: S26 (#26, Opus) from `7e976ae`, carrying rev-46 F1/F2 as acceptance cases; third-party fixture replaced by a synthetic test + local corpus check.
- 2026-10-28 operator: "PR #48 merge approved" → merged as is (no force-push; `f717f1f` stays in history by the operator's call). F2 carried as a fast-forward commit `e1b490c` (check `0`; code diff vs `8e883c8` empty); validated on `7e976ae` (race `dissect/test 72.925s`) → #48 merged (`8bd9741`).
- 2026-10-28 rev-47 round 2 MERGE at `3beca42` (F1 fixed: raw-identical on the user's matches and 11 fixtures; F5 advisory latent op-0 swap guard). Validating on the track with #48.
- 2026-10-28 #47 validated on `8bd9741` (race `dissect/test 87.578s`, corpus unchanged) → merged.
- 2026-10-28 S31 → PR #49 (`ce8c0bd`; preflight 0 FAIL, 3 claim WARNs left for round 1). 27 mutants reported; candidate C1 in its body (default log level hides the Warn line).
- 2026-10-28 #49 validated on `f30b152` (race `dissect/test 79.031s`, `exports 3.772s`; no `.rec.json` churn, only the PR's own test files staged; corpus `knownMatchFailures` unchanged). Awaiting rev-49.
- 2026-10-28 S26 → PR #50 (`81debed`; preflight citation FAIL — two lines under one anchor — fixed in the body, 0 FAIL). Removes both 47572 `knownMatchFailures` entries (score-drop and the flaky nondeterminism). Candidate: health/defuser indices not moved on a `deriveTeamRoles` drop.
- 2026-10-28 rev-49 round 1 NEEDS-CHANGES: F1 BLOCKING (a failed `readPlayer` now keeps the round, `deriveTeamRoles` drops the Operator-0 entry, and index-keyed state is not remapped → 9 of 9 other players carry another's stats; CLASS of 9 sites in `reader.go`); F2 MECHANICAL (no test that EVERY round carries its own warning); F3–F5 ADVISORY (default-level stderr silence = author's C1; per-tick `readY7Time` warnings on pre-Y8S1; `readAtkOpSwap` keeps a swap and also warns). Sequencing: the remap is #50's mechanism (S26 remaps 6 of 9), so #50 owns it — rev-50 told to judge the 3 unmoved sites; #49 merges the track after #50 and adds rev-49's F1/F2 tests.
- 2026-10-28 W4 part 2 dispatched: S28 (#28, Opus) from `f30b152` — not waiting on #49 (the coupling is one call at the tail of `Read`; the orchestrator resolves it at merge); rev-47's single-`MarshalJSON` note carried.
- 2026-10-28 rev-50 round 1 MERGE-WITH-NITS (4 ADVISORY): F1 the 3 unmoved sites (`healthState`, `healthTransitions[].player`, `defuserRun.playerIndex`) are empty at `deriveTeamRoles` on 99 rounds → no reachable wrong output (author's candidate; its "#48 (open)" was stale); F2 zip-vs-link chosen per call, not per round (unreachable: first link precedes the first player packet on 88 of 88 Y10S4+ rounds); F3/F4 body prose. Nits applied by the orchestrator: slot log `7695269`, body (stale #48, the ownerless-link form, local numbering note; three `--strict-claims` count shapes rephrased) → preflight 0 FAIL. Validating on `f30b152`.
- 2026-10-28 rev-50 re-judge with #49 in (track+#49+#50 `112201b`): F1 stays ADVISORY — rev-49's injection over 88 Y10S4+ rounds gives a mid-list drop in 52 and stale state at `deriveTeamRoles` in 0; #50's 6-site remap takes other-player stat mismatches from 52 rounds (without #50) to 1. New F5 ADVISORY (owner #49 / S35): a lost player record still changes others' output through name-based fallbacks — `reconcileKillsFromScoreboard` skips a kill whose target is unknown (14012 R02: player90 kills 1 want 0, player74 1 want 2) and `startDefuserRun`'s `lastAliveDefuserPlayer` fallback credits the plant to another player (32000 R02). Carried into rev-49's delta round. Verdict unchanged.
- 2026-10-28 #50 validated on `f30b152` + nit head `7695269` (race `dissect/test 405.647s` under load; corpus: only the 21916 candidate-6 entry left, green) → merged.
- 2026-10-28 W5 dispatched: S35 (#35, Opus) from `95cdcab`; folds in closed PR #12's sort + determinism test (expectation corrected to `U1>E1 U2>E2`), crediting @ElyasYassin; `targetIdx < 0` skip (rev-50 F5) left to #49's delta round.
- 2026-10-28 #49 fix round by the orchestrator (Opus): track merged in (`521459a`, brings #50's remap) + rev-49's F1/F2 tests (`3724f63`); F1 test green with #50, red with `movePlayerIndices` removed; F2 red under first-round-only warnings. Body: round-2 bullets + mutation output; 5 citations re-pointed after the merge, 2 exit-result claims rephrased → preflight `--strict-claims` 0 FAIL. Delta round to rev-49 with rev-50 F5 as context; validating.
- 2026-10-28 #49 `3724f63` validated on `95cdcab` (race `dissect/test 280.459s`, `exports 8.476s`; no `.rec.json` churn; corpus: only the 21916 candidate-6 entry). Awaiting rev-49 round 2.
- 2026-10-28 S28 → PR #51 (`a47e4e6`; preflight FAILs fixed in the body: 10 citations without `dissect/`, then 5 anchors re-pointed to the cited line's code → 0 FAIL). Goldens: 11 files, 78 kills (66 primary / 7 secondary / 5 gadget); corpus + user matches 539 kills, 539 `weaponID`, 536 `weaponSlot`.
- 2026-10-28 rev-49 round 2 MERGE-AFTER-MECHANICAL NO-RE-REVIEW: F1 FIXED (by #50's remap via the merge; `movePlayerIndices` removed → red), F2 FIXED, F3–F5 ADVISORY unchanged; F7 MECHANICAL FIX-INTRODUCED (the new test's comment overclaims); F6 = rev-50's F5, ADVISORY PRE-EXISTING-AT-ROUND-1 (events whose subject is the lost player fall back; no unrelated player's data changes: 0 rounds). Applied F7 in `19f421f`; NO-RE-REVIEW check — expected `0`, actual:
  ```
  grep-actual: 0  expected: 0
  ```
  Match → merged #49 with no resume (track unchanged at `95cdcab` since the `3724f63` validation; the delta is a test comment).
- 2026-10-28 #51 validated on `95cdcab` (race `dissect/test 244.807s`; 11 goldens staged from the PR; corpus unchanged) — stale after #49 merged (`5eab788`); re-validating on `5eab788`.
- 2026-10-28 #51 re-validated on `5eab788` (with #49; auto-merge of `reader.go` clean; race `dissect/test 210.736s`, `exports 7.797s`; corpus unchanged). rev-51 round 1 MERGE-AFTER-MECHANICAL: F1 MECHANICAL CLASS (README feedback table has no rows for `weaponID`/`weaponSlot`), F2 ADVISORY (unit test ID below 2^32: a 4-byte read survives). Applied by the orchestrator: `3df1e3b` README rows, `c802a5d` ID 395972905309 (4-byte mutant red); greps `2` / `1` as expected; body round-2 section → preflight `--strict-claims` 0 FAIL. Delta round to rev-51.
- 2026-10-28 rev-51 round 2 MERGE at `c802a5d` (F1/F2 FIXED, no FIX-INTRODUCED; README claims checked against the code). Track unchanged at `5eab788` since the validation; the delta is README + one test literal → merged #51.
- 2026-10-28 S35 → PR #52 (`9618bb6`; preflight citation FAIL — two lines under one anchor — fixed in the body → 0 FAIL). No output change on the real replays (over/under counts per round are 0/0 or 1/1 everywhere after #50); PR #12's sort folded in, credited to @ElyasYassin.
- 2026-10-28 #52 validated on `6491fa7` (race `dissect/test 87.907s`, `exports 3.912s`; no golden churn; corpus unchanged). Awaiting rev-52.
- 2026-10-28 rev-52 round 1 MERGE at `9618bb6` (F1 ADVISORY pre-existing: the zip path at `dissect/scoreboard.go` maps two sbIDs to one player when `readPlayerOrder` repeats one, so `expectedKills` depends on map order; not observed, 0 of 88 rounds). Second guard site and the M03d/M04a–c equivalence proved by a 200000-round differential; 3-over/3-under pass-order case correct at head. → merged #52. All slot PRs merged.
- 2026-10-28 Stage 6: main has no commit the track lacks (merge-base `7d37745`); landing gates at `d66418b` green (race `dissect/test 195.207s`, `exports 5.569s`; corpus: candidate-6 entry only; user-match acceptance 5 dirs PASS). Landing PR #53 opened at `230c43d`; closing refs = 17,18,19,23–35 (16); preflight 0 FAIL; GitHub CI armed (`ci-settle.sh`); fresh integration reviewer rev-53 dispatched.
- 2026-10-28 GitHub CI on #53 head `230c43d`: run 40646286918 SETTLED PASS (lint, test 1.26, test 1.27, vulncheck; 305s). Body updated; full preflight (with CI) 0 FAIL.
- 2026-10-28 rev-53 (landing) round 1 MERGE-AFTER-MECHANICAL: F1 EVIDENCE `Closes #34` premature (its acceptance needs a real 9734089 match; `research` label) → `Refs #34`; F2 MECHANICAL CLASS README lacks `skippedRounds`, the `roleName` operator naming and `matchFeedback: []`; F3 MECHANICAL CLASS (#39 × #49) the real-replay acceptance tests ignore `warnings`; A1 stale corpus docs, A2 `Closes #25` ends Task 5 tracking (desk's call; candidate 8), A3 Summary #26 row overstates the remap, A4 the round JSON shape is copied by hand on 4 write paths (PR #10's carry must include `exports/exports.go`). Applied by the orchestrator on the track: README + corpus README, `kindWarnings` + `TestCheckMatchJSON_Warnings` (mutant red) + a `Warnings` check in `TestAcceptance_Replays`; body: `Refs #34`, #34 row marked simulated, #26 row narrowed, observation 4 and candidate 20 for A4.
- 2026-10-28 #53 round-2 head `0bc65e5`: GitHub CI run 40650548559 SETTLED PASS (246s); body `Refs #34` cleared the #34 closing link (graphql: 17,18,19,23–33,35); full preflight `--strict-claims` 0 FAIL. Awaiting rev-53 round 2.
- 2026-10-28 rev-53 round 2 MERGE-AFTER-MECHANICAL NO-RE-REVIEW: F1, F3, A1 FIXED; F2 FIXED-BUT-INTRODUCED (R2-F1 MECHANICAL: the new README sentence calls a frame-boundary cut skipped, but it is undetected); R2-A1–A3 ADVISORY prose (the #26 row names the defuser index wrongly, candidate 20 counts the write paths wrong, the body candidates differ from the committed run log); C1 the #38 row has the same frame-boundary overclaim. Applied (README, body, run log) and checked — expected beside actual:
  ```
  R2-F1  grep -c 'empty, truncated or unreadable' README.md        expected 0  actual 0
  R2-F1  grep -c 'frame boundary' README.md                       expected 1  actual 1
  R2-A1  body | grep -c 'the health and defuser indices are candidate 13'  expected 0  actual 0
  R2-A2  grep -c 'copied by hand on four write paths' run log     expected 0  actual 0
  R2-A2  body | grep -c 'copied by hand on four write paths'      expected 0  actual 0
  C1     body | grep -c 'An empty, truncated or unreadable round file is skipped'  expected 0  actual 0
  ```
  All match → merge with no resume once CI on the new head is green.

## Findings

finding: PR=48 round=1 tier=MECHANICAL origin=EVIDENCE kind=other author-catchable=yes (league names in a pushed commit's history)
finding: PR=47 round=1 tier=MECHANICAL origin=IN-DIFF kind=other author-catchable=yes (unnecessary JSON key move)
finding: PR=46 round=1 tier=ADVISORY origin=PRE-EXISTING kind=other author-catchable=no (stale index-keyed state; routed to #26)
finding: PR=42 round=1 tier=ADVISORY origin=EVIDENCE kind=other author-catchable=no
finding: PR=43 round=1 tier=ADVISORY origin=IN-DIFF kind=prose author-catchable=yes (README scope)
finding: PR=44 round=1 tier=ADVISORY origin=EVIDENCE kind=prose author-catchable=yes (stale cite)
finding: PR=45 round=1 tier=ADVISORY origin=IN-DIFF kind=other author-catchable=no (observations only)
finding: PR=39 round=2 tier=ADVISORY origin=EVIDENCE kind=prose author-catchable=yes
finding: PR=40 round=1 tier=MECHANICAL origin=IN-DIFF kind=vacuous-test author-catchable=yes (pin guard never ran Y10S4+ listeners)
finding: PR=40 round=2 tier=ADVISORY origin=EVIDENCE kind=prose author-catchable=yes
finding: PR=41 round=1 tier=ADVISORY origin=EVIDENCE kind=other author-catchable=no (track keeps the old trigger until main is merged in)
finding: PR=3 round=3 tier=MECHANICAL origin=FIX-INTRODUCED kind=vacuous-test author-catchable=yes
finding: PR=3 round=3 tier=MECHANICAL origin=FIX-INTRODUCED kind=false-claim author-catchable=yes
finding: PR=39 round=1 tier=MECHANICAL origin=IN-DIFF kind=other author-catchable=no (cross-PR identifier collision)
finding: PR=37 round=1 tier=MECHANICAL origin=EVIDENCE kind=false-claim author-catchable=yes
finding: PR=38 round=1 tier=ADVISORY origin=IN-DIFF kind=other author-catchable=yes (red intermediate commits)
finding: PR=38 round=1 tier=ADVISORY origin=PRE-EXISTING kind=other author-catchable=no
finding: PR=38 round=1 tier=ADVISORY origin=EVIDENCE kind=other author-catchable=no
finding: PR=38 round=1 tier=ADVISORY origin=IN-DIFF kind=other author-catchable=yes
finding: PR=49 round=1 tier=BLOCKING origin=PRE-EXISTING kind=sibling author-catchable=yes
finding: PR=49 round=1 tier=MECHANICAL origin=IN-DIFF kind=vacuous-test author-catchable=yes
finding: PR=49 round=1 tier=ADVISORY origin=IN-DIFF kind=other author-catchable=yes
finding: PR=49 round=1 tier=ADVISORY origin=IN-DIFF kind=other author-catchable=no
finding: PR=49 round=1 tier=ADVISORY origin=PRE-EXISTING kind=other author-catchable=no
finding: PR=50 round=1 tier=ADVISORY origin=PRE-EXISTING kind=sibling author-catchable=yes
finding: PR=50 round=1 tier=ADVISORY origin=IN-DIFF kind=other author-catchable=no
finding: PR=50 round=1 tier=ADVISORY origin=EVIDENCE kind=prose author-catchable=yes
finding: PR=50 round=1 tier=ADVISORY origin=EVIDENCE kind=prose author-catchable=yes
finding: PR=50 round=1 tier=ADVISORY origin=EVIDENCE kind=sibling author-catchable=no
finding: PR=49 round=2 tier=MECHANICAL origin=FIX-INTRODUCED kind=false-claim author-catchable=yes
finding: PR=49 round=2 tier=ADVISORY origin=PRE-EXISTING kind=sibling author-catchable=no
finding: PR=51 round=1 tier=MECHANICAL origin=IN-DIFF kind=prose author-catchable=yes
finding: PR=51 round=1 tier=ADVISORY origin=IN-DIFF kind=vacuous-test author-catchable=yes
finding: PR=52 round=1 tier=ADVISORY origin=PRE-EXISTING kind=other author-catchable=no
finding: PR=53 round=1 tier=MECHANICAL origin=EVIDENCE kind=false-claim author-catchable=yes
finding: PR=53 round=1 tier=MECHANICAL origin=IN-DIFF kind=prose author-catchable=yes
finding: PR=53 round=1 tier=MECHANICAL origin=IN-DIFF kind=vacuous-test author-catchable=no
finding: PR=53 round=1 tier=ADVISORY origin=IN-DIFF kind=prose author-catchable=yes
finding: PR=53 round=1 tier=ADVISORY origin=EVIDENCE kind=other author-catchable=no
finding: PR=53 round=1 tier=ADVISORY origin=EVIDENCE kind=false-claim author-catchable=yes
finding: PR=53 round=1 tier=ADVISORY origin=PRE-EXISTING kind=sibling author-catchable=no
finding: PR=53 round=2 tier=MECHANICAL origin=FIX-INTRODUCED kind=false-claim author-catchable=yes
finding: PR=53 round=2 tier=ADVISORY origin=FIX-INTRODUCED kind=false-claim author-catchable=yes
finding: PR=53 round=2 tier=ADVISORY origin=FIX-INTRODUCED kind=false-claim author-catchable=yes
finding: PR=53 round=2 tier=ADVISORY origin=EVIDENCE kind=prose author-catchable=yes

## Candidates

1. **match: `MatchReader.RoundAt` panics on an out-of-range index instead of returning `ErrInvalidFile`** — from PR #38 rev-38 F2. Observed in a scratch copy of `1a15d3d`: `m.RoundAt(2)` on a 2-file folder → `panic: runtime error: index out of range [2] with length 2`. `dissect/match.go` `RoundAt` indexes `if m.rounds[i] == nil {` before `read()` checks `if i < 0 || i >= len(m.paths) {`. Not reachable from the CLI or exports (they call `FirstRound` only). Acceptance: a unit test calling `RoundAt(len(paths))` gets `ErrInvalidFile`, red today. Verified-against: 1a15d3d.
2. **R6Stats ingest: rounds the parser skipped (`skippedRounds`, r6-dissect #23) are dropped without an upload warning** — from PR #38 rev-38 F3; called for by issue #23 Scope and plan 01 Risks. After #23 lands the CLI reports success with `skippedRounds` in the match JSON; R6Stats `api/r6stats/worker/jobs.py` discards stderr and nothing reads `skippedRounds`. Acceptance: an ingest test with a match JSON carrying `skippedRounds` produces an upload warning naming the file. Verified-against: R6Stats 826dc36 / r6-dissect 1a15d3d.
3. **match: `skippedRounds[].error` carries the absolute host path of an unreadable file** — from PR #38 rev-38 F4. Observed: `"error":"open /tmp/…/R02.rec: permission denied"` in stdout JSON (the CLI reports success). R6Stats stores the raw match JSON. Acceptance: with an unreadable R02, the JSON error string contains no `/` path prefix (file name only). Verified-against: 1a15d3d.

4. **test: `TestAcceptance_Matches` matches expected failures by kind only, and the flaky entry can never turn red** — from PR #39 rev-39 F2+F3. A second finding of the same kind in a known folder stays green (`compareToKnown`); the 47572 `flaky: true` nondeterminism entry stays green after #26 lands, contrary to `scripts/corpus/README.md` ("When a fix lands, the test goes red for that entry"). Acceptance: adding a second null round to a known folder's expected output, or fixing the 47572 nondeterminism, turns the test red. Verified-against: 1fea036. Update at landing: #50 removed folder `nick-Match-2026-03-09_17-13-54-47572`'s expected failures, so the flaky half no longer applies; kind-only matching (`compareToKnown`) stands.
5. **scripts: `fetch.sh` follows symlinks already in the output dir and cannot recover blobs that exist only in a fork's history** — from PR #39 rev-39 F4+F5. Probe: a symlink `out/<folder>` → `victim/` received the file; `--depth=1` reported a history-only blob `missing`. `scripts/corpus/README.md` overstates force-push safety. Acceptance: fetch.sh refuses a symlinked target path and fetches full history for the pinned refs (or the README says tips only). Verified-against: 1fea036.

6. **scoreboard: the Y10S4+ score reader drops round-start snapshot rows (marker byte `0xa3`)** — from PR #43 rev-43 F3. In nicktstewart match3 R05 (21916), player38's 930 appears once, in the round-start snapshot at offset 20630 with byte −18 = `0xa3`; `scoreboardEntryHasMarker` requires `0x23`, so it is dropped and the player's cumulative score reads 930 → 0 in a round with no score change. Not owned by #17–#35 (#26 scopes snapshot detection out). The corpus entry `knownMatchFailures["nick-Match-2026-03-08_19-55-32-21916"]` `score-drop` is mislabelled `#19`; relabel to this candidate at reconciliation (done: `ef23785`). Acceptance: the corpus test's 21916 folder passes the score-drop invariant. Verified-against: 6b3617d.

7. **damage taken: an empty team role applies the defender rule to attackers (stale snapshot counted)** — PR #48 rev-48 F3. Roles are set only when `playersRead == 10`; `health.go` treats "" as not-attacker. Blanking roles on custom_solid_snake moves an attacker row 100 → 210. Acceptance: with empty roles, attacker rows keep the attacker rule (`Role != Defense` fallback, or roles derived another way). Verified-against: 8e883c8.
8. **damage taken: README states the value as measured with no caveat for unvalidated heal sources (Doc, Thunderbird, Skopos swaps, disconnects), and the match total silently omits rounds with no health records** — PR #48 rev-48 F4/F5. Acceptance: README caveat, and a match row that marks partial coverage. Verified-against: 8e883c8.

9. **CLI: a round with `warnings` leaves no stderr line at the default log level** — PR #49 author C1 / rev-49 F3. Observed on a crafted match folder: track `match rc=0 stderr_lines=1` (`round skipped`), head `match rc=0 stderr_lines=0` with `round1_warnings=1`. `main.go` `zerolog.SetGlobalLevel(zerolog.ErrorLevel)`; `dissect/warning.go` `log.Warn().Str("listener", name)`. Acceptance: at the default level the CLI prints one line per round carrying `warnings`, or the desk closes this (the JSON key is the channel). Verified-against: 3724f63.
10. **warnings: pre-Y8S1 `readY7Time` parse errors embed the parsed text, so a broken clock gives one warning per tick** — rev-49 F4. Probe: `ticks=50 warnings=50`, first `Error:strconv.ParseFloat: parsing "x00": invalid syntax`. The merge key in `dissect/warning.go` `if w.Listener == name && w.Pattern == pattern && w.Error == msg {` includes the text. Acceptance: 50 failing ticks give one merged warning with `count` 50. Verified-against: 3724f63.
11. **a lost player record lets events about that player fall back to another attribution** — rev-50 F5 / rev-49 F6. With a `readPlayer` warning: `dissect/scoreboard.go` `if targetIdx < 0 {` skips the scoreboard correction (14012 R02: the lost player's death credited to the feed killer, 2 players' kills off by one), and `dissect/defuse.go` `playerIndex = r.lastAliveDefuserPlayer(plant)` credits the lost planter's plant to another player (32000 R02). Also the `ReadWarning` doc and README "Round warnings" say only that data "may be missing" (`dissect/warning.go` and `README.md`, the phrase `may be missing`). Acceptance: on a round with a `readPlayer` warning, such events stay unattributed (or the docs name the fallback). Verified-against: 3724f63.
12. **readAtkOpSwap records a swap and then returns Skip's EOF** — rev-49 F5 (pre-existing). `dissect/player.go` `r.swapOperator(objectIndex, o)` then `return err`; under #49 the swap is kept and a warning is also recorded for a packet whose data was used. Acceptance: a swap packet ending at the buffer end yields the swap and no warning. Verified-against: 3724f63.
13. **health/defuse index state is not moved by `movePlayerIndices`** — PR #50 author candidate / rev-50 F1. `healthState`, `healthTransitions[].player`, `defuserRun.playerIndex` (`dissect/defuse.go` `playerIndex int`) are empty at `deriveTeamRoles` on 88 of 88 Y10S4+ rounds, including with #49's failure injected; a synthetic test can reach them. Acceptance: a unit test records a health transition and starts a defuser run before a non-last drop; DBNO, Revive and defuser events name the right player. Verified-against: 81debed.
14. **scoreboard mapping: zip-vs-link chosen per call, not per round** — rev-50 F2. `dissect/scoreboard.go` checks the links read so far; a mapping call after a player packet but before the first link token would zip and cache. Not observed (first link precedes the first player packet on 88 of 88 Y10S4+ rounds). Acceptance: a synthetic round with a scoreboard packet before the first link maps by link once links arrive. Verified-against: 81debed.
15. **reader: `Bytes(n)` reports `io.EOF` for a read that ends exactly on the last byte of `r.b`** — PR #45 candidate. `(&Reader{b: []byte{1, 2, 3, 4}}).Bytes(4)` returns `[], EOF`; `dissect/reader.go` `Skip` does `r.offset += n` then `if r.offset >= len(r.b) {`. Acceptance: that call returns the four bytes and `nil`, goldens unchanged or each change justified. Verified-against: 7d37745.
16. **player: a late joiner is always put on team 1** — PR #46 candidate (plan 08 Risks "08b"). `dissect/player.go` `playersRead > 5` in `readPlayer`. Acceptance: a late joiner on team 0 gets `teamIndex` 0. Verified-against: 00383f6.
17. **export: the XLSX export has no `damageTaken` column** — PR #48 candidate; the JSON has it (`dissect/match.go`). Acceptance: the match XLSX carries the column. Verified-against: 8e883c8.
18. **scoreboard: the zip fallback can map two scoreboard IDs to one player** — rev-52 F1. `dissect/scoreboard.go` zip loop (the one unguarded writer of `r.scoreboardIDToPlayer[...] =`, no `!mappedPlayers[idx]` check) when `readPlayerOrder` repeats a player; reconcile's `expectedKills` then depends on map order (constructed, `for i := 0; i < 400; i++`: `map[A>E :46 B>E :354]`). Not observed (`manyToOne=0` in 88 reconciled rounds; the zip path never ran). Acceptance: the constructed state gives the same outcome in every iteration of that loop. Verified-against: 9618bb6.
19. **R6Stats: ingest the new parser keys** — PR #48 / #51 / #49 follow-ups. `damageTaken` (`_stats_row`), `weaponID`/`weaponSlot` (`RoundEvent` columns + migration), round `warnings` (with candidate 2's `skippedRounds`) as upload warnings. Needs a vendor bump. Acceptance: an ingest test per key. Verified-against: R6Stats c350dee / r6-dissect 9618bb6.
20. **JSON: three hand-written round structs serve the four write paths, with no test that they stay equal** — rev-53 A4 (landing). `git grep -n 'json:"matchFeedback"' -- '*.go' ':!*_test.go'` lists `dissect/match.go` (the `Data()` struct both match paths share), `exports/exports.go` (FFI single), `main.go` (CLI single) and `dissect/reader.go` (the Reader's own tags, not a write path). The lane kept them byte-equal (rev-53: FFI and CLI byte-equal single 42/42, match 5/5); PR #10 adds `TimelineError` to two of them. Acceptance: one test that marshals a round through every path and compares the bytes. Verified-against: 230c43d.

## Observations

Unreproduced or not acted on; never filed.

1. **Map IDs unconfirmed** (PR #37): the four new map IDs are unconfirmed by any replay (plan Risks), and the unknown-map warning in `readHeader` is exercised only by a mutant.
2. **Silent partial rounds** (PR #38): a cut exactly at a zstd frame boundary is still undetected (plan Risks).
3. **`ReadPartial` range unpinned** (PR #45): with `end /= 3` removed under `readPartial`, `TestReader_ReadPartial` stays green; the line predates the lane.
4. **PR #10 carry** (PR #49): when `track/rec-timeline` lands, drop its `settleTimelineOnStop` and `return` from the listener loop (`Read` no longer has a named error result), keep `buildTimeline` in the end-of-round block, add `Warnings` after `TimelineError` in `dissect/match.go` `round` and `main.go` `writeRound`, and give `exports/exports.go`'s single-round output the same `TimelineError` and `Warnings` keys (rev-53 A4: PR #10 adds `TimelineError` to match.go and main.go only), replace the README sentence about `round read stopped`, and add the plan's `TestTimeline_ListenerFailureStillBuildsTimeline`.
5. **Pre-Y10S4 drops** (PR #50): Y8S1 `quick_1` and Y8S2 `attacker_recorder_site_test` remove a non-last header-only player in `deriveTeamRoles`; the pre-Y10S4 scoreboard path resolves dissect IDs, so no golden changed.
6. **`+4` mapping kept across calls** (PR #50): a `+4` mapping made before a linked sbID's owner is read would keep that player; not seen.
7. **Weapon gaps** (PR #51): `ReadPartial` does not run `applyLoadouts`, so a partial read's Kill has `weaponID` but no `weaponSlot`; asset `70077334926` (2 kills) is in no loadout, probably melee, unclassified.
8. **Fallback moves are a guess** (PR #52): a pass-1 reconcile move is a guess by construction; the determinism test pins the player-index order, not ground truth.
9. **Operator-0 header players** (PR #47): out of scope (plan Risks).
10. **govulncheck** fails locally on the go1.27.2 stdlib (GO-2026-6809, fixed in go1.27.3): toolchain, not repo code; reported by every slot.
11. **Memory**: `-race` on `./dissect/test` peaks near 15–16 GB; two parallel runs swapped the host (27/31 GB). Lane rule since: agents never run it; the orchestrator runs it `-parallel=1`, one at a time, under `memguard.sh`.
