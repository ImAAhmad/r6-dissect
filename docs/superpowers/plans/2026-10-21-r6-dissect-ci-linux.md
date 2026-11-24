# r6-dissect CI Linux Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove macOS (and Windows) from r6-dissect CI, run everything on ubuntu-latest, fix the broken module cache and deprecated steps, and add lint and vulnerability gates that are green on day one.

**Architecture:** Two PRs against `prestonwillis10/r6-dissect` `main`. PR 1 (`chore:`) makes the code pass staticcheck and bumps excelize so the new gates have nothing pre-existing to flag. PR 2 (`ci:`) rewrites `build-test.yaml` into three Linux jobs, strips macOS from `release.yaml`, hardens `operators.yaml`, adds Dependabot, and amends the README.

**Tech Stack:** Go 1.26/1.27, GitHub Actions (hosted ubuntu-latest), staticcheck v0.8.2, govulncheck v1.9.0, excelize master pseudo-version v2.11.1-0.20261020033641-611b222fd8f7 (see Task 2 note).

**Spec:** `docs/superpowers/specs/2026-10-21-r6-dissect-ci-linux-design.md` (in the R6Stats repo; the code lives in the `r6-dissect` fork checkout at `/home/prest/projects/R6Stats/r6-dissect`).

## Global Constraints

- Repo: `/home/prest/projects/R6Stats/r6-dissect`, remote `origin` = `prestonwillis10/r6-dissect`. Never touch `upstream` (redraskal). Branch from `origin/main`.
- Go toolchain is at `~/.local/go/bin` (not on PATH by default): `export PATH=$HOME/.local/go/bin:$HOME/go/bin:$PATH`.
- Every `runs-on` in `.github/workflows/` must be `ubuntu-latest` or `ubuntu-24.04-arm`, except the single `windows-latest` release leg for the DLL. `grep -rn macos .github/` must return nothing after PR 2.
- Every workflow job gets `timeout-minutes`; every workflow gets `permissions:` (build-test and operators: `contents: read`; release keeps `contents: write`).
- Every `uses:` is pinned to a full commit SHA with a `# vX.Y.Z` comment. Pins to use:
  - `actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1`
  - `actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0`
  - `actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2`
  - `wangyoucao577/go-release-action@279495102627de7960cbc33434ab01a12bae144b # v1.55`
- Tool pins: `honnef.co/go/tools/cmd/staticcheck@v0.8.2`, `golang.org/x/vuln/cmd/govulncheck@v1.9.0`.
- Golden fixtures (`dissect/test/data/replays/valid/**/*.rec.json`) must not change in either PR. Never run `-update`.
- Commit messages: conventional prefixes as in the repo log (`chore:`, `ci:`, `docs:`, `fix:`). A code-path change is never labelled `docs:`.
- PRs are opened with the `/ship` skill once a task group is complete; do not merge without CI green.

## Review Focus

1. The `readY7Time` error fix (Task 1) must not alter the parse of any fixture replay. The golden test `TestReader_Read` over all 11 valid `.rec` files is the pin; it is run in Task 1 step 5.
2. The excelize bump (Task 2) must not change exported cell values. No test covers the xlsx writer, so Task 2 pins it with a before/after cell-value comparison recorded in the PR body.
3. The module cache (Task 3) must actually hit on the second run. Task 6 checks the `setup-go` log for a cache hit and the absence of `Restore cache failed`.
4. A second push to `main` must not cancel the first run; a second push to a PR must. Task 6 verifies both.
5. `govulncheck` must be green on `main` at landing. Task 2 lands first and Task 6 confirms the `vulncheck` job passes on the PR 2 merge commit.

---

## PR 1: `chore: pass staticcheck and bump excelize`

Branch: `chore/staticcheck-excelize` from `origin/main`.

### Task 1: Fix the staticcheck findings

**Files:**
- Modify: `dissect/reader.go:155-157`
- Modify: `dissect/time.go:176`, `dissect/time.go:236`, `dissect/time.go:246`, `dissect/time.go:249`
- Modify: `dissect/utils.go:46-?` (remove the `hexEventComparison` type and its two methods, through the end of `Flush`)

**Interfaces:**
- Consumes: nothing.
- Produces: a tree on which `staticcheck ./...` exits 0. Task 3's `lint` job relies on that.

- [ ] **Step 1: Record the baseline**

Run: `go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...`
Expected: exactly these 11 lines (S1011 reader.go:155; SA4006 time.go:176; SA4011+S1023 at time.go:236, :246, :249; U1000 at utils.go:47, :52, :57), exit status 1.

- [ ] **Step 2: `dissect/reader.go:155` — replace the byte loop with a single append**

