package dissect

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// readRoundRetagged reads a fixture round. When retag is set, the decompressed
// round is rewritten the way build 9734089 tagged its player records before
// the listeners run, so the result shows what readPlayer does on that build.
func readRoundRetagged(t *testing.T, path string, retag bool) (*Reader, error) {
	return readRoundRetaggedParts(t, path, retag, retag)
}

// readRoundRetaggedParts rewrites the id tag, the uiID tag, or both.
func readRoundRetaggedParts(t *testing.T, path string, retagID, retagUI bool) (*Reader, error) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(r.b, testIDAlpha03) || bytes.Contains(r.b, testUIAlpha03) {
		t.Fatalf("%s already contains a 9734089 tag", path)
	}
	if retagID {
		r.b = bytes.ReplaceAll(r.b, testIDClassic, testIDAlpha03)
	}
	if retagUI {
		r.b = bytes.ReplaceAll(r.b, testUIClassic, testUIAlpha03)
	}
	if retagID || retagUI {
		r.Header.CodeVersion = 9734089
	}
	return r, r.Read()
}

func roundJSON(t *testing.T, r *Reader) string {
	t.Helper()
	h := r.Header
	h.CodeVersion = 0
	b, err := json.Marshal(struct {
		Header        Header        `json:"header"`
		MatchFeedback []MatchUpdate `json:"matchFeedback"`
	}{h, r.MatchFeedback})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// No 9734089 replay is available, so this rewrites real Y11S2_Alpha04 rounds
// with that build's tags and requires the same output as the original.
func TestReadPlayer_Y11S2Alpha03Tags(t *testing.T) {
	for _, path := range []string{
		"test/data/replays/valid/Y11S2/custom_early_plant.rec",
		"test/data/replays/valid/Y11S2/custom_solid_snake.rec",
	} {
		t.Run(path, func(t *testing.T) {
			want, err := readRoundRetagged(t, path, false)
			if !Ok(err) {
				t.Fatal(err)
			}
			got, err := readRoundRetagged(t, path, true)
			if err != nil {
				t.Fatalf("Read() = %v, want nil", err)
			}
			if len(got.Header.Players) != len(want.Header.Players) {
				t.Fatalf("players = %d, want %d", len(got.Header.Players), len(want.Header.Players))
			}
			if len(got.MatchFeedback) != len(want.MatchFeedback) {
				t.Fatalf("matchFeedback = %d, want %d", len(got.MatchFeedback), len(want.MatchFeedback))
			}
			if g, w := roundJSON(t, got), roundJSON(t, want); g != w {
				t.Errorf("retagged round differs from the original\n got: %s\nwant: %s", g, w)
			}
		})
	}
}

// A build that moves only one of the two tags is read in full too.
func TestReadPlayer_OneTagMoved(t *testing.T) {
	path := "test/data/replays/valid/Y11S2/custom_early_plant.rec"
	want, err := readRoundRetagged(t, path, false)
	if !Ok(err) {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name       string
		id, uiOnly bool
	}{{"only the id tag moved", true, false}, {"only the uiID tag moved", false, true}} {
		t.Run(c.name, func(t *testing.T) {
			got, err := readRoundRetaggedParts(t, path, c.id, c.uiOnly)
			if err != nil {
				t.Fatalf("Read() = %v, want nil", err)
			}
			if g, w := roundJSON(t, got), roundJSON(t, want); g != w {
				t.Errorf("retagged round differs from the original\n got: %s\nwant: %s", g, w)
			}
		})
	}
}
