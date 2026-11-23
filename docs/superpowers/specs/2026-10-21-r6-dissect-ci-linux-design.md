# r6-dissect CI: drop macOS, consolidate on Linux, tighten the pipeline

Date: 2026-10-21
Repo: prestonwillis10/r6-dissect (fork, canonical, `main`)
Status: approved 2026-10-21 (owner answered both open questions: Linux only; macOS removed from releases, README amended)

## Intent

The Build & Test workflow runs six jobs per push/PR: Go 1.26 and 1.27 on
ubuntu, macOS and Windows. The two macOS jobs run on GitHub's 3-core Apple
Silicon runner. They have never caught anything the ubuntu job did not, and
nothing in the repo is macOS-specific. The owner wants macOS out and the
pipeline consolidated on Linux, and wants to use the rewrite to make CI more
effective overall.

Decision already made (2026-10-21): CI stays on GitHub-hosted runners. Moving
to the SpartanInventory self-hosted fleet is out of scope and tracked
separately.

## Goals

1. No macOS job on push/PR CI.
2. Linux is the primary CI platform: full Go version matrix, cgo shared
   library build, tests, and a new lint/vet gate all run on ubuntu.
3. Fix the real inefficiencies found in the evaluation: broken module cache,
   deprecated steps, redundant builds, no timeouts, no cancel-in-progress.
4. Add the quality gates a Go library should have: vet, gofmt, staticcheck,
   govulncheck.
5. Keep the release workflow working for the first fork release.

## Non-goals

- Self-hosted runners (deferred, see memory note `r6-dissect-ci-self-hosted-deferred`).
- Changing what the test suite tests. Fixtures stay plain git blobs (113 MB,
  fine for a depth-1 checkout).
- Upstreaming anything to redraskal/r6-dissect (fork is canonical).
- Branch protection or required checks (none exist today; not adding any in
  this effort).

## Anti-goals

- Lint noise that blocks every PR: the lint job must be green on `main` the
  day it lands, so the existing staticcheck findings get fixed first.
- Silently breaking the README's shared-library promise.

## Findings the design rests on

Verified 2026-10-21 by a read-only evaluation (commands run locally with Go
1.27.2 and against the GitHub API).

- Nothing on `main` is OS-specific. The only build tags are `ignore` and
  `tools`. Golden tests compare decoded structs, not bytes.
- The cgo `c-shared` build of `exports/exports.go` succeeds on ubuntu-latest
  (gcc) and Windows (mingw). The Linux job fully covers "does the export
  still compile".
- Test suite: 14 tests + 2 network-gated skips, ~7 s wall locally, ~42 s on
  ubuntu-latest. No network needed by default.
- Module cache is broken in every job: `actions/setup-go` runs before
  `actions/checkout`, so `cache: true` cannot find `go.sum` and logs
  `Restore cache failed`. All 35 modules re-download every run.
- `go get -v -t -d ./...` is deprecated (`-d` is a no-op) and redundant.
- `go build -v ./...` compiles the whole tree, then `go test` compiles it
  again, then the c-shared step compiles `exports` a third time.
- No `timeout-minutes`, no `concurrency`, no `permissions` block on
  build-test. Actions are tag-pinned, not SHA-pinned. No Dependabot config.
- No vet / gofmt / staticcheck / govulncheck anywhere. Locally: gofmt, vet,
  `go mod tidy -diff`, `go mod verify` are all clean. staticcheck reports 11
  findings. govulncheck reports 3 reachable vulns via excelize v2.9.0 (fixed
  in v2.11.x) and 1 stdlib x509 vuln that depends on the patch release.
- Windows has caught one real bug (run 39467628223). macOS has caught none in
  30 runs and always matched ubuntu.
- Durations per job (Go 1.26): ubuntu ~1m40s, macOS ~1m30s, Windows ~2m.
- Release workflow (`release.yaml`) has never run in the fork. It builds the
  cgo shared library natively on five runners including two macOS legs.
  Nobody in this stack consumes the shared library: R6Stats-deploy builds the
  CLI with `CGO_ENABLED=0` and calls it via subprocess. The only promise is
  README "Shared Library (FFI)", which says each release includes
  libr6dissect for Linux, Windows and macOS.
