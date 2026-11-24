package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/redraskal/r6-dissect/dissect"
)

// Invariant kinds reported by checkMatchJSON and matched against knownMatchFailures.
const (
	kindUnknownID = "unknown-id"     // an Operator(N), Map(N) or GameMode(N) name in the output
	kindNotArray  = "not-array"      // a round's matchFeedback or stats is not a JSON array
	kindScoreDrop = "score-drop"     // a player's cumulative score went down between rounds
	kindNondet    = "nondeterminism" // two reads of the same match differ
	kindNoScores  = "no-scores"      // every round's scores are 0, so the score check had nothing to compare
	kindWarnings  = "warnings"       // a round carries warnings: a listener failed on that replay
)

// knownFailure is a corpus folder that fails an invariant today because of a bug another issue owns.
type knownFailure struct {
	kind  string
	owner string
	// flaky marks a failure that depends on map iteration order, so one run may not show it.
	// It is allowed to be absent but is never allowed to be a different kind.
	flaky bool
}

// knownMatchFailures lists the corpus folders (see scripts/corpus) that fail today. The test is
// green while exactly these fail. If one starts passing, the test goes red and the entry must be
// removed; if any other folder or invariant fails, it goes red as well.
var knownMatchFailures = map[string][]knownFailure{
	// round 5 is decided, but the score reader drops the round-start snapshot row, so a player who
	// scored nothing in that round reads 0 (rev-43 F3)
	"nick-Match-2026-03-08_19-55-32-21916": {
		{kind: kindScoreDrop, owner: "fork-ports candidate 6 (the Y10S4+ score reader drops round-start snapshot rows)"},
	},
}

// TestAcceptance_Matches checks match-level invariants of the JSON output on real match folders
// that are not committed (third-party data, or too large). Set R6_DISSECT_CORPUS to a directory
// whose subdirectories each hold the .rec files of one match (see scripts/corpus/README.md).
// Without it the test is skipped, which is what CI does.
func TestAcceptance_Matches(t *testing.T) {
	root := os.Getenv("R6_DISSECT_CORPUS")
	if root == "" {
		t.Skip("R6_DISSECT_CORPUS not set")
	}
	matches := corpusMatchDirs(t, root)
	if len(matches) == 0 {
		t.Fatalf("no match folders with .rec files under %s", root)
	}
	seen := map[string]bool{}
	for _, dir := range matches {
		name := filepath.Base(dir)
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			first := readMatchJSON(t, dir)
			found := checkMatchJSON(first)
			if second := readMatchJSON(t, dir); !bytes.Equal(first, second) {
				found = append(found, finding{kindNondet, "output differs between two reads of the same match"})
			}
			compareToKnown(t, found, knownMatchFailures[name])
		})
	}
	// An expected-failure entry for a folder that is not in the corpus is stale only when the
	// corpus claims to be the Y11S1 one, so a user's own matches do not trip it.
	for name := range knownMatchFailures {
		if !seen[name] && len(seen) > 0 && anyPrefix(seen, "nick-", "arnaud-") {
			t.Errorf("expected-failure entry %q has no folder in the corpus", name)
		}
	}
}

func anyPrefix(set map[string]bool, prefixes ...string) bool {
	for name := range set {
		for _, p := range prefixes {
			if strings.HasPrefix(name, p) {
				return true
			}
		}
	}
	return false
}

type finding struct {
	kind string
	msg  string
}

// compareToKnown fails when a finding is not expected, or an expected (non-flaky) one is missing.
func compareToKnown(t *testing.T, found []finding, known []knownFailure) {
	t.Helper()
	got := map[string][]string{}
	for _, f := range found {
		got[f.kind] = append(got[f.kind], f.msg)
	}
	want := map[string]knownFailure{}
	for _, k := range known {
		want[k.kind] = k
	}
	var kinds []string
	for k := range got {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		msgs := got[kind]
		if k, ok := want[kind]; ok {
			t.Logf("expected failure (%s, %s): %d finding(s), first: %s", kind, k.owner, len(msgs), msgs[0])
			continue
		}
		t.Errorf("unexpected %s failure: %s (and %d more)", kind, msgs[0], len(msgs)-1)
	}
	for _, k := range known {
		if _, ok := got[k.kind]; !ok && !k.flaky {
			t.Errorf("%s no longer fails (owner %s): remove it from knownMatchFailures", k.kind, k.owner)
		}
	}
}

