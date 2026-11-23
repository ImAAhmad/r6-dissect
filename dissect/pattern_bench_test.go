package dissect

import (
	"bytes"
	"os"
	"testing"
)

// benchRound returns the round file the read benchmarks use: $BENCH_REC, or a
// committed fixture.
func benchRound(b *testing.B) []byte {
	b.Helper()
	path := os.Getenv("BENCH_REC")
	if path == "" {
		path = "test/data/replays/valid/Y11S2/custom_solid_snake.rec"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	return raw
}

// BenchmarkReadScan times Read with every listener replaced by a no-op, so it
// measures the pattern search, the dispatch and the end-of-round work on an
// empty round.
func BenchmarkReadScan(b *testing.B) {
	quiet(b)
	src, err := NewReader(bytes.NewReader(benchRound(b)))
	if err != nil {
		b.Fatal(err)
	}
	noop := func(*Reader) error { return nil }
	listeners := make([][]func(*Reader) error, len(src.listeners))
	for j := range listeners {
		listeners[j] = []func(*Reader) error{noop}
	}
	b.SetBytes(int64(len(src.b) - src.offset))
	b.ReportAllocs()
	for b.Loop() {
		r := *src // Read sets r.b to nil on the copy only
		r.listeners = listeners
		if err := r.Read(); !Ok(err) {
			b.Fatal(err)
		}
	}
}

// BenchmarkReadRound times NewReader and Read of one round, end to end.
func BenchmarkReadRound(b *testing.B) {
	quiet(b)
	raw := benchRound(b)
	b.ReportAllocs()
	for b.Loop() {
		r, err := NewReader(bytes.NewReader(raw))
		if err != nil {
			b.Fatal(err)
		}
		if err := r.Read(); !Ok(err) {
			b.Fatal(err)
		}
	}
}
