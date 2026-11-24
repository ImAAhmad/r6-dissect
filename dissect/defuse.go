package dissect

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// Y10S4+ defuser packets.
//
// The defuser timer (22 A9 C8 58 D9 <string>) is a property of the pawn that is
// planting/disabling. It is preceded by a reference to that pawn and its other
// defuser properties:
//
//	23 <pawn id: 4 bytes> 00 00 00 00 E5 8C 06 E9 04 <state: uint32>   (only when the state changes)
//	23 <pawn id: 4 bytes> 00 00 00 00 E9 A3 7F EB 04 <remaining: float32, 1 -> 0>
//	22 A9 C8 58 D9 <timer string>
//
// state is 0 when a plant starts, 1 when a disable starts and 2 when the action ends.
// Pawn ids change every time a player (re)spawns. They are linked to a player slot entity by
//
//	1B <slot id: 4 bytes> 00 00 00 00 27 C0 8D CA <pawn id: 4 bytes> 00 00 00 00
//
// and the slot id is part of the player packet (see peekPlayerSlotID).
var (
	defuserPawnIndicator      = []byte{0x27, 0xC0, 0x8D, 0xCA}
	defuserStateProperty      = []byte{0xE5, 0x8C, 0x06, 0xE9}
	defuserRemainingProperty  = []byte{0xE9, 0xA3, 0x7F, 0xEB}
	playerSlotIDProperty      = []byte{0x63, 0xCC, 0x18, 0x8F}
	defuserTimerPatternLength = 5
)

const (
	defuserStatePlanting  = 0
	defuserStateDisabling = 1
	defuserStateIdle      = 2
	defuserStateUnknown   = -1
)

// defuserRun is a single plant or disable attempt, i.e. one countdown of the defuser timer.
type defuserRun struct {
	active      bool // the countdown is running (not completed or stopped yet)
	plant       bool
	playerIndex int
	announced   bool // a start event was emitted for this run
	// time of the last timer packet of the run
	timeRaw string
	time    float64
	phase   RoundPhase
	elapsed float64
}

func (r *Reader) getTeamByRole(role TeamRole) int {
	for i, team := range r.Header.Teams {
		if team.Role == role {
			return i
		}
	}
	return -1
}

func (r *Reader) getAlivePlayersByTeam(teamIndex int) []string {
	var alive []string
	for _, p := range r.Header.Players {
		if p.TeamIndex == teamIndex {
			died := false
			for _, fb := range r.MatchFeedback {
				if fb.Type == Kill && fb.Target == p.Username {
					died = true
					break
				}
				if fb.Type == Death && fb.Username == p.Username {
					died = true
					break
				}
			}
			if !died && p.Username != "" {
				alive = append(alive, p.Username)
			}
		}
	}
	return alive
}

// peekPlayerSlotID returns the player slot entity id that follows the DissectID in a
// Y10S4+ player packet (23 <slot id> 00 00 00 00 63 CC 18 8F), without moving the reader.
func (r *Reader) peekPlayerSlotID() []byte {
	end := r.offset + 160
	if end > len(r.b) {
		end = len(r.b)
	}
	if r.offset >= end {
		return nil
	}
	k := bytes.Index(r.b[r.offset:end], playerSlotIDProperty)
	if k < 9 {
		return nil
	}
	k += r.offset
	if r.b[k-9] != 0x23 || !isZero(r.b[k-4:k]) {
		return nil
	}
	return append([]byte(nil), r.b[k-8:k-4]...)
}

// readDefuserPawn links a pawn entity to a player slot (Y10S4+).
func readDefuserPawn(r *Reader) error {
	start := r.offset - len(defuserPawnIndicator)
	if start < 9 || r.offset+8 > len(r.b) {
		return nil
	}
	if r.b[start-9] != 0x1B || !isZero(r.b[start-4:start]) || !isZero(r.b[r.offset+4:r.offset+8]) {
		return nil
	}
	var pawn, slot [4]byte
	copy(pawn[:], r.b[r.offset:r.offset+4])
	copy(slot[:], r.b[start-8:start-4])
	if pawn == [4]byte{} {
		return nil
	}
	r.defuserPawnToSlot[pawn] = slot
	return nil
}

