package dissect

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const failureFixture = "test/data/replays/valid/Y11S2/custom_early_plant.rec"

var (
	clockPattern  = []byte{0x1F, 0x07, 0xEF, 0xC9}       // readTime, Y8S1+
	playerPattern = []byte{0x22, 0x07, 0x94, 0x9B, 0xDC} // readPlayer
	// absentPattern occurs in no fixture, so a Seek for it runs to EOF.
	absentPattern = []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x03, 0x03, 0x03, 0x03, 0xFE, 0xED}

	errA = errors.New("a")
	errB = errors.New("b")
)

// seekAbsent is a listener whose Seek always runs to EOF, like a listener whose marker moved.
func seekAbsent(r *Reader) error { return r.Seek(absentPattern) }

func failA(*Reader) error { return errA }

// failByOffset fails with errA before offset 40 and with errB from there on.
func failByOffset(r *Reader) error {
	if r.offset < 40 {
		return errA
	}
	return errB
}

func openFixture(t *testing.T, path string) *Reader {
	t.Helper()
	quiet(t)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(r.b, absentPattern) {
		t.Fatalf("%s contains absentPattern; pick another", path)
	}
	return r
}

// scanReader returns a Reader that scans exactly b and skips the end-of-round
// processing (see scanHits).
func scanReader(b []byte) *Reader {
	r := &Reader{readPartial: true}
	r.b = append(append([]byte{}, b...), make([]byte, 2*len(b))...)
	return r
}

// occurrences returns where each match of p starts in r.b[r.offset:], stepping the way findQueries does.
func occurrences(r *Reader, p []byte) []int {
	var starts []int
	for i := r.offset; ; {
		j := bytes.Index(r.b[i:], p)
		if j < 0 {
			return starts
		}
		starts = append(starts, i+j)
		i += j + len(p)
	}
}

// captureLog sends the global logger's output at Warn level and above to the returned buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	prevLogger, prevLevel := log.Logger, zerolog.GlobalLevel()
	t.Cleanup(func() {
		log.Logger = prevLogger
		zerolog.SetGlobalLevel(prevLevel)
	})
	var buf bytes.Buffer
	log.Logger = zerolog.New(&buf)
	zerolog.SetGlobalLevel(zerolog.WarnLevel)
	return &buf
}

// One listener hits EOF halfway through the round. Everything the other listeners read,
// before and after that point, and the end-of-round work, must be the same as a clean read.
func TestRead_ListenerEOFDoesNotStopRound(t *testing.T) {
	clean := openFixture(t, failureFixture)
	ticks := 0
	clean.Listen(clockPattern, func(*Reader) error { ticks++; return nil })
	if err := clean.Read(); err != nil {
		t.Fatalf("clean Read: %v", err)
	}
	if clean.Warnings != nil {
		t.Fatalf("clean read: want no warnings, got %+v", clean.Warnings)
	}
	faulted := openFixture(t, failureFixture)
	calls := 0
	faulted.Listen(clockPattern, func(r *Reader) error {
		calls++
		if calls == ticks/2 {
			return r.Seek(absentPattern)
		}
		return nil
	})
	if err := faulted.Read(); err != nil {
		t.Fatalf("Read returned %v; a listener failure must not fail the round", err)
	}
	if !reflect.DeepEqual(faulted.MatchFeedback, clean.MatchFeedback) {
		t.Fatalf("matchFeedback differs: got %d events, want %d", len(faulted.MatchFeedback), len(clean.MatchFeedback))
	}
	if !reflect.DeepEqual(faulted.Header, clean.Header) {
		t.Fatal("header differs (winner, loadouts or players)")
	}
	if !reflect.DeepEqual(faulted.Scoreboard, clean.Scoreboard) {
		t.Fatal("scoreboard differs")
	}
	if faulted.b != nil {
		t.Fatal("Read did not finish: the buffer was not released")
	}
	if len(faulted.Warnings) != 1 || faulted.Warnings[0].Count != 1 || faulted.Warnings[0].Error != io.EOF.Error() {
		t.Fatalf("want one EOF warning with count 1, got %+v", faulted.Warnings)
	}
}

// Failures of one listener are merged into one warning that names it, and the
// listener registered before it on the same pattern keeps its work.
func TestRead_ListenerFailuresAreRecorded(t *testing.T) {
	r := openFixture(t, failureFixture)
	starts := occurrences(r, playerPattern)
	if len(starts) == 0 {
		t.Fatal("no player packets in the fixture")
	}
	r.Listen(playerPattern, seekAbsent)
	if err := r.Read(); err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []ReadWarning{{Listener: "seekAbsent", Pattern: "22 07 94 9B DC", Error: "EOF", Count: len(starts), FirstOffset: starts[0]}}
	if !reflect.DeepEqual(r.Warnings, want) {
		t.Fatalf("warnings:\n got %+v\nwant %+v", r.Warnings, want)
	}
	if len(r.Header.Players) != 10 {
		t.Fatalf("readPlayer shares the pattern and must still run: got %d players", len(r.Header.Players))
	}
}