- No branch protection, no rulesets, no required status checks. Renaming or
  removing jobs cannot block merges.
- Run 39545378205 (`action_required`) is a first-time outside contributor's
  fork PR (#12) awaiting "Approve and run". Not a pipeline fault.
- Upstream's build-test is older (Go 1.23, pull_request only, no c-shared
  step). The fork has already diverged; no reason to track upstream's CI.

## Design

### Build & Test workflow (`.github/workflows/build-test.yaml`)

Triggers unchanged: push to `main`, every `pull_request`.

Top-level additions:

```yaml
permissions:
  contents: read
concurrency:
  group: build-test-${{ github.ref }}
  cancel-in-progress: ${{ github.event_name == 'pull_request' }}
```

Cancel is PR-only so every commit on `main` keeps a completed run (my call,
after the builder check).

Three jobs, all on `ubuntu-latest`:

**`test` (Linux, Go matrix).** `runs-on: ubuntu-latest`, matrix over Go
`["1.26", "1.27"]`, `fail-fast: false`, `timeout-minutes: 15`. Steps:

1. `actions/checkout` (depth 1).
2. `actions/setup-go` with `go-version: ${{ matrix.go }}` and `cache: true`
   (now works because `go.sum` is present).
3. `go mod download` (replaces `go get -d`).
4. `go build ./...` for the non-test packages (keeps root `main` and
   `exports` compiling; drop `-v` to cut log noise).
5. cgo c-shared build of `exports/exports.go` to `$RUNNER_TEMP`, unchanged.
6. `go test -race -count=1 ./...`. `-race` is cheap at this suite size and
   catches concurrency bugs in the reader; it is the Linux-only addition that
   makes the Linux leg stronger than the legs it replaces.

Note on build redundancy: `go test` compiles everything with tests;
`go build` is kept only for packages without tests (root, `exports`, `ubi`).
Both stay because they are cheap (~25 s) and the cache now makes the second
compile mostly a no-op. This is the builder's call to tune.

No Windows job (owner decision 2026-10-21). Note for the record: Windows
caught one portability bug in the fork's history (dir-mode assertion in
`TestWriteRound_Sidecar`, fixed in d9da436, now guarded by `runtime.GOOS`).
Portability regressions of that kind will no longer be caught in CI.

**`lint` (Linux).** `runs-on: ubuntu-latest`, Go 1.27, `timeout-minutes: 10`:

1. checkout, setup-go with cache.
2. `gofmt -l .` must print nothing.
3. `go vet ./...`.
4. `go mod tidy -diff` must be empty (CI already clean; prevents drift).
5. `staticcheck ./...` via `dominikh/go-tools` installed with
   `go install honnef.co/go/tools/cmd/staticcheck@<pinned version>`.
**`vulncheck` (Linux), separate job.** `govulncheck ./...` via
`go install golang.org/x/vuln/cmd/govulncheck@<pinned>`, `timeout-minutes: 10`.
Blocking from the start, because the excelize bump lands in the preceding
PR and clears the known findings. It is its own job rather than a lint step
so a red caused by a newly published CVE (no code change) is distinguishable
from a lint red; the fix path for that case is Dependabot's weekly gomod PR
or a manual bump. My call after the builder check: the anti-goal is about
pre-existing noise, not about a real new CVE turning CI red.

Job naming: the old names `build (1.26, macos-latest)` etc. disappear. No
required checks reference them, so nothing breaks.

### Code fixes required for lint to be green

Done in the same PR or a preceding one, before the lint job is made blocking:

- Fix the 11 staticcheck findings: unused `err` in `dissect/time.go:176`;
  three ineffective `break` statements in `dissect/time.go:236,246,249`;
  unused type `hexEventComparison` in `dissect/utils.go:47`; loop-to-append
  in `dissect/reader.go:155`; plus whatever the remaining findings are once
  staticcheck is run at the pinned version.
