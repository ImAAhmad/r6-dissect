package dissect

import (
	"bytes"
	"encoding/binary"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

const (
	chunkedFixture    = "test/data/replays/valid/Y11S2/custom_early_plant.rec" // Y8S4+: chunked zstd sections
	nonChunkedFixture = "test/data/replays/valid/Y8S1/ranked_1.rec"            // pre-Y8S4: one zstd stream
)

var matchFixtureDirs = []string{"test/data/replays/valid/Y8S2", "test/data/replays/valid/Y11S2"}

// quiet silences the debug logs for the test and restores the previous level after it.
func quiet(t testing.TB) {
	t.Helper()
	prev := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	t.Cleanup(func() { zerolog.SetGlobalLevel(prev) })
}

func settledGoroutines() int {
	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
	}
	return runtime.NumGoroutine()
}

func readOnce(t *testing.T, path string) {
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
	if err := r.Read(); !Ok(err) {
		t.Fatal(err)
	}
}

// frameWithFCS hand-writes a single-segment zstd frame header declaring an 8-byte
// content size, followed by one empty raw last block.
func frameWithFCS(size uint64) []byte {
	b := []byte{0x28, 0xB5, 0x2F, 0xFD, 0xE0}
	b = binary.LittleEndian.AppendUint64(b, size)
	return append(b, 0x01, 0x00, 0x00)
}

func TestNewReader_DoesNotLeakGoroutines(t *testing.T) {
	quiet(t)
	for _, tc := range []struct{ name, path string }{
		{"chunked", chunkedFixture},
		{"nonChunked", nonChunkedFixture},
	} {
		t.Run(tc.name, func(t *testing.T) {
			readOnce(t, tc.path) // warm up lazily started runtime goroutines
			before := settledGoroutines()
			const n = 10
			for i := 0; i < n; i++ {
				readOnce(t, tc.path)
			}
			after := settledGoroutines()
			t.Logf("GOMAXPROCS=%d goroutines before=%d after=%d (%d reads)", runtime.GOMAXPROCS(0), before, after, n)
			if after > before {
				t.Errorf("goroutines grew by %d over %d reads (leaked zstd decoder)", after-before, n)
			}
		})
	}
}

func TestNewReader_CorruptInputDoesNotLeakGoroutines(t *testing.T) {
	quiet(t)
	for _, path := range []string{chunkedFixture, nonChunkedFixture} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"truncate", "corrupt"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				b := append([]byte(nil), raw...)
				switch mode {
				case "truncate":
					b = b[:len(b)/2]
				case "corrupt":
					for i := len(b) / 3; i < len(b)/3+4096; i++ {
						b[i] ^= 0x5A
					}
				}
				before := settledGoroutines()
				errs := 0
				for i := 0; i < 10; i++ {
					r, err := NewReader(bytes.NewReader(b))
					if err == nil {
						err = r.Read()
					}
					if err != nil {
						errs++
					}
				}
				after := settledGoroutines()
				t.Logf("before=%d after=%d errors=%d/10", before, after, errs)
				if after > before {
					t.Errorf("goroutines grew by %d", after-before)
				}
			})
		}
	}
}

func TestMatchReader_DoesNotPinRoundBuffers(t *testing.T) {
	quiet(t)
	for _, dir := range matchFixtureDirs {
		t.Run(dir, func(t *testing.T) {
			f, err := os.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			m, err := NewMatchReader(f)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Read(); !Ok(err) {
				t.Fatal(err)
			}
			runtime.GC()
			runtime.GC()
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			t.Logf("rounds=%d live heap after Read+GC: %.1f MiB", m.NumRounds(), float64(ms.HeapAlloc)/(1<<20))
			for i := 0; i < m.NumRounds(); i++ {
				r, _ := m.RoundAt(i)
				for _, p := range r.Header.Players {
					if cap(p.DissectID) > 16 {
						t.Errorf("round %d player %q: DissectID cap=%d, still a window into the round buffer", i, p.Username, cap(p.DissectID))
					}
				}
				for _, p := range r.Scoreboard.Players {
					if cap(p.ID) > 16 {
						t.Errorf("round %d: Scoreboard ID cap=%d, still a window into the round buffer", i, cap(p.ID))
					}
				}
			}
			const limit = 32 << 20
			if ms.HeapAlloc > limit {
				t.Errorf("live heap %d MiB > %d MiB after Read: round buffers are still reachable", ms.HeapAlloc>>20, limit>>20)
			}
			runtime.KeepAlive(m)
		})
	}
}

func TestDecompressedSizeHint(t *testing.T) {
	quiet(t)
	for _, tc := range []struct{ name, path string }{
		{"chunked", chunkedFixture},
		{"nonChunked", nonChunkedFixture},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			r, err := NewReader(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if h := decompressedSizeHint(raw); h != len(r.b) {
				t.Errorf("hint %d != decompressed length %d", h, len(r.b))
			}
		})
	}
	t.Run("no_fcs", func(t *testing.T) {
		if h := decompressedSizeHint([]byte{0x28, 0xB5, 0x2F, 0xFD, 0x00, 0x00}); h != 0 {
			t.Errorf("hint %d, want 0", h)
		}
	})
	t.Run("sums_frames", func(t *testing.T) {
		b := append(frameWithFCS(100), frameWithFCS(200)...)
		if h := decompressedSizeHint(b); h != 300 {
			t.Errorf("hint %d, want 300", h)
		}
	})
	t.Run("garbage_and_cap", func(t *testing.T) {
		g := append(frameWithFCS(1<<40), make([]byte, 64-16)...)
		if h := decompressedSizeHint(g); h != maxSizeHintRatio*len(g) {
			t.Errorf("hint %d, want the cap %d", h, maxSizeHintRatio*len(g))
		}
	})
	t.Run("cap_across_frames", func(t *testing.T) {
		// each frame is under the cap alone (32*48 = 1536 > 600), the sum (1800) is over it
		var g []byte
		for i := 0; i < 3; i++ {
			g = append(g, frameWithFCS(600)...)
		}
		if h := decompressedSizeHint(g); h != maxSizeHintRatio*len(g) {
			t.Errorf("hint %d, want the cap %d", h, maxSizeHintRatio*len(g))
		}
	})
}

func TestReadChunkedData_AllocationBound(t *testing.T) {
	quiet(t)
	raw, err := os.ReadFile(chunkedFixture)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	r, err := NewReader(bytes.NewReader(raw))
	runtime.ReadMemStats(&b)
	if err != nil {
		t.Fatal(err)
	}
	delta := b.TotalAlloc - a.TotalAlloc
	bound := uint64(2*len(r.b) + 2*len(raw))
	t.Logf("allocated %d MB for %d MB output", delta>>20, len(r.b)>>20)
	if delta > bound {
		t.Errorf("allocated %d > bound %d", delta, bound)
	}
}

func BenchmarkNewReader(b *testing.B) {
	quiet(b)
	for _, path := range []string{chunkedFixture, nonChunkedFixture} {
		raw, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(path, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := NewReader(bytes.NewReader(raw)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
