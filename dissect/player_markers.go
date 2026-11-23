package dissect

import (
	"bytes"

	"github.com/rs/zerolog/log"
)

// playerMarkers are the property tags readPlayer seeks inside a player record.
type playerMarkers struct {
	id   []byte // followed by the 4-byte DissectID
	uiID []byte // Y9S3+: the uiID follows 13 bytes after it
}

var (
	// playerMarkersClassic is used by every known build after Y7S2 except 9734089.
	playerMarkersClassic = playerMarkers{
		id:   []byte{0x33, 0xD8, 0x3D, 0x4F, 0x23},
		uiID: []byte{0x38, 0xDF, 0xEE, 0x88},
	}
	// playerMarkersY11S2Alpha03 is used by build 9734089, a Y11S2_Alpha03
	// mid-season patch (June 2026). Builds 9718747 (before it) and 9751808,
	// 9785623 and 9838928 (Y11S2_Alpha04, after it) use the classic tags.
	playerMarkersY11S2Alpha03 = playerMarkers{
		id:   []byte{0x8C, 0x61, 0x1A, 0x75, 0x23},
		uiID: []byte{0x70, 0xFA, 0xAF, 0x28},
	}
	// playerIDMarkerY7S2 is the id tag of Y7S2 and earlier.
	playerIDMarkerY7S2 = []byte{0xE6, 0xF9, 0x7D, 0x86}

	// knownPlayerMarkers is the detection order. The classic set comes first.
	knownPlayerMarkers = []playerMarkers{playerMarkersClassic, playerMarkersY11S2Alpha03}
)

// detectPlayerMarkers returns the player tags that the decompressed round b
// contains. The build number does not tell which set a build uses (9734089
// changed both tags and later Y11S2 builds changed them back), so the tags are
// looked up in the round itself. The id tag (5 bytes) picks the set. The uiID
// tag of that set is tried first, then the other known uiID tags, so a build
// that moves only one tag is still read and a stray 4-byte match of another
// set's uiID tag does not win. ok is false when a tag readPlayer needs is not
// in b; the classic tag is returned for it, so readPlayer fails the way it did
// before detection.
func detectPlayerMarkers(b []byte, codeVersion int) (m playerMarkers, ok bool) {
	if codeVersion <= Y7S2 {
		return playerMarkers{id: playerIDMarkerY7S2, uiID: playerMarkersClassic.uiID}, true
	}
	m, ok = playerMarkersClassic, true
	order := knownPlayerMarkers
	found := false
	for i, s := range knownPlayerMarkers {
		if bytes.Contains(b, s.id) {
			m.id, found = s.id, true
			order = append([]playerMarkers{s}, knownPlayerMarkers[:i]...)
			order = append(order, knownPlayerMarkers[i+1:]...)
			break
		}
	}
	if !found {
		ok = false
	}
	if codeVersion < Y9S3 {
		return m, ok
	}
	for _, s := range order {
		if bytes.Contains(b, s.uiID) {
			return playerMarkers{id: m.id, uiID: s.uiID}, ok
		}
	}
	return m, false
}

// playerMarkers returns the player tags of this round. They are detected on
// the first call and kept for the rest of the round.
func (r *Reader) playerMarkers() playerMarkers {
	if r.markers == nil {
		m, ok := detectPlayerMarkers(r.b, r.Header.CodeVersion)
		if !ok {
			log.Warn().
				Str("gameVersion", r.Header.GameVersion).
				Int("codeVersion", r.Header.CodeVersion).
				Msg("no known player id or uiID tag in this round")
		}
		r.markers = &m
	}
	return *r.markers
}
