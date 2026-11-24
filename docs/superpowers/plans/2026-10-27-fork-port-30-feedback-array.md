# Fork port 30: an empty round writes matchFeedback as [] (issue #30)

Goal: every round object carries `matchFeedback` as a JSON array, `[]` when the round has no events.

Cause: `Reader.MatchFeedback` is a nil slice until the first `append`; `encoding/json` writes a nil slice as `null`. The single-round output (`main.go`, `exports/exports.go`), `Reader` itself and `MatchReader.Data` all marshal that field directly.

Census of serialised slices in the output structs (Header.Players, Header.GMSettings, PlayerStats, MatchReader rounds and stats) is in the slot log. The others are already built with `make`.

## Tasks
1. Baseline: count passing tests (unit and replay packages) and run the corpus acceptance test.
2. Red: `dissect/feedback_array_test.go` marshals a `NewReader` reader that read nothing (single round and `MatchReader.Data`), a one-event round, and a match whose only reader is nil.
3. Green: `MatchFeedback: make([]MatchUpdate, 0)` in the `NewReader` literal.
4. Drop the two `#30` entries from `knownMatchFailures`.
5. Mutations: drop the init; init only in one output path; set nil after `Read`.
6. Gates, slot log, ship.
