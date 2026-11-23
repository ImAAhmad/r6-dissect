package dissect

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func openChunkedReader(t *testing.T) *Reader {
	t.Helper()
	raw, err := os.ReadFile(chunkedFixture)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func marshalString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// #30: a round without events is written as "matchFeedback": [], never null.
func TestEmptyRound_MatchFeedbackIsArray(t *testing.T) {
	t.Run("reader", func(t *testing.T) {
		r := openChunkedReader(t)
		if got := marshalString(t, r); !strings.Contains(got, `"matchFeedback":[]`) {
			t.Fatalf("reader JSON lacks an empty matchFeedback array: %.200s", got)
		}
	})
	t.Run("reader after a partial read", func(t *testing.T) {
		r := openChunkedReader(t)
		if err := r.ReadPartial(); !Ok(err) {
			t.Fatal(err)
		}
		if len(r.MatchFeedback) != 0 {
			t.Skipf("the partial read produced %d events", len(r.MatchFeedback))
		}
		if got := marshalString(t, r); !strings.Contains(got, `"matchFeedback":[]`) {
			t.Fatalf("reader JSON lacks an empty matchFeedback array: %.200s", got)
		}
	})
	t.Run("match output", func(t *testing.T) {
		m := &MatchReader{rounds: []*Reader{openChunkedReader(t)}}
		got := marshalString(t, m.Data())
		if !strings.Contains(got, `"matchFeedback":[]`) || strings.Contains(got, `"matchFeedback":null`) {
			t.Fatalf("match JSON lacks an empty matchFeedback array: %.300s", got)
		}
	})
	t.Run("one event stays an array of one", func(t *testing.T) {
		r := openChunkedReader(t)
		r.MatchFeedback = append(r.MatchFeedback, MatchUpdate{Type: Kill})
		m := &MatchReader{rounds: []*Reader{r}}
		var out struct {
			Rounds []struct {
				MatchFeedback []json.RawMessage `json:"matchFeedback"`
			} `json:"rounds"`
		}
		if err := json.Unmarshal([]byte(marshalString(t, m.Data())), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Rounds) != 1 || len(out.Rounds[0].MatchFeedback) != 1 {
			t.Fatalf("want 1 round with 1 event, got %+v", out)
		}
	})
	t.Run("unreadable rounds never reach the output", func(t *testing.T) {
		m := &MatchReader{rounds: []*Reader{nil, nil}}
		got := marshalString(t, m.Data())
		if !strings.Contains(got, `"rounds":[]`) || strings.Contains(got, "null") {
			t.Fatalf("want an empty rounds array and no null, got %.300s", got)
		}
	})
}
