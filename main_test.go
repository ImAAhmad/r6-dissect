package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/redraskal/r6-dissect/dissect"
	"github.com/rs/zerolog"
)

const earlyPlant = "dissect/test/data/replays/valid/Y11S2/custom_early_plant.rec"

func TestMain(m *testing.M) {
	zerolog.SetGlobalLevel(zerolog.Disabled)
	os.Exit(m.Run())
}

func openMatchFolder(t *testing.T, files map[string][]byte) *os.File {
	t.Helper()
	dir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(earlyPlant)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// lastByteFailure returns the fixture with its round data cut right after the last
// clock pattern, so readTime fails on the last byte of the round.
func lastByteFailure(t *testing.T) []byte {
	t.Helper()
	raw := fixture(t)
	r, err := dissect.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if _, err := r.Write(&data); err != nil {
		t.Fatal(err)
	}
	b := data.Bytes()
	clock := []byte{0x1F, 0x07, 0xEF, 0xC9}
	last := bytes.LastIndex(b, clock)
	headerEnd := bytes.Index(raw, []byte{0x28, 0xB5, 0x2F, 0xFD})
	if last < 0 || headerEnd < 0 {
		t.Fatal("no clock packet or no zstd frame in the fixture")
	}
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	return enc.EncodeAll(b[:last+len(clock)], bytes.Clone(raw[:headerEnd]))
}

// the readTime warning of a lastByteFailure round
var lastByteWarning = dissect.ReadWarning{Listener: "readTime", Pattern: "1F 07 EF C9", Error: "EOF", Count: 1}

func hasWarning(ws []dissect.ReadWarning, want dissect.ReadWarning) bool {
	for _, w := range ws {
		w.FirstOffset = 0
		if w == want {
			return true
		}
	}
	return false
}

func TestWriteRound_ListenerFailureIsInWarnings(t *testing.T) {
	var buf bytes.Buffer
	if err := writeRound(bytes.NewReader(lastByteFailure(t)), &buf); err != nil {
		t.Fatalf("writeRound: %v", err)
	}
	var out struct {
		Warnings []dissect.ReadWarning `json:"warnings"`
		Teams    []struct {
			Won bool `json:"won"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !hasWarning(out.Warnings, lastByteWarning) {
		t.Fatalf("warnings = %+v, want %+v", out.Warnings, lastByteWarning)
	}
	if len(out.Teams) != 2 || out.Teams[0].Won == out.Teams[1].Won {
		t.Fatalf("teams = %+v, want one winner: the round end must still run", out.Teams)
	}
}

func TestWriteRound_CleanRoundHasNoWarningsKey(t *testing.T) {
	var buf bytes.Buffer
	if err := writeRound(bytes.NewReader(fixture(t)), &buf); err != nil {
		t.Fatalf("writeRound: %v", err)
	}
	if bytes.Contains(buf.Bytes(), []byte(`"warnings"`)) {
		t.Fatal(`clean round JSON contains "warnings"`)
	}
}

func TestWriteMatch_ListenerFailureIsInWarnings(t *testing.T) {
	in := openMatchFolder(t, map[string][]byte{"R01.rec": lastByteFailure(t), "R02.rec": fixture(t)})
	var buf bytes.Buffer
	if err := writeMatch(in, JSON, &buf); err != nil {
		t.Fatalf("writeMatch: %v", err)
	}
	var out struct {
		Rounds []struct {
			Warnings []dissect.ReadWarning `json:"warnings"`
		} `json:"rounds"`
		SkippedRounds json.RawMessage `json:"skippedRounds"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Rounds) != 2 || out.SkippedRounds != nil {
		t.Fatalf("rounds = %d, skippedRounds = %s; want 2 rounds and none skipped", len(out.Rounds), out.SkippedRounds)
	}
	if !hasWarning(out.Rounds[0].Warnings, lastByteWarning) {
		t.Fatalf("round 1 warnings = %+v, want %+v", out.Rounds[0].Warnings, lastByteWarning)
	}
	if out.Rounds[1].Warnings != nil {
		t.Fatalf("round 2 warnings = %+v, want none", out.Rounds[1].Warnings)
	}
}

func TestWriteMatch_SkipsEmptyRoundFile(t *testing.T) {
	in := openMatchFolder(t, map[string][]byte{"R01.rec": fixture(t), "R02.rec": {}})
	var buf bytes.Buffer
	if err := writeMatch(in, JSON, &buf); err != nil {
		t.Fatalf("writeMatch: %v", err)
	}
	var out struct {
		Rounds        []json.RawMessage `json:"rounds"`
		SkippedRounds []struct {
			File string `json:"file"`
		} `json:"skippedRounds"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Rounds) != 1 || len(out.SkippedRounds) != 1 || out.SkippedRounds[0].File != "R02.rec" {
		t.Fatalf("rounds=%d skipped=%+v", len(out.Rounds), out.SkippedRounds)
	}
}

func TestWriteMatch_FailsWhenNoRoundIsReadable(t *testing.T) {
	in := openMatchFolder(t, map[string][]byte{"R01.rec": {}, "R02.rec": {}})
	var buf bytes.Buffer
	if err := writeMatch(in, JSON, &buf); err == nil {
		t.Fatal("writeMatch = nil, want error")
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes, want 0", buf.Len())
	}
}

func TestPrintHead_EmptyFirstRoundFile(t *testing.T) {
	in := openMatchFolder(t, map[string][]byte{"R01.rec": {}, "R02.rec": fixture(t)})
	err := printHead(in)
	var re *dissect.RoundError
	if !errors.As(err, &re) {
		t.Fatalf("printHead = %v, want *RoundError", err)
	}
}