func corpusMatchDirs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		d := filepath.Join(root, e.Name())
		if recs, _ := filepath.Glob(filepath.Join(d, "*.rec")); len(recs) > 0 {
			dirs = append(dirs, d)
		}
	}
	sort.Strings(dirs)
	return dirs
}

func readMatchJSON(t *testing.T, dir string) []byte {
	t.Helper()
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := dissect.NewMatchReader(f)
	if err != nil {
		t.Fatalf("NewMatchReader: %v", err)
	}
	if err := m.Read(); !dissect.Ok(err) {
		t.Fatalf("Read: %v", err)
	}
	b, err := json.Marshal(m.Data())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// checkMatchJSON returns every invariant violation in one match's JSON output.
func checkMatchJSON(b []byte) []finding {
	var found []finding
	for _, unknown := range []string{`"Operator(`, `"Map(`, `"GameMode(`} {
		if bytes.Contains(b, []byte(unknown)) {
			found = append(found, finding{kindUnknownID, "output names an unknown ID: " + unknown})
		}
	}
	var out struct {
		Rounds []map[string]json.RawMessage `json:"rounds"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return append(found, finding{kindNotArray, "output is not valid JSON: " + err.Error()})
	}
	prev := map[string]int{}
	scored := 0
	for i, r := range out.Rounds {
		for _, key := range []string{"matchFeedback", "stats"} {
			if v := strings.TrimSpace(string(r[key])); !strings.HasPrefix(v, "[") {
				found = append(found, finding{kindNotArray, fmt.Sprintf("round %d: %q is %s, want a JSON array", i, key, v)})
			}
		}
		if v := strings.TrimSpace(string(r["warnings"])); v != "" && v != "null" && v != "[]" {
			found = append(found, finding{kindWarnings, fmt.Sprintf("round %d: warnings %s", i, v)})
		}
		var stats []struct {
			Username string `json:"username"`
			Score    int    `json:"score"`
		}
		_ = json.Unmarshal(r["stats"], &stats)
		allZero := true
		for _, s := range stats {
			if s.Score != 0 {
				allZero = false
			}
		}
		if allZero { // no scoreboard recorded (e.g. a round forfeited in prep)
			continue
		}
		scored++
		cur := map[string]int{}
		for _, s := range stats {
			cur[s.Username] = s.Score
			if p, ok := prev[s.Username]; ok && s.Score < p {
				found = append(found, finding{kindScoreDrop, fmt.Sprintf("round %d: a player's cumulative score went down %d -> %d", i, p, s.Score)})
			}
		}
		prev = cur
	}
	if scored == 0 {
		found = append(found, finding{kindNoScores, fmt.Sprintf("none of %d rounds has a non-zero score", len(out.Rounds))})
	}
	return found
}

// A round with warnings is a finding: since #31 a failing listener no longer fails the read, so
// the warnings key is the only sign of it.
func TestCheckMatchJSON_Warnings(t *testing.T) {
	b := []byte(`{"rounds":[{"matchFeedback":[],"stats":[{"username":"a","score":10}],` +
		`"warnings":[{"listener":"readTime","error":"EOF","count":1}]}]}`)
	var got []finding
	for _, f := range checkMatchJSON(b) {
		if f.kind == kindWarnings {
			got = append(got, f)
		}
	}
	if len(got) != 1 {
		t.Fatalf("warnings findings = %+v, want exactly one", got)
	}
	clean := []byte(`{"rounds":[{"matchFeedback":[],"stats":[{"username":"a","score":10}]}]}`)
	for _, f := range checkMatchJSON(clean) {
		if f.kind == kindWarnings {
			t.Fatalf("clean round gave a warnings finding: %+v", f)
		}
	}
}
