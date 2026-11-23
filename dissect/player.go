package dissect

import (
	"bytes"
	"strings"

	"github.com/rs/zerolog/log"
)

func readPlayer(r *Reader) error {
	markers := r.playerMarkers()
	idIndicator := markers.id
	spawnIndicator := []byte{0xAF, 0x98, 0x99, 0xCA}
	profileIDIndicator := []byte{0x8A, 0x50, 0x9B, 0xD0}
	//unknownIndicator := []byte{0x22, 0xEE, 0xD4, 0x45, 0xC8, 0x08} // maybe player appearance?
	playerObject := r.playerObjectBefore(r.offset - 5)
	r.playersRead++
	defer func() {
		if r.playersRead == 10 {
			r.deriveTeamRoles()
		}
	}()
	username, err := r.String()
	if err != nil {
		return err
	}
	if r.Header.CodeVersion >= Y7S4 {
		if err := r.Seek([]byte{0x40, 0xF2, 0x15, 0x04}); err != nil {
			return err
		}
		if err = r.Skip(8); err != nil {
			return err
		}
		swap, err := r.Bytes(1)
		if err != nil {
			return err
		}
		// Sometimes, 0x40, 0xF2, 0x15, 0x04 is sent twice.
		// Does not seem to be linked to role swap.
		if swap[0] == 0x9D {
			return nil
		}
	} else {
		if err := r.Seek([]byte{0x22, 0xA9, 0x26, 0x0B, 0xE4}); err != nil {
			return err
		}
	}
	op, err := r.Uint64() // Op before atk role swaps
	if err != nil {
		return err
	}
	if op == 0 { // Empty player slot
		log.Debug().Msg("empty player slot?")
		return nil
	}
	validPlayer, err := r.Bytes(1)
	if err != nil {
		return err
	}
	if validPlayer[0] != 0x22 {
		log.Warn().Uint64("op", op).Msg("strange invalid player located")
		return nil
	}
	if err := r.Seek(idIndicator); err != nil {
		return err
	}
	id, err := r.Bytes(4)
	if err != nil {
		return err
	}
	var slotID []byte
	if r.Header.CodeVersion >= Y10S4 {
		slotID = r.peekPlayerSlotID()
	}
	if err := r.Seek(spawnIndicator); err != nil {
		return err
	}
	spawn, err := r.String()
	if err != nil {
		return err
	}
	if spawn == "" {
		if err = r.Skip(10); err != nil {
			return err
		}
		valid, err := r.Bytes(1)
		if err != nil {
			return err
		}
		if !bytes.Equal(valid, []byte{0x1B}) {
			return nil
		}
	}
	teamIndex := 0
	if r.playersRead > 5 {
		teamIndex = 1
	}
	// ui id (y9s3+?)
	// there seems to be more to this, but its a quick fix for atk op swaps for now
	var uiID uint64
	if r.Header.CodeVersion >= Y9S3 {
		if err = r.Seek(markers.uiID); err != nil {
			return err
		}
		if err = r.Skip(13); err != nil {
			return err
		}
		if uiID, err = r.Uint64(); err != nil {
			return err
		}
	}
	// Older versions of siege did not include profile ids
	profileID := ""
	startingRoom := ""
	var unknownId uint64
	if len(r.Header.RecordingProfileID) > 0 {
		if err = r.Seek(profileIDIndicator); err != nil {
			return err
		}
		profileID, err = r.String()
		if err != nil {
			return err
		}
		startingRoom = r.readStartingRoom(r.offset)
		if err = r.Skip(5); err != nil { // 22eed445c8
			return err
		}
		unknownId, err = r.Uint64()
		if err != nil {
			return err
		}
	} else {
		log.Debug().Str("warn", "profileID not found, skipping").Send()
	}
	p := Player{
		ID:        unknownId,
		ProfileID: profileID,
		Username:  username,
		TeamIndex: teamIndex,
		Operator:  Operator(op),
		Spawn:     spawn,
		DissectID: bytes.Clone(id), // a copy: a slice of r.b would pin the whole round buffer after Read
		uiID:      uiID,
		slotID:    slotID,

		StartingRoom: startingRoom,
	}
	if p.Operator != Recruit && p.Operator.Role() == Defense {
		p.Spawn = r.Header.Site // We cannot detect the spawn here on defense
	}
	log.Debug().Str("username", username).
		Int("teamIndex", teamIndex).
		Interface("op", p.Operator).
		Str("profileID", profileID).
		Hex("DissectID", id).
		Uint64("ID", p.ID).
		Uint64("uiID", p.uiID).
		Hex("slotID", p.slotID).
		Str("spawn", spawn).
		Str("startingRoom", startingRoom).Send()
	found := false
	foundIndex := -1
	for i, existing := range r.Header.Players {
		if existing.Username == p.Username ||
			(r.Header.CodeVersion < Y8S2 && existing.ID == p.ID && p.ID != 0) ||
			(r.Header.CodeVersion >= Y8S2 && bytes.Equal(existing.DissectID, p.DissectID)) ||
			(r.Header.CodeVersion <= Y7S2 && strings.HasPrefix(p.Username, existing.Username)) {
			r.Header.Players[i].ProfileID = p.ProfileID
			r.Header.Players[i].Username = p.Username
			r.Header.Players[i].Operator = p.Operator
			r.Header.Players[i].Spawn = p.Spawn
			r.Header.Players[i].DissectID = p.DissectID
			r.Header.Players[i].uiID = p.uiID
			if p.slotID != nil {
				r.Header.Players[i].slotID = p.slotID
			}
			if r.Header.Players[i].StartingRoom == "" {
				r.Header.Players[i].StartingRoom = p.StartingRoom
			}
			found = true
			foundIndex = i
			break
		}
	}
	if !found && len(username) > 0 {
		r.Header.Players = append(r.Header.Players, p)
		foundIndex = len(r.Header.Players) - 1
	}
	if foundIndex >= 0 {
		r.readPlayerOrder = append(r.readPlayerOrder, foundIndex)
		r.trackPlayerObject(playerObject, r.Header.Players[foundIndex])
	}
	return err
}