`for _, b := range decompressed { data = append(data, b) }` becomes `data = append(data, decompressed...)`.

- [ ] **Step 3: `dissect/time.go:176` — propagate the read error in `readY7Time`**

```go
time, err := r.String()
if err != nil {
    return err
}
```
Keep everything else in the function unchanged. (`Reader.String` returns `""` on error, so the function already failed at `strconv.ParseFloat("")`; this only improves the error.)

- [ ] **Step 4: `dissect/time.go:236,246,249` — delete the three trailing `break` statements**

Each is the last statement of a `case` in the `switch u.Type` block and does nothing in Go. Delete the line only; do not change case bodies.

- [ ] **Step 5: `dissect/utils.go` — delete `hexEventComparison`**

Remove the comment `// hexEventComparison - Debugging tool`, the struct, `Push`, and `Flush` (through its closing brace). Run `gofmt -l ./dissect` afterwards; it must print nothing. If an import becomes unused, remove it.

- [ ] **Step 6: Verify lint, vet, format, tidy, tests, goldens**

Run:
```bash
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./... && go vet ./... && test -z "$(gofmt -l .)" && go mod tidy -diff && go test -race -count=1 ./... && git status --short dissect/test/data
```
Expected: staticcheck prints nothing (exit 0); vet silent; gofmt empty; tidy -diff empty; all packages `ok`; `git status` on the fixture dir is empty (goldens untouched).

- [ ] **Step 7: Commit**

```bash
git add dissect/reader.go dissect/time.go dissect/utils.go
git commit -m "chore: fix staticcheck findings (unused err, dead breaks, unused debug type)"
```

### Task 2: Bump excelize to v2.11.0

