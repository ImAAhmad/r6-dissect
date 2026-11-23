package dissect

import (
	"bytes"
	"io"
	"reflect"
	"testing"
)

// hit is one listener call: which listener ran and where r.offset pointed.
type hit struct {
	listener int
	offset   int
}

// scanHits runs Read over b with one recording listener per pattern and
// returns the listener calls in the order Read made them.
func scanHits(t *testing.T, b []byte, patterns ...[]byte) []hit {
	t.Helper()
	r := &Reader{readPartial: true}
	var hits []hit
	for i, p := range patterns {
		r.Listen(p, func(r *Reader) error {
			hits = append(hits, hit{i, r.offset})
			return nil
		})
	}
	// readPartial scans only the first third of r.b and skips the end-of-round
	// processing; pad to 3x so exactly b is scanned.
	r.b = append(append([]byte{}, b...), make([]byte, 2*len(b))...)
	if err := r.Read(); err != nil {
		t.Fatal(err)
	}
	return hits
}

// plant returns n zero bytes with p written at off.
func plant(n, off int, p []byte) []byte {
	b := make([]byte, n)
	copy(b[off:], p)
	return b
}

func TestRead_PatternAcrossWorkerBlockBoundary(t *testing.T) {
	p := []byte{0xA1, 0xB2, 0xC3, 0xD4}
	// The old scanner split 1000 bytes into 5 worker blocks: [0,200], [201,400], ...
	// The pattern occupies 199..202, across the 200/201 boundary.
	got := scanHits(t, plant(1000, 199, p), p)
	want := []hit{{0, 203}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

func TestRead_PatternAfterRepeatedFirstByte(t *testing.T) {
	p := []byte{0xA1, 0xB2, 0xC3, 0xD4}
	// A1 A1 B2 C3 D4: the first A1 starts a partial match that fails on the second A1.
	got := scanHits(t, plant(1000, 10, append([]byte{0xA1}, p...)), p)
	want := []hit{{0, 15}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

func TestRead_SelfOverlappingPrefix(t *testing.T) {
	p := []byte{0xA1, 0xB2, 0xA1, 0xC3}
	// A1 B2 A1 B2 A1 C3: the match starts at the second A1, inside a failed partial.
	in := []byte{0xA1, 0xB2, 0xA1, 0xB2, 0xA1, 0xC3}
	got := scanHits(t, plant(1000, 10, in), p)
	want := []hit{{0, 16}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

func TestRead_TiedOffsetsRunInListenerOrder(t *testing.T) {
	short := []byte{0xC3, 0xD4}
	long := []byte{0xA1, 0xB2, 0xC3, 0xD4}
	// Both patterns end on the same byte; the listener registered first runs first.
	got := scanHits(t, plant(1000, 10, long), short, long)
	want := []hit{{0, 14}, {1, 14}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

func TestRead_RepeatedPrefixRun(t *testing.T) {
	// 22 22 22 07: each match starts at the second 22, inside a failed partial match.
	p := []byte{0x22, 0x22, 0x07}
	b := bytes.Repeat([]byte{0x22, 0x22, 0x22, 0x07, 0x00}, 200)
	got := scanHits(t, b, p)
	if len(got) != 200 {
		t.Fatalf("got %d hits, want 200", len(got))
	}
}

func TestRead_MatchesRunInOffsetOrder(t *testing.T) {
	first := []byte{0xA1, 0xB2, 0xC3, 0xD4}
	second := []byte{0xE5, 0xF6, 0x07, 0x18}
	// The listener registered second has the earlier match, so it runs first.
	b := plant(1000, 500, first)
	copy(b[10:], second)
	copy(b[900:], second)
	got := scanHits(t, b, first, second)
	want := []hit{{1, 14}, {0, 504}, {1, 904}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

func TestRead_PatternAtScanRangeEdges(t *testing.T) {
	p := []byte{0xA1, 0xB2, 0xC3, 0xD4}
	// p at offset 0, then two adjacent copies, the last ending on the last scanned byte.
	b := plant(100, 0, p)
	copy(b[92:], p)
	copy(b[96:], p)
	got := scanHits(t, b, p)
	want := []hit{{0, 4}, {0, 96}, {0, 100}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

// Seek walked r.Bytes(1) and, on a mismatch, reset without trying the breaking
// byte as a new start, so it skipped a match that began inside a failed partial
// match of the same pattern.
func TestSeek_MatchInsideFailedPartial(t *testing.T) {
	cases := []struct {
		name       string
		b, pattern []byte
		wantOffset int
	}{
		{"repeated first byte", []byte{0x22, 0x22, 0x07, 0x00}, []byte{0x22, 0x07}, 3},
		{"self-overlapping prefix", []byte{0xA1, 0xB2, 0xA1, 0xB2, 0xA1, 0xC3, 0x00}, []byte{0xA1, 0xB2, 0xA1, 0xC3}, 6},
		{"earlier of two", []byte{0x22, 0x22, 0x07, 0x22, 0x07, 0x00}, []byte{0x22, 0x07}, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &Reader{b: c.b}
			err := r.Seek(c.pattern)
			if err != nil || r.offset != c.wantOffset {
				t.Fatalf("got (%v, offset %d), want (<nil>, offset %d)", err, r.offset, c.wantOffset)
			}
		})
	}
}

// TestSeek_KeepsWalkResults pins what the byte walk returned where it did not
// miss a match, including its window: r.Bytes(1) reports io.EOF on reaching the
// last byte of r.b, so the walk never examined that byte.
func TestSeek_KeepsWalkResults(t *testing.T) {
	cases := []struct {
		name       string
		b, pattern []byte
		start      int
		wantErr    error
		wantOffset int
	}{
		{"found", []byte{0, 1, 2, 3, 4}, []byte{2, 3}, 0, nil, 4},
		{"found at start", []byte{2, 3, 0, 0}, []byte{2, 3}, 0, nil, 2},
		{"first of two", []byte{2, 3, 2, 3, 0}, []byte{2, 3}, 0, nil, 2},
		{"before start is not found", []byte{2, 3, 0, 0, 0}, []byte{2, 3}, 1, io.EOF, 5},
		{"absent", []byte{1, 2, 3, 4, 5}, []byte{9, 9}, 1, io.EOF, 5},
		{"ending on the last byte is not found", []byte{0, 0, 2, 3}, []byte{2, 3}, 0, io.EOF, 4},
		{"ending one before the last byte", []byte{0, 2, 3, 0}, []byte{2, 3}, 0, nil, 3},
		{"offset on the last byte", []byte{1, 2}, []byte{2}, 1, io.EOF, 2},
		{"offset at end", []byte{1, 2}, []byte{1}, 2, io.EOF, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &Reader{b: c.b, offset: c.start}
			err := r.Seek(c.pattern)
			if err != c.wantErr || r.offset != c.wantOffset {
				t.Fatalf("got (%v, offset %d), want (%v, offset %d)", err, r.offset, c.wantErr, c.wantOffset)
			}
		})
	}
}