func readAtkOpSwap(r *Reader) error {
	patternStart := r.offset - 5
	op, err := r.Uint64()
	if err != nil {
		return err
	}
	o := Operator(op)
	// before Y9S3 caster view overhaul
	if r.Header.CodeVersion < Y9S3 {
		if err = r.Skip(5); err != nil {
			return err
		}
		id, err := r.Bytes(4)
		if err != nil {
			return err
		}
		i := r.PlayerIndexByID(id)
		log.Debug().Hex("id", id).Interface("op", op).Msg("atk_op_swap")
		if i > -1 {
			r.Header.Players[i].Operator = o
			u := MatchUpdate{
				Type:          OperatorSwap,
				Username:      r.Header.Players[i].Username,
				Time:          r.timeRaw,
				TimeInSeconds: r.time,
				Operator:      o,
			}
			r.MatchFeedback = append(r.MatchFeedback, u)
			log.Debug().Interface("match_update", u).Send()
		}
		return nil
	}
	// after Y9S3 caster view overhaul
	// Y11S2: the swap is a property of the player object,
	// 23 <player object> F93911F2 08 <u64> 22 A9260BE4 08 <op>
	objectIndex := r.playerIndexByObject(r.opSwapPlayerObject(patternStart))
	found := false
	if err = r.Skip(402); err == nil {
		// id shows up in player data and in op swaps afaik
		var id uint64
		if id, err = r.Uint64(); err == nil {
			for i, p := range r.Header.Players {
				if p.uiID == id {
					r.swapOperator(i, o)
					found = true
					break
				}
			}
		}
	}
	if !found && objectIndex > -1 {
		r.swapOperator(objectIndex, o)
	}
	return err
}

func (r *Reader) swapOperator(i int, o Operator) {
	r.Header.Players[i].Operator = o
	u := MatchUpdate{
		Type:          OperatorSwap,
		Username:      r.Header.Players[i].Username,
		Time:          r.timeRaw,
		TimeInSeconds: r.time,
		Operator:      o,
	}
	r.MatchFeedback = append(r.MatchFeedback, u)
	log.Debug().Interface("match_update", u).Send()
}

// opSwapPlayerObject returns the player object an operator property
// starting at offset belongs to, or 0.
func (r *Reader) opSwapPlayerObject(offset int) uint64 {
	const prefix = 1 + handleSize + 4 + 1 + 8 // 23 <player object> F93911F2 08 <u64>
	if offset < prefix || offset > len(r.b) {
		return 0
	}
	b := r.b[offset-prefix : offset]
	if b[0] != 0x23 || !bytes.Equal(b[1+handleSize:1+handleSize+5], []byte{0xF9, 0x39, 0x11, 0xF2, 0x08}) {
		return 0
	}
	return readHandle(b[1 : 1+handleSize])
}
