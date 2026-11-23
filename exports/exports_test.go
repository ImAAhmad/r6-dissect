package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/redraskal/r6-dissect/dissect"
	"github.com/rs/zerolog"
)

const earlyPlant = "../dissect/test/data/replays/valid/Y11S2/custom_early_plant.rec"

func TestMain(m *testing.M) {
	zerolog.SetGlobalLevel(zerolog.Disabled)
	os.Exit(m.Run())
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

// writeFiles writes files into a temp dir and returns its path.
func writeFiles(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestReadJSON_RoundWarnings(t *testing.T) {
	dir := writeFiles(t, map[string][]byte{"R01.rec": lastByteFailure(t)})
	var out struct {
		Data struct {
			Warnings []dissect.ReadWarning `json:"warnings"`
		} `json:"data"`
		Error string `json:"error"`
	}
	res := readJSON(filepath.Join(dir, "R01.rec"))
	if err := json.Unmarshal([]byte(res), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "" || !hasWarning(out.Data.Warnings, lastByteWarning) {
		t.Fatalf("error = %q, warnings = %+v; want %+v", out.Error, out.Data.Warnings, lastByteWarning)
	}
}

func TestReadJSON_CleanRoundHasNoWarningsKey(t *testing.T) {
	res := readJSON(earlyPlant)
	if !bytes.HasPrefix([]byte(res), []byte(`{"data":`)) || bytes.Contains([]byte(res), []byte(`"warnings"`)) {
		t.Fatalf("want data without a warnings key, got %.200s", res)
	}
}

func TestReadJSON_MatchRoundWarnings(t *testing.T) {
	dir := writeFiles(t, map[string][]byte{"R01.rec": lastByteFailure(t), "R02.rec": fixture(t)})
	var out struct {
		Data struct {
			Rounds []struct {
				Warnings []dissect.ReadWarning `json:"warnings"`
			} `json:"rounds"`
			SkippedRounds json.RawMessage `json:"skippedRounds"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(readJSON(dir)), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "" || len(out.Data.Rounds) != 2 || out.Data.SkippedRounds != nil {
		t.Fatalf("error = %q, rounds = %d, skippedRounds = %s; want 2 rounds and none skipped", out.Error, len(out.Data.Rounds), out.Data.SkippedRounds)
	}
	if !hasWarning(out.Data.Rounds[0].Warnings, lastByteWarning) {
		t.Fatalf("round 1 warnings = %+v, want %+v", out.Data.Rounds[0].Warnings, lastByteWarning)
	}
	if out.Data.Rounds[1].Warnings != nil {
		t.Fatalf("round 2 warnings = %+v, want none", out.Data.Rounds[1].Warnings)
	}
}
