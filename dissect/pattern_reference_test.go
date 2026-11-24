package dissect

import (
	"bytes"
	"cmp"
	"io"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
)

// referenceMatches is the brute-force definition findQueries must meet: for each
// query, every leftmost non-overlapping occurrence in b[start:end], reported by
// the index of its last byte, ordered by offset and then by query index.
func referenceMatches(b []byte, queries [][]byte, start, end int) []match {
	var out []match
	for j, q := range queries {
		for i := start; i+len(q) <= end; {
			if bytes.Equal(b[i:i+len(q)], q) {
				out = append(out, match{offset: i + len(q) - 1, listenerIndex: j})
				i += len(q)
			} else {
				i++
			}
		}
	}
	slices.SortFunc(out, func(a, b match) int {
		return cmp.Or(cmp.Compare(a.offset, b.offset), cmp.Compare(a.listenerIndex, b.listenerIndex))
	})
	return out
}

// randomCase draws bytes and queries from a 3-letter alphabet so that partial
// matches, repeated prefixes and self-overlapping queries are common.
func randomCase(rng *rand.Rand) ([]byte, [][]byte, int, int) {
	alphabet := []byte{0xA1, 0xB2, 0xC3}
	n := 1 + rng.IntN(400)
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[rng.IntN(len(alphabet))]
	}
	queries := make([][]byte, 1+rng.IntN(4))
	for i := range queries {
		q := make([]byte, 1+rng.IntN(5))
		for k := range q {
			q[k] = alphabet[rng.IntN(len(alphabet))]
		}
		queries[i] = q
	}
	start := rng.IntN(n)
	end := start + rng.IntN(n-start+1)
	return b, queries, start, end
}

func TestFindQueries_MatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 2026))
	for c := 0; c < 20000; c++ {
		b, queries, start, end := randomCase(rng)
		r := &Reader{b: b, queries: queries}
		got := r.findQueries(start, end)
		want := referenceMatches(b, queries, start, end)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("case %d: b=%X queries=%X [%d,%d)\n got %v\nwant %v", c, b, queries, start, end, got, want)
		}
	}
}

func TestFindQueries_OutOfRange(t *testing.T) {
	r := &Reader{b: []byte{1, 2, 3}, queries: [][]byte{{1}}}
	for _, c := range [][2]int{{0, 0}, {3, 3}, {2, 1}, {-1, 2}, {0, 4}} {
		if got := r.findQueries(c[0], c[1]); got != nil {
			t.Errorf("findQueries(%d, %d) = %v, want nil", c[0], c[1], got)
		}
	}
}

func TestFindQueries_EmptyQueryMatchesNothing(t *testing.T) {
	r := &Reader{b: []byte{1, 2, 3}, queries: [][]byte{{}, {2}}}
	want := []match{{offset: 1, listenerIndex: 1}}
	if got := r.findQueries(0, 3); !reflect.DeepEqual(got, want) {
		t.Fatalf("findQueries(0, 3) = %v, want %v", got, want)
	}
}

// referenceSeek is the definition Seek must meet: the first occurrence of
// pattern that starts at or after start and ends before the last byte of b
// (the window the byte walk examined), leaving the offset just past it.
// Without one, the offset moves to len(b), or one past start when start was
// already at the end.
func referenceSeek(b, pattern []byte, start int) (int, error) {
	for i := start; i+len(pattern) <= len(b)-1; i++ {
		if bytes.Equal(b[i:i+len(pattern)], pattern) {
			return i + len(pattern), nil
		}
	}
	if start >= len(b) {
		return start + 1, io.EOF
	}
	return len(b), io.EOF
}

func TestSeek_MatchesReference(t *testing.T) {
	quiet(t) // every miss logs a large seek
	rng := rand.New(rand.NewPCG(7, 2026))
	for c := 0; c < 20000; c++ {
		b, queries, start, _ := randomCase(rng)
		if rng.IntN(10) == 0 {
			start = len(b) // a Seek from the end of the buffer
		}
		pattern := queries[0]
		r := &Reader{b: b, offset: start}
		err := r.Seek(pattern)
		wantOffset, wantErr := referenceSeek(b, pattern, start)
		if err != wantErr || r.offset != wantOffset {
			t.Fatalf("case %d: b=%X pattern=%X start=%d\n got (%v, offset %d)\nwant (%v, offset %d)", c, b, pattern, start, err, r.offset, wantErr, wantOffset)
		}
	}
}