// defuserPacketPawn finds the pawn referenced right before the defuser timer that starts at
// patternStart, along with the defuser state if it changed in this packet.
func (r *Reader) defuserPacketPawn(patternStart int) (pawn [4]byte, state int, ok bool) {
	state = defuserStateUnknown
	limit := patternStart - 64
	if limit < 0 {
		limit = 0
	}
	for p := patternStart - 13; p >= limit; p-- {
		if r.b[p] != 0x23 || !isZero(r.b[p+5:p+9]) {
			continue
		}
		prop := r.b[p+9 : p+13]
		if !bytes.Equal(prop, defuserRemainingProperty) && !bytes.Equal(prop, defuserStateProperty) {
			continue
		}
		copy(pawn[:], r.b[p+1:p+5])
		if q := bytes.Index(r.b[p:patternStart], defuserStateProperty); q >= 0 {
			q += p
			if q+9 <= patternStart && r.b[q+4] == 0x04 {
				state = int(binary.LittleEndian.Uint32(r.b[q+5 : q+9]))
			}
		}
		return pawn, state, true
	}
	return pawn, state, false
}

func (r *Reader) playerIndexByDefuserPawn(pawn [4]byte) int {
	slot, ok := r.defuserPawnToSlot[pawn]
	if !ok {
		return -1
	}
	for i, p := range r.Header.Players {
		if bytes.Equal(p.slotID, slot[:]) {
			return i
		}
	}
	return -1
}

func isZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func readDefuserTimer(r *Reader) error {
	patternStart := r.offset - defuserTimerPatternLength
	timer, err := r.String()
	if err != nil {
		return err
	}
	if r.Header.CodeVersion >= Y10S4 {
		return readDefuserTimerY10S4(r, patternStart, timer)
	}
	return readDefuserTimerLegacy(r, timer)
}

// readDefuserTimerLegacy handles versions before Y10S4, where the player id follows the timer.
func readDefuserTimerLegacy(r *Reader, timer string) error {
	prevTimer := r.lastDefuserTimer
	timerValue := -1.0
	if len(timer) > 0 {
		if v, parseErr := strconv.ParseFloat(timer, 64); parseErr == nil {
			timerValue = v
		}
	}
	if err := r.Skip(34); err != nil {
		return err
	}
	id, err := r.Bytes(4)
	if err != nil {
		return err
	}
	playerIndex := r.PlayerIndexByID(id)

	if playerIndex > -1 {
		a := DefuserPlantStart
		recordStartEvent := true
		if r.planted {
			if timerValue >= 0 && prevTimer >= 0 && timerValue > prevTimer {
				a = DefuserDisableStart
				r.defuserDisabling = true
			} else {
				recordStartEvent = false
			}
		} else {
			r.defuserDisabling = false
		}
		if recordStartEvent && r.lastDefuserPlayerIndex != playerIndex {
			u := MatchUpdate{
				Type:          a,
				Username:      r.Header.Players[playerIndex].Username,
				Time:          r.timeRaw,
				TimeInSeconds: r.time,
			}
			r.MatchFeedback = append(r.MatchFeedback, u)
			log.Debug().Interface("match_update", u).Send()
			r.lastDefuserPlayerIndex = playerIndex
		}
	}

	// NOTE: 0.00 can be present even if defuser was not disabled.
	if !strings.HasPrefix(timer, "0.00") {
		r.lastDefuserTimer = timerValue
		return nil
	}
	eventType := DefuserDisableComplete
	if !r.planted {
		eventType = DefuserPlantComplete
		r.planted = true
		r.defuserDisabling = false
	} else if r.defuserDisabling {
		eventType = DefuserDisableComplete
		r.defuserDisabling = false
		r.planted = false
	} else {
		r.lastDefuserTimer = timerValue
		return nil
	}

	username := ""
	if r.lastDefuserPlayerIndex >= 0 && r.lastDefuserPlayerIndex < len(r.Header.Players) {
		username = r.Header.Players[r.lastDefuserPlayerIndex].Username
	}

	u := MatchUpdate{
		Type:          eventType,
		Username:      username,
		Time:          r.timeRaw,
		TimeInSeconds: r.time,
	}
	// stamp before the clock switches, the plant is the last event of the action phase
	if u.Type == DefuserPlantComplete {
		r.stampPlantComplete(&u)
	} else {
		r.stamp(&u)
	}
	r.MatchFeedback = append(r.MatchFeedback, u)
	if u.Type == DefuserPlantComplete {
		r.startPostPlant()
	}
	log.Debug().Interface("match_update", u).Send()

	r.lastDefuserTimer = timerValue
	return nil
}