- Bump `github.com/xuri/excelize/v2` v2.9.0 → latest v2.11.x (amended 2026-10-22: pinned to the master pseudo-version `v2.11.1-0.20261020033641-611b222fd8f7` because v2.11.0 still carries two reachable findings; `SetCellInt` now takes `int64`, adapted in the `Int` wrapper). No test covers
  the xlsx export, so verification is a one-off: export one fixture match
  with `--export` before and after the bump and compare sheet names, row
  counts and cell values (not bytes; xlsx embeds timestamps). Record the
  comparison in the PR body. Writing a golden xlsx test is out of scope (my
  call). This clears the three reachable vulns.
- The unused `err` in `readY7Time` (`dissect/time.go:176`) is fixed by
  propagating it. `Reader.String()` returns an empty string on error, so
  today the function already fails at the following parse step; returning
  the read error keeps behavior identical on the success path. On a
  truncated payload the error is now `io.EOF`, which the top-level readers
  treat as end-of-stream, the same as every sibling `r.String()` listener,
  so a truncated time field ends the read like any other truncated field
  instead of raising a parse error (rev-13 finding F1 on PR #13, accepted;
  my call).

### Release workflow (`.github/workflows/release.yaml`)

The CLI cross-compile job (go-release-action on ubuntu) is untouched.

The `shared-library` job drops both `macos-latest` legs (owner decision
2026-10-21). Remaining legs: linux/amd64 (ubuntu-latest), linux/arm64
(ubuntu-24.04-arm), windows/amd64 (windows-latest). The Windows leg stays
because it is release-only, was not part of the decision, and the README
promises a Windows DLL (my assumption; trivial to drop later).

The CLI cross-compile matrix keeps `darwin` targets: the CLI has no cgo and
cross-compiles from ubuntu, so macOS users still get a binary.

README "Shared Library (FFI)" is amended to say each release includes
libr6dissect for Linux and Windows. The `ctypes` example is unchanged.

Minor hardening in the same PR: `timeout-minutes` on both jobs, `concurrency`
on the workflow, pinned `go-version-file: go.mod` already present.

### Operators workflow (`.github/workflows/operators.yaml`)

Already Linux-only. Add `permissions: contents: read` and
`timeout-minutes: 10`. Dispatch it once after merge to confirm it runs; it has
never executed in the fork.

### Dependabot (`.github/dependabot.yml`)

Weekly updates for `github-actions` and `gomod`, grouped so Go minor bumps
arrive as one PR. This is what keeps the pinned staticcheck/govulncheck
versions and action SHAs from rotting.

### Action pinning

Pin all `uses:` to full commit SHAs with a `# vN` comment, matching the
convention in SpartanInventory. Dependabot keeps them current.

## What the builder decides

- Exact SHA pins and the staticcheck/govulncheck versions (newest at build
  time).
- Whether `go build ./...` stays as a separate step or is folded into the
  c-shared step (either is fine; keep the c-shared build).
- Whether the staticcheck fixes ship in the same PR as the workflow change or
  a preceding `chore:` PR. Preference: preceding PR, so the lint job's first
  run on `main` is green.
- Fixture handling in checkout (depth 1 is enough; no LFS).

## Verification

- Open the PR; confirm exactly four jobs run: `test (1.26)`, `test (1.27)`,
  `lint`, `vulncheck`. No macOS or Windows job appears.
- `setup-go` log shows a cache hit on the second run (no
  `Restore cache failed`).
- No `-d flag is deprecated` warning in logs.
- `lint` and `vulncheck` are green on the merge commit.
- A second push to a PR cancels the first run; a second push to `main` does
  not.
- After merge, `workflow_dispatch` on operators.yaml completes.
- Total wall time per PR is at or below the current ~2m.
- `grep -rn macos .github/` returns nothing.

## PR shape (builder's call, recommended)

1. `chore:` PR: staticcheck fixes + excelize bump. Runs under today's CI.
2. `ci:` PR: the workflow rewrite, release.yaml change, README amendment,
   Dependabot config, SHA pins. Its first run on `main` is green because
   PR 1 landed first.

## Owner decisions (2026-10-21)

1. Build & Test is Linux only. No Windows job.
2. macOS is removed from the release workflow too; README amended.
