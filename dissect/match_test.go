package dissect

import "testing"

// a second callback on the same pattern joins the first instead of adding the pattern again.
func TestMatchReaderListen_SamePattern(t *testing.T) {
	m := &MatchReader{}
	p := []byte{0x01, 0x02}
	m.Listen(p, func(*Reader) error { return nil })
	m.Listen(p, func(*Reader) error { return nil })
	if len(m.queries) != 1 || len(m.listeners[0]) != 2 {
		t.Fatalf("want 1 pattern with 2 callbacks, got %d patterns, %d callbacks", len(m.queries), len(m.listeners[0]))
	}
}
