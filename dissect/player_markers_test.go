package dissect

import (
	"bytes"
	"testing"
)

// The bytes readPlayer seeks, spelled out so the tests do not depend on the
// variables under test.
var (
	testIDClassic = []byte{0x33, 0xD8, 0x3D, 0x4F, 0x23}
	testUIClassic = []byte{0x38, 0xDF, 0xEE, 0x88}
	testIDAlpha03 = []byte{0x8C, 0x61, 0x1A, 0x75, 0x23}
	testUIAlpha03 = []byte{0x70, 0xFA, 0xAF, 0x28}
	testIDY7S2    = []byte{0xE6, 0xF9, 0x7D, 0x86}
)

var testMarkerSlack = bytes.Repeat([]byte{0x11}, 64)

func testRound(parts ...[]byte) []byte {
	b := append([]byte{}, testMarkerSlack...)
	for _, p := range parts {
		b = append(b, p...)
		b = append(b, testMarkerSlack...)
	}
	return b
}

func TestDetectPlayerMarkers(t *testing.T) {
	cases := []struct {
		name        string
		codeVersion int
		round       []byte
		wantID      []byte
		wantUI      []byte
		wantOK      bool
	}{
		{"Y11S2_Alpha03 9718747 uses the classic tags", 9718747, testRound(testIDClassic, testUIClassic), testIDClassic, testUIClassic, true},
		{"Y11S2_Alpha03 9734089 uses its own tags", 9734089, testRound(testIDAlpha03, testUIAlpha03), testIDAlpha03, testUIAlpha03, true},
		{"Y11S2_Alpha04 9785623 reverted to the classic tags", 9785623, testRound(testIDClassic, testUIClassic), testIDClassic, testUIClassic, true},
		{"Y11S3 9950954 uses the classic tags", 9950954, testRound(testIDClassic, testUIClassic), testIDClassic, testUIClassic, true},
		{"the bytes decide, not the build number", 9950954, testRound(testIDAlpha03, testUIAlpha03), testIDAlpha03, testUIAlpha03, true},
		{"the uiID tag is found on its own when only it moved", 9950954, testRound(testIDClassic, testUIAlpha03), testIDClassic, testUIAlpha03, true},
		{"the uiID tag of the set the id tag picked wins", 9734089, testRound(testIDAlpha03, testUIClassic, testUIAlpha03), testIDAlpha03, testUIAlpha03, true},
		{"before Y9S3 there is no uiID to find", Y8S1, testRound(testIDClassic), testIDClassic, testUIClassic, true},
		{"Y7S2 and earlier keep their own id tag", Y7S2, testRound(testIDClassic), testIDY7S2, testUIClassic, true},
		{"both id tags present: the classic set wins", 9950954, testRound(testIDAlpha03, testUIAlpha03, testIDClassic, testUIClassic), testIDClassic, testUIClassic, true},
		{"the id tag is found on its own when only it moved", 9950954, testRound(testIDAlpha03, testUIClassic), testIDAlpha03, testUIClassic, true},
		{"a round with no known tag falls back to the classic tags", 9950954, testRound(), testIDClassic, testUIClassic, false},
		{"Y7S2 needs no uiID even in an empty round", Y7S2, testRound(), testIDY7S2, testUIClassic, true},
		{"an unknown id tag falls back to the classic tags", 9950954, testRound(testUIClassic), testIDClassic, testUIClassic, false},
		{"an unknown uiID tag falls back to the classic tags", 9950954, testRound(testIDClassic), testIDClassic, testUIClassic, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := detectPlayerMarkers(c.round, c.codeVersion)
			if !bytes.Equal(got.id, c.wantID) {
				t.Errorf("id = % X, want % X", got.id, c.wantID)
			}
			if !bytes.Equal(got.uiID, c.wantUI) {
				t.Errorf("uiID = % X, want % X", got.uiID, c.wantUI)
			}
			if ok != c.wantOK {
				t.Errorf("ok = %v, want %v", ok, c.wantOK)
			}
		})
	}
}

// The tags are detected on the first call and kept for the round: later
// players do not scan the round again.
func TestReaderPlayerMarkers_Cached(t *testing.T) {
	r := &Reader{b: testRound(testIDClassic, testUIClassic)}
	r.Header.CodeVersion = 9950954
	if r.markers != nil {
		t.Fatal("markers set before the first call")
	}
	first := r.playerMarkers()
	if !bytes.Equal(first.id, testIDClassic) || !bytes.Equal(first.uiID, testUIClassic) {
		t.Fatalf("first = % X / % X, want the classic tags", first.id, first.uiID)
	}
	r.b = testRound(testIDAlpha03, testUIAlpha03)
	second := r.playerMarkers()
	if !bytes.Equal(second.id, testIDClassic) || !bytes.Equal(second.uiID, testUIClassic) {
		t.Errorf("second call re-detected: % X / % X", second.id, second.uiID)
	}
}
