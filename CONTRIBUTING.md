# Contributing

This repository is [prestonwillis10](https://github.com/prestonwillis10)'s fork of [redraskal/r6-dissect](https://github.com/redraskal/r6-dissect). It is maintained independently and powers replay stats for [CollegiateR6](https://collegiater6.com). Contributions are welcome. This page covers how they get reviewed and merged.

## Fork or upstream?

This fork does not forward changes upstream, and upstream changes are not merged here automatically. If your fix applies to the original project, open it at [redraskal/r6-dissect](https://github.com/redraskal/r6-dissect). Open it here if it targets this fork's `main`.

## Before you start

- **Bug fixes** with a clear reproduction can go straight to a pull request.
- **Anything else** needs an issue first so the approach can be agreed before you write code. That covers new features, new or renamed output fields, changes to the JSON shape, and new decoding of replay data that isn't understood yet. The JSON output is consumed downstream, so changing a field's name, type or meaning breaks things.

## Pull request requirements

- One focused change per PR, opened against `main`. Leave out unrelated reformatting, refactors and dependency bumps.
- In the description, cover the problem, the fix and how you tested it, and link the issue if there is one. The [pull request template](.github/pull_request_template.md) lays this out.
- A bug fix comes with a test that fails without the fix, wherever that is practical. If the parsed output changes on purpose, regenerate the golden files (see [Tests](README.md#tests)) and say why in the PR.
- CI must pass. To run the same checks locally (the shared-library build and `-race` need cgo and a C compiler):

  ```bash
  go build ./...
  go build -buildmode=c-shared -o /tmp/libr6dissect exports/exports.go
  go test -race -count=1 ./...
  ! gofmt -l . | grep .
  go vet ./...
  go mod tidy -diff
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
  go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
  ```

- **Replay files:** only commit replays you recorded or have permission to share, and trim fixtures to as little as reproduces the behaviour. If verifying your change needs a replay you can't publish, say so in the PR and the maintainer will arrange another way to get it.
- **AI-assisted contributions** are fine. Say so in the PR. You are responsible for understanding and verifying everything you submit.

## Review and merging

- Every pull request from outside contributors is reviewed by the maintainer and is **merged only with the maintainer's explicit approval**. Automated checks and automated or AI review may comment on a PR, but they never merge an outside contribution on their own.
- If new commits are pushed after approval, they need another look before merge.
- The maintainer may ask for changes or for the PR to be split. A PR that doesn't fit the fork's direction may be closed. Opening an issue first is the best way to avoid that.
- PRs are merged with a merge commit. If "Allow edits and access to secrets by maintainers" is on, the maintainer may push small fixups to your branch before merging.

## License

By contributing, you agree that your contributions are licensed under this repository's [MIT License](LICENSE).