// A listener error that is not EOF is a per-packet failure too.
func TestRead_ListenerHardErrorDoesNotStopRound(t *testing.T) {
	clean := openFixture(t, failureFixture)
	if err := clean.Read(); err != nil {
		t.Fatal(err)
	}
	r := openFixture(t, failureFixture)
	n := len(occurrences(r, playerPattern))
	r.Listen(playerPattern, func(*Reader) error { return errors.New("boom") })
	if err := r.Read(); err != nil {
		t.Fatalf("Read returned %v; want nil", err)
	}
	if !reflect.DeepEqual(r.MatchFeedback, clean.MatchFeedback) {
		t.Fatalf("matchFeedback differs: got %d events, want %d", len(r.MatchFeedback), len(clean.MatchFeedback))
	}
	if !reflect.DeepEqual(r.Header, clean.Header) {
		t.Fatal("header differs (winner, loadouts or players)")
	}
	if len(r.Warnings) != 1 || r.Warnings[0].Error != "boom" || r.Warnings[0].Count != n {
		t.Fatalf("want one \"boom\" warning with count %d, got %+v", n, r.Warnings)
	}
}

// The round ends right after a clock pattern, so readTime fails on the last packet of
// the round: the failure is still recorded and Read still finishes the round.
func TestRead_ListenerErrorOnLastByte(t *testing.T) {
	r := openFixture(t, failureFixture)
	last := bytes.LastIndex(r.b, clockPattern)
	if last < 0 {
		t.Fatal("no clock packet in the fixture")
	}
	r.b = r.b[:last+len(clockPattern)]
	if err := r.Read(); err != nil {
		t.Fatalf("Read returned %v; want nil", err)
	}
	want := ReadWarning{Listener: "readTime", Pattern: "1F 07 EF C9", Error: "EOF", Count: 1, FirstOffset: last}
	found := false
	for _, w := range r.Warnings {
		if w.Listener == "readTime" {
			if w != want {
				t.Fatalf("readTime warning: got %+v, want %+v", w, want)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("no readTime warning in %+v", r.Warnings)
	}
	if !r.roundDecided() {
		t.Fatal("the round has no winner: the end-of-round work did not run")
	}
	if r.b != nil {
		t.Fatal("Read did not finish: the buffer was not released")
	}
}

// Warnings are merged by listener, pattern and error, in the order of their first failure.
func TestRead_WarningsMergePerListenerPatternAndError(t *testing.T) {
	p1 := []byte{0xA1, 0xB2, 0xC3, 0xD4}
	p2 := []byte{0xE5, 0xF6, 0x07, 0x18}
	p3 := []byte{0x29, 0x3A, 0x4B, 0x5C}
	b := plant(100, 10, p1)
	copy(b[50:], p1)
	copy(b[20:], p3)
	copy(b[60:], p3)
	copy(b[30:], p2)
	copy(b[70:], p2)
	r := scanReader(b)
	r.Listen(p1, failA)
	r.Listen(p2, failA)        // the same listener on another pattern
	r.Listen(p3, failByOffset) // another error from the same listener and pattern
	if err := r.Read(); err != nil {
		t.Fatalf("Read returned %v; want nil", err)
	}
	want := []ReadWarning{
		{Listener: "failA", Pattern: "A1 B2 C3 D4", Error: "a", Count: 2, FirstOffset: 10},
		{Listener: "failByOffset", Pattern: "29 3A 4B 5C", Error: "a", Count: 1, FirstOffset: 20},
		{Listener: "failA", Pattern: "E5 F6 07 18", Error: "a", Count: 2, FirstOffset: 30},
		{Listener: "failByOffset", Pattern: "29 3A 4B 5C", Error: "b", Count: 1, FirstOffset: 60},
	}
	if !reflect.DeepEqual(r.Warnings, want) {
		t.Fatalf("warnings:\n got %+v\nwant %+v", r.Warnings, want)
	}
	if r.b != nil {
		t.Fatal("Read did not finish: the buffer was not released")
	}
}

// A failed listener does not stop the next listener on the same match, which still
// starts right after the pattern, nor the listeners of later matches.
func TestRead_FailedListenerDoesNotStopTheNext(t *testing.T) {
	p := []byte{0xA1, 0xB2, 0xC3, 0xD4}
	b := plant(100, 10, p)
	copy(b[50:], p)
	r := scanReader(b)
	var offsets []int
	r.Listen(p, seekAbsent)
	r.Listen(p, func(r *Reader) error {
		offsets = append(offsets, r.offset)
		return nil
	})
	if err := r.Read(); err != nil {
		t.Fatalf("Read returned %v; want nil", err)
	}
	if want := []int{14, 54}; !reflect.DeepEqual(offsets, want) {
		t.Fatalf("second listener offsets = %v, want %v", offsets, want)
	}
	want := []ReadWarning{{Listener: "seekAbsent", Pattern: "A1 B2 C3 D4", Error: "EOF", Count: 2, FirstOffset: 10}}
	if !reflect.DeepEqual(r.Warnings, want) {
		t.Fatalf("warnings:\n got %+v\nwant %+v", r.Warnings, want)
	}
}

// Each failure is logged at Warn level with the listener, pattern, offset and error.
func TestRead_ListenerFailureIsLoggedAtWarn(t *testing.T) {
	logs := captureLog(t)
	p := []byte{0xA1, 0xB2, 0xC3, 0xD4}
	r := scanReader(plant(100, 10, p))
	r.Listen(p, failA)
	if err := r.Read(); err != nil {
		t.Fatalf("Read returned %v; want nil", err)
	}
	out := strings.TrimSpace(logs.String())
	lines := strings.Split(out, "\n")
	if out == "" || len(lines) != 1 {
		t.Fatalf("want one log line, got %q", logs.String())
	}
	var got struct {
		Level, Listener, Pattern, Error string
		Offset                          int
	}
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatal(err)
	}
	want := struct {
		Level, Listener, Pattern, Error string
		Offset                          int
	}{"warn", "failA", "A1 B2 C3 D4", "a", 10}
	if got != want {
		t.Fatalf("log line: got %+v, want %+v (%s)", got, want, lines[0])
	}
}

// A Seek that runs to EOF logs the pattern it was looking for.
func TestSeek_MissLogsPattern(t *testing.T) {
	logs := captureLog(t)
	r := &Reader{b: []byte{1, 2, 3, 4, 5}}
	if err := r.Seek([]byte{0x09, 0xAB}); err != io.EOF {
		t.Fatalf("want io.EOF, got %v", err)
	}
	var got struct {
		Message, Pattern string
		Bytes            int
	}
	if err := json.Unmarshal(logs.Bytes(), &got); err != nil {
		t.Fatalf("%v: %q", err, logs.String())
	}
	if got.Message != "large seek" || got.Pattern != "09 AB" || got.Bytes != 5 {
		t.Fatalf("log line: got %+v (%s)", got, logs.String())
	}
}

// A player record whose profile ID marker is missing fails readPlayer for the last player read
// (its Seek runs to EOF). On these two fixtures, every other player keeps the score, kills and
// assists of a clean read. An event whose subject is the lost player (a kill on them, their
// plant) can still fall back to another attribution; this test does not cover that.
func TestRead_PlayerRecordFailureKeepsOtherPlayersStats(t *testing.T) {
	marker := []byte{0x8A, 0x50, 0x9B, 0xD0} // readPlayer's profileIDIndicator
	for _, path := range []string{failureFixture, "test/data/replays/valid/Y11S2/custom_solid_snake.rec"} {
		t.Run(path, func(t *testing.T) {
			clean := openFixture(t, path)
			if err := clean.Read(); err != nil {
				t.Fatal(err)
			}
			want := map[string]PlayerRoundStats{}
			for _, s := range clean.PlayerStats() {
				want[s.Username] = s
			}
			r := openFixture(t, path)
			last := bytes.LastIndex(r.b, marker)
			if last < 0 {
				t.Fatal("no profile ID marker in the fixture")
			}
			for i := range marker {
				r.b[last+i] ^= 0xFF
			}
			if err := r.Read(); err != nil {
				t.Fatalf("Read returned %v; want nil", err)
			}
			if len(r.Warnings) != 1 || r.Warnings[0].Listener != "readPlayer" {
				t.Fatalf("want one readPlayer warning, got %+v", r.Warnings)
			}
			got := r.PlayerStats()
			if len(got) != len(want)-1 {
				t.Fatalf("got %d players, want %d", len(got), len(want)-1)
			}
			for _, s := range got {
				w := want[s.Username]
				if s.Score != w.Score || s.Kills != w.Kills || s.Assists != w.Assists {
					t.Errorf("%s: score/kills/assists = %d/%d/%d, want %d/%d/%d", s.Username, s.Score, s.Kills, s.Assists, w.Score, w.Kills, w.Assists)
				}
			}
		})
	}
}
