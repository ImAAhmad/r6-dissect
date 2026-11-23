package dissect

import (
	"bytes"
	"strings"

	"github.com/rs/zerolog/log"
)

var currentSitePattern = []byte{0xFC, 0xC6, 0xA8, 0x60, 0x01}

// Room location object (Y9S1+). Each player entity owns a child object holding the
// localized name of the room the player is currently in. It is created directly after the
// player's profile id property, in the same player state block:
//
//	1A 9D EB E8 A7 <room obj id: 8> 41 36 0C E6 23 <room obj id: 8>
//	AF 98 99 CA 08 <8 bytes, always FF..FF>
//	26 AF 98 99 CA 00 00 00 00 <len> <room name>
//
// Later updates to the room reuse the "23 <room obj id> AF 98 99 CA ..." part, but the
// room object id is never referenced elsewhere, so the only reliable link back to the
// player is this creation record.
var (
	roomObjectIndicator = []byte{0x1A, 0x9D, 0xEB, 0xE8, 0xA7}
	roomObjectClass     = []byte{0x41, 0x36, 0x0C, 0xE6, 0x23}
	roomNameHeader      = []byte{0xAF, 0x98, 0x99, 0xCA, 0x08}
	roomNameElement     = []byte{0x26, 0xAF, 0x98, 0x99, 0xCA, 0x00, 0x00, 0x00, 0x00}
)

// roomObjectSearchWindow bounds how far after the profile id the room object may start.
// Observed gap is 17 bytes (two small properties) in Y9S1 through Y11S2.
const roomObjectSearchWindow = 48

// readStartingRoom returns the initial room of the player whose profile id string ended at
// offset start, or "" if no room object follows. It does not move the reader offset.
func (r *Reader) readStartingRoom(start int) string {
	if start < 0 || start >= len(r.b) {
		return ""
	}
	end := start + roomObjectSearchWindow
	if end > len(r.b) {
		end = len(r.b)
	}
	i := bytes.Index(r.b[start:end], roomObjectIndicator)
	if i < 0 {
		return ""
	}
	o := start + i + len(roomObjectIndicator)
	need := 8 + len(roomObjectClass) + 8 + len(roomNameHeader) + 8 + len(roomNameElement) + 1
	if o+need > len(r.b) {
		return ""
	}
	id := r.b[o : o+8]
	o += 8
	if !bytes.Equal(r.b[o:o+len(roomObjectClass)], roomObjectClass) {
		return ""
	}
	o += len(roomObjectClass)
	if !bytes.Equal(r.b[o:o+8], id) {
		return ""
	}
	o += 8
	if !bytes.Equal(r.b[o:o+len(roomNameHeader)], roomNameHeader) {
		return ""
	}
	o += len(roomNameHeader) + 8
	if !bytes.Equal(r.b[o:o+len(roomNameElement)], roomNameElement) {
		return ""
	}
	o += len(roomNameElement)
	size := int(r.b[o])
	o++
	if o+size > len(r.b) {
		return ""
	}
	return string(r.b[o : o+size])
}

func readSpawn(r *Reader) error {
	location, err := r.String()
	if err != nil {
		return err
	}
	if err = r.Skip(150); err != nil {
		return err
	}
	pattern, err := r.Bytes(5)
	if err != nil {
		return err
	}
	if !strings.Contains(location, "<br/>") {
		return nil
	}
	log.Debug().
		Str("site", location).
		Send()
	if r.Header.Site == "" || bytes.Equal(pattern, currentSitePattern) {
		formatted := strings.Replace(location, "<br/>", ", ", 1)
		log.Debug().Str("site", formatted).Msg("defense site")
		for i, p := range r.Header.Players {
			defenseTeam := r.Header.Teams[p.TeamIndex].Role == Defense
			defenseRole := p.Operator != Recruit && p.Operator != 0 && p.Operator.Role() == Defense
			if defenseTeam || defenseRole {
				r.Header.Players[i].Spawn = formatted
			}
		}
		r.Header.Site = formatted
		return nil
	}
	return nil
}