func readDefuserTimerY10S4(r *Reader, patternStart int, timer string) error {
	if len(timer) == 0 {
		// empty timers are sent when pawns spawn
		return nil
	}
	timerValue, err := strconv.ParseFloat(timer, 64)
	if err != nil {
		return nil
	}
	prevTimer := r.lastDefuserTimer
	r.lastDefuserTimer = timerValue

	pawn, state, found := r.defuserPacketPawn(patternStart)
	playerIndex := -1
	if found {
		playerIndex = r.playerIndexByDefuserPawn(pawn)
	}
	log.Debug().
		Str("timer", timer).
		Hex("pawn", pawn[:]).
		Int("state", state).
		Int("playerIndex", playerIndex).
		Msg("defuser_timer")

	run := &r.defuserRun
	// every attempt restarts the countdown, and the first packet of an attempt carries its state
	newRun := (run.active && timerValue > prevTimer) ||
		(!run.active && (timerValue > prevTimer || state == defuserStatePlanting || state == defuserStateDisabling))
	if newRun {
		if run.active {
			r.stopDefuserRun()
		}
		r.startDefuserRun(playerIndex, state)
	}
	if !run.active {
		// trailing packets of an attempt that already ended
		return nil
	}
	if run.playerIndex < 0 && playerIndex >= 0 {
		run.playerIndex = playerIndex
	}
	run.timeRaw = r.timeRaw
	run.time = r.time
	run.phase = r.clock.phase
	run.elapsed = r.elapsed()

	if strings.HasPrefix(timer, "0.00") {
		r.completeDefuserRun()
	} else if state == defuserStateIdle {
		r.stopDefuserRun()
	}
	return nil
}

func (r *Reader) startDefuserRun(playerIndex int, state int) {
	plant := !r.planted
	switch state {
	case defuserStatePlanting:
		plant = true
	case defuserStateDisabling:
		plant = false
	}
	if playerIndex < 0 {
		playerIndex = r.lastAliveDefuserPlayer(plant)
	}
	r.defuserRun = defuserRun{
		active:      true,
		plant:       plant,
		playerIndex: playerIndex,
	}
	if playerIndex < 0 {
		return
	}
	t := DefuserPlantStart
	if !plant {
		t = DefuserDisableStart
	}
	u := MatchUpdate{
		Type:          t,
		Username:      r.Header.Players[playerIndex].Username,
		Time:          r.timeRaw,
		TimeInSeconds: r.time,
	}
	r.MatchFeedback = append(r.MatchFeedback, u)
	log.Debug().Interface("match_update", u).Send()
	r.defuserRun.announced = true
}

// lastAliveDefuserPlayer is the fallback when the pawn cannot be resolved:
// if a single player of the acting team is alive, they must be the one planting/disabling.
func (r *Reader) lastAliveDefuserPlayer(plant bool) int {
	role := Attack
	if !plant {
		role = Defense
	}
	teamIndex := r.getTeamByRole(role)
	if teamIndex < 0 {
		return -1
	}
	alive := r.getAlivePlayersByTeam(teamIndex)
	if len(alive) != 1 {
		return -1
	}
	return r.PlayerIndexByUsername(alive[0])
}

func (r *Reader) completeDefuserRun() {
	run := &r.defuserRun
	run.active = false
	t := DefuserPlantComplete
	if run.plant {
		r.planted = true
	} else {
		t = DefuserDisableComplete
		r.planted = false
	}
	username := ""
	if run.playerIndex >= 0 && run.playerIndex < len(r.Header.Players) {
		username = r.Header.Players[run.playerIndex].Username
	}
	u := MatchUpdate{
		Type:          t,
		Username:      username,
		Time:          r.timeRaw,
		TimeInSeconds: r.time,
	}
	// stamp before the clock switches, the plant is the last event of the action phase
	if u.Type == DefuserPlantComplete {
		r.stampPlantComplete(&u)
	} else {
		r.stamp(&u)
	}
	r.MatchFeedback = append(r.MatchFeedback, u)
	if u.Type == DefuserPlantComplete {
		r.startPostPlant()
	}
	log.Debug().Interface("match_update", u).Send()
	// if the player is unknown, the scoreboard score handler identifies them via the +100 score bonus
	if username == "" {
		r.pendingDefuserPlantIdx = len(r.MatchFeedback) - 1
		r.pendingDefuserIsPlant = run.plant
	}
}

// stopDefuserRun ends an attempt that did not complete. The event is timed at the
// last timer packet of the attempt.
func (r *Reader) stopDefuserRun() {
	run := &r.defuserRun
	if !run.active {
		return
	}
	run.active = false
	if !run.announced || run.playerIndex < 0 || run.playerIndex >= len(r.Header.Players) {
		return
	}
	t := DefuserPlantStop
	if !run.plant {
		t = DefuserDisableStop
	}
	u := MatchUpdate{
		Type:           t,
		Username:       r.Header.Players[run.playerIndex].Username,
		Time:           run.timeRaw,
		TimeInSeconds:  run.time,
		Phase:          run.phase,
		ElapsedSeconds: run.elapsed,
	}
	// the stop can be noticed after later events were recorded
	r.insertByElapsed(u)
	log.Debug().Interface("match_update", u).Send()
}