> **Amended 2026-10-22 during execution (W1, PR #13):** the newest tag v2.11.0 still has GO-2026-6688 and GO-2026-6684 reachable under govulncheck v1.9.0 (the vuln DB moved after the evaluation). The orchestrator decided to pin the master pseudo-version `v2.11.1-0.20261020033641-611b222fd8f7` instead; Dependabot (Task 5) advances it to the next tag. v2.11.x also changed `SetCellInt` to take `int64`, so the `Int` wrapper in `dissect/utils.go` gained an `int64(n)` conversion inside the bump commit. Read `v2.11.0` below as that pseudo-version.

**Files:**
- Modify: `go.mod` (`github.com/xuri/excelize/v2 v2.9.0` → `v2.11.0`), `go.sum`

**Interfaces:**
- Consumes: Task 1's clean tree.
- Produces: a `go.mod` on which `govulncheck ./...` reports no findings reachable through `WriteExcel`. Task 3's `vulncheck` job relies on that.

- [ ] **Step 1: Capture the "before" workbook**

Build and export from the pre-bump tree:
```bash
S=/tmp/claude-1001/-home-prest-projects-R6Stats/3e6d225d-041e-48e8-b0fa-84fbc5c7e49c/scratchpad/excelize
mkdir -p "$S" && go build -o "$S/r6-dissect-before" . && \
"$S/r6-dissect-before" dissect/test/data/replays/valid/Y11S2 -o "$S/before.xlsx"
```
Expected: `before.xlsx` exists, non-empty, no `fatal` log line.

- [ ] **Step 2: Bump and tidy**

Run: `go get github.com/xuri/excelize/v2@v2.11.0 && go mod tidy && go mod verify`
Expected: `go.mod` shows `v2.11.0`; `go mod verify` prints `all modules verified`.

- [ ] **Step 3: Capture the "after" workbook and compare cell values**

```bash
go build -o "$S/r6-dissect-after" . && "$S/r6-dissect-after" dissect/test/data/replays/valid/Y11S2 -o "$S/after.xlsx"
```
Write `$S/xlsx_cells.py` (scratchpad, not the repo): open the `.xlsx` with `zipfile`, parse `xl/sharedStrings.xml` into a list, then for every `xl/worksheets/sheet*.xml` emit one line per `<c>` element as `sheetfile<TAB>r-attribute<TAB>resolved value` (resolve `t="s"` through sharedStrings, else the `<v>` text), sorted. Run `python3 -I "$S/xlsx_cells.py" "$S/before.xlsx" > "$S/before.txt"` and the same for after, then `diff "$S/before.txt" "$S/after.txt"`.
Expected: `diff` prints nothing and both files have the same non-zero line count. If sheet order or `r` attributes differ but values match, report that in the PR body instead of failing. Any value difference is a stop: do not land the bump; report it.

- [ ] **Step 4: Verify govulncheck is clean**

Run: `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`
Expected: `No vulnerabilities found.` One possible residual: a stdlib finding tied to the local `go1.27.2` patch level. That is not an excelize finding and does not block; note it in the PR body.

- [ ] **Step 5: Run the suite**

Run: `go test -race -count=1 ./... && git status --short dissect/test/data`
Expected: all `ok`; fixture dir unchanged.

- [ ] **Step 6: Commit and ship PR 1**

```bash
git add go.mod go.sum
git commit -m "chore: bump excelize to v2.11.0 (clears 3 govulncheck findings)"
```
Then `/ship`. PR body must include: the staticcheck before/after line counts (11 → 0), the xlsx comparison result (line count and `diff` empty), and the govulncheck output. Wait for the existing six-job CI to go green and merge before starting PR 2.

---

## PR 2: `ci: Linux-only pipeline, lint and vulncheck gates, release without macOS`

Branch: `ci/linux-only` from `origin/main` after PR 1 is merged.

### Task 3: Rewrite `build-test.yaml`

**Files:**
- Modify (full rewrite): `.github/workflows/build-test.yaml`

**Interfaces:**
- Consumes: Task 1 (staticcheck clean) and Task 2 (govulncheck clean).
- Produces: jobs named `test (1.26)`, `test (1.27)`, `lint`, `vulncheck`. Task 6 looks for exactly these names.

- [ ] **Step 1: Write the workflow**

Top level:
```yaml
name: Build & Test
on:
  push:
    branches: [main]
  pull_request:
permissions:
  contents: read
concurrency:
  group: build-test-${{ github.ref }}
  cancel-in-progress: ${{ github.event_name == 'pull_request' }}
```

Job `test`: `runs-on: ubuntu-latest`, `timeout-minutes: 15`, `strategy: { fail-fast: false, matrix: { go: ["1.26", "1.27"] } }`. Steps in this order: checkout (pinned); setup-go (pinned) with `go-version: ${{ matrix.go }}` and `cache: true`; `go mod download`; `go build ./...`; the c-shared step copied verbatim from the current file (`CGO_ENABLED: "1"`, output `$RUNNER_TEMP/libr6dissect`, `exports/exports.go`); `go test -race -count=1 ./...`.

Job `lint`: `runs-on: ubuntu-latest`, `timeout-minutes: 10`. Steps: checkout; setup-go with `go-version: "1.27"` and `cache: true`; then one `run` step per check so a failure names itself:
- `test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }`
- `go vet ./...`
- `go mod tidy -diff`
- `go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...`

Job `vulncheck`: `runs-on: ubuntu-latest`, `timeout-minutes: 10`. Steps: checkout; setup-go `"1.27"` with cache; `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`.

Remove the `go get -v -t -d ./...` step and the `-v` on `go build` entirely.

- [ ] **Step 2: Validate the YAML locally**

Run: `python3 -c "import yaml,sys; d=yaml.safe_load(open('.github/workflows/build-test.yaml')); print(sorted(d['jobs']))"` (if PyYAML is absent, `go run github.com/goccy/go-yaml/cmd/ycat@latest` is an alternative; any parser that proves it is valid YAML is fine).
Expected: `['lint', 'test', 'vulncheck']`.

- [ ] **Step 3: Confirm no macOS or Windows reference remains in this file**

Run: `grep -n -i "macos\|windows" .github/workflows/build-test.yaml`
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/build-test.yaml
git commit -m "ci: run Build & Test on Linux only; fix module cache; add lint and vulncheck jobs"
```

### Task 4: Remove macOS from `release.yaml` and amend the README

**Files:**
- Modify: `.github/workflows/release.yaml:13` (pin), `:29-44` (matrix include), `:45-47` (setup steps), `:64` (upload-artifact pin); add `timeout-minutes` to both jobs and `concurrency`.
- Modify: `README.md:134`

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: a `shared-library` matrix of exactly three entries (linux/amd64, linux/arm64, windows/amd64).

- [ ] **Step 1: Edit the `shared-library` matrix**

Delete the two `macos-latest` include lines (darwin arm64 and darwin amd64 with `cc: "clang -arch x86_64"`). Keep the three others unchanged. With no entry setting `cc`, the `CC: ${{ matrix.cc }}` env line and the `if [ -z "$CC" ]; then unset CC; fi` guard are dead; remove both.

- [ ] **Step 2: Harden the workflow**

Add `concurrency: { group: release-${{ github.ref }}, cancel-in-progress: false }` at top level; `timeout-minutes: 20` on `releases-matrix` and on `shared-library`. Replace the four `uses:` with the pinned SHAs from Global Constraints (go-release-action v1.55 replaces v1.52; keep all its `with:` inputs). The `releases-matrix` `goos: [linux, windows, darwin]` list stays: the CLI has no cgo and cross-compiles.

- [ ] **Step 3: Amend README.md:134**

Change `Each release includes \`libr6dissect\` for Linux, Windows and macOS, for use from other languages via FFI.` to `Each release includes \`libr6dissect\` for Linux and Windows, for use from other languages via FFI.` Leave the C signatures and the Python example as they are.

- [ ] **Step 4: Verify**

Run: `grep -rn -i macos .github/ README.md; python3 -c "import yaml; d=yaml.safe_load(open('.github/workflows/release.yaml')); print(len(d['jobs']['shared-library']['strategy']['matrix']['include']))"`
Expected: grep prints nothing; the count is `3`.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/release.yaml README.md
git commit -m "ci: drop macOS shared-library builds from releases; pin actions"
```

### Task 5: Harden `operators.yaml` and add Dependabot

**Files:**
- Modify: `.github/workflows/operators.yaml`
- Create: `.github/dependabot.yml`

**Interfaces:**
- Consumes: nothing.
- Produces: Dependabot config that keeps the SHA pins and Go modules current.

- [ ] **Step 1: operators.yaml**

Add top-level `permissions: { contents: read }`, `timeout-minutes: 10` on the job, and swap the two `uses:` to the pinned SHAs. Trigger and test command unchanged.

- [ ] **Step 2: dependabot.yml**

```yaml
version: 2
updates:
  - package-ecosystem: github-actions
    directory: /
    schedule: { interval: weekly, day: monday, time: "06:00", timezone: America/New_York }
    groups:
      actions: { patterns: ["*"] }
  - package-ecosystem: gomod
    directory: /
    schedule: { interval: weekly, day: monday, time: "06:00", timezone: America/New_York }
    groups:
      go-minor-patch:
        patterns: ["*"]
        update-types: ["minor", "patch"]
```
Majors for gomod arrive as individual PRs (not ignored: this repo has few deps and no major migration debt). Action majors are grouped with everything else.

- [ ] **Step 3: Verify every `uses:` in the repo is SHA-pinned**

Run: `grep -rn "uses:" .github/workflows/ | grep -v -E "@[0-9a-f]{40} # v"`
Expected: no output.

- [ ] **Step 4: Commit and ship PR 2**

```bash
git add .github/workflows/operators.yaml .github/dependabot.yml
git commit -m "ci: harden operators workflow; add Dependabot for actions and gomod"
```
Then `/ship`. PR body lists the job rename (`build (go, os)` × 6 → `test (go)` × 2 + `lint` + `vulncheck`) and states that no branch protection references the old names.

### Task 6: Verify the live pipeline

**Files:** none (observation only). Done on the open PR 2 and after its merge.

- [ ] **Step 1: Job set**

Run: `gh run list --repo prestonwillis10/r6-dissect --branch ci/linux-only --workflow build-test.yaml --limit 1 --json databaseId --jq '.[0].databaseId'` then `gh run view <id> --repo prestonwillis10/r6-dissect --json jobs --jq '.jobs[].name'`.
Expected: exactly `test (1.26)`, `test (1.27)`, `lint`, `vulncheck`. All `success`.

- [ ] **Step 2: Cache and deprecation**

Push an empty commit to the PR branch (`git commit --allow-empty -m "ci: cache probe"`), then on the new run: `gh run view <id> --repo prestonwillis10/r6-dissect --log --job <test-1.27-job-id> | grep -i -E "cache|deprecated"`.
Expected: a `Cache restored` (or `cache hit`) line from setup-go; no `Restore cache failed`; no `-d flag is deprecated`. Drop the probe commit before merge (`git reset --hard HEAD~1 && git push --force-with-lease`).

- [ ] **Step 3: Cancel semantics**

The push in Step 2 must show the previous PR run as `cancelled` (`gh run list --branch ci/linux-only --limit 3`). After merge, push two commits to `main` in quick succession only if there is something real to push; otherwise verify by reading the `concurrency` expression and note "not exercised" in the closing report.

- [ ] **Step 4: Wall time**

`gh run view <id> --json jobs --jq '.jobs[] | "\(.name) \(.startedAt) \(.completedAt)"'`.
Expected: the longest job at or under 2m00s (current ubuntu leg is ~1m40s uncached).

- [ ] **Step 5: Post-merge**

After PR 2 merges: `gh workflow run operators.yaml --repo prestonwillis10/r6-dissect` and confirm the run completes (`success`, or a test failure that names a real operator-list drift, which is a separate issue to file, not a pipeline failure). Confirm the Dependabot tab shows both ecosystems enabled.

- [ ] **Step 6: Record**

Append to `docs/superpowers/specs/2026-10-21-r6-dissect-ci-linux-design.md` (R6Stats repo) a short `## Landed` section: PR numbers, merge SHAs, measured job durations, and which Review Focus checks were exercised vs. reasoned.
