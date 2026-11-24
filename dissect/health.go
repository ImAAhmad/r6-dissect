package dissect

import (
	"bytes"
	"encoding/binary"

	"github.com/rs/zerolog/log"
)

// Y10S4+ player health component.
//
// Every player slot owns a health component object, linked during the prep phase by
//
//	1B <slot id: 4> 00 00 00 00 41 54 DC C4 <health id: 4> 00 00 00 00 0C 98 C6 3F ...
//
// Its properties are written either as the first property of a reference record
// (23 <health id> 00 00 00 00 <hash> <len> <value>) or as a continuation (22 <hash> <len> <value>)
// directly after one. Observed properties (Y11S2 and Y11S3):
//
//	25 26 76 C9  uint32  current health (100/110/125 at spawn, 135/150 with Rook armor, 20 after a
//	                     revive, 0 when downed or dead); read into healthSamples, see damageTaken
//	E7 88 F6 A5  uint32  health state (see below)
//	72 5E 99 F9  float32 bleed-out progress while downed, 0 -> 1 (~0.028/s)
//	91 5F DF 38  uint8   set while downed (toggles while being revived/interrogated, meaning unconfirmed)
//
// Health state values: 0 healthy, 2 wounded (health <= 20, e.g. after a revive), 3 downed (DBNO),
// 4 dead. A down is a transition into 3; it ends with 4 (death, sometimes via a transient 2 right
// before the kill feed record), a revive (usually 2 with health reset to 20), or the end of the round.
var (
	healthLinkIndicator  = []byte{0x41, 0x54, 0xDC, 0xC4}
	healthStateIndicator = []byte{0xE7, 0x88, 0xF6, 0xA5, 0x04}
	healthValueIndicator = []byte{0x25, 0x26, 0x76, 0xC9, 0x04}
)

const (
	healthStateHealthy = 0
	healthStateWounded = 2
	healthStateDowned  = 3
	healthStateDead    = 4
	// continuation records follow their reference record closely (max 59 bytes observed)
	healthOwnerSearchWindow = 128
	// how soon after leaving the downed state a death must follow for the down to count as
	// finished rather than revived (the state briefly passes through 2 before 4)
	healthDeathWindow = 1.5
	// the most time between a revive and the reviver's +50 score update
	healthReviveWindow = 3.0
)

type healthTransition struct {
	player  int
	state   uint32
	elapsed float64
	timeRaw string
	time    float64
	phase   RoundPhase
}

// scoreRevive is a +50 score update, the score bonus for a revive.
type scoreRevive struct {
	player  int
	elapsed float64
}

// healthSample is one current-health value of a health component. It is keyed by the component
// and resolved to a player after the round: the spawn snapshot is written before the player's
// slot is known.
type healthSample struct {
	health  [4]byte
	hp      uint32
	started bool // the round clock was running
	phase   RoundPhase
	elapsed float64
}

// readHealthLink links a health component to a player slot.
func readHealthLink(r *Reader) error {
	start := r.offset - len(healthLinkIndicator)
	if start < 9 || r.offset+8 > len(r.b) {
		return nil
	}
	if r.b[start-9] != 0x1B || !isZero(r.b[start-4:start]) || !isZero(r.b[r.offset+4:r.offset+8]) {
		return nil
	}
	var health, slot [4]byte
	copy(health[:], r.b[r.offset:r.offset+4])
	copy(slot[:], r.b[start-8:start-4])
	if health == [4]byte{} {
		return nil
	}
	r.healthToSlot[health] = slot
	return nil
}

// healthRecordOwner returns the health component that a property record starting at start belongs to.
func (r *Reader) healthRecordOwner(start int) (owner [4]byte, ok bool) {
	if start >= 9 && r.b[start-9] == 0x23 && isZero(r.b[start-4:start]) {
		copy(owner[:], r.b[start-8:start-4])
		_, ok = r.healthToSlot[owner]
		return owner, ok
	}
	if start < 1 || r.b[start-1] != 0x22 {
		return owner, false
	}
	limit := start - healthOwnerSearchWindow
	if limit < 0 {
		limit = 0
	}
	for p := start - 10; p >= limit; p-- {
		if r.b[p] != 0x23 || !isZero(r.b[p+5:p+9]) {
			continue
		}
		copy(owner[:], r.b[p+1:p+5])
		if _, known := r.healthToSlot[owner]; known {
			return owner, true
		}
	}
	return owner, false
}

func (r *Reader) playerIndexByHealth(health [4]byte) int {
	slot, ok := r.healthToSlot[health]
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

// readHealthState records health state changes and adds a DBNO when a player is downed.
// Who downed them and revives are resolved after the round, in resolveHealthEvents.
func readHealthState(r *Reader) error {
	start := r.offset - len(healthStateIndicator)
	if r.offset+4 > len(r.b) {
		return nil
	}
	state := binary.LittleEndian.Uint32(r.b[r.offset : r.offset+4])
	owner, ok := r.healthRecordOwner(start)
	if !ok {
		return nil
	}
	idx := r.playerIndexByHealth(owner)
	if idx < 0 || !r.clock.started {
		return nil
	}
	prev, seen := r.healthState[idx]
	r.healthState[idx] = state
	if seen && prev == state {
		return nil
	}
	r.healthTransitions = append(r.healthTransitions, healthTransition{
		player:  idx,
		state:   state,
		elapsed: r.elapsed(),
		timeRaw: r.timeRaw,
		time:    r.time,
		phase:   r.clock.phase,
	})
	log.Debug().
		Str("username", r.Header.Players[idx].Username).
		Uint32("state", state).
		Uint32("prev", prev).
		Msg("health_state")
	if state != healthStateDowned {
		return nil
	}
	u := MatchUpdate{
		Type:             DBNO,
		Target:           r.Header.Players[idx].Username,
		Time:             r.timeRaw,
		TimeInSeconds:    r.time,
		healthTransition: len(r.healthTransitions),
	}
	r.stamp(&u)
	r.MatchFeedback = append(r.MatchFeedback, u)
	log.Debug().Interface("match_update", u).Msg("dbno from health state")
	return nil
}

// downRevived reports whether the down at healthTransitions[down] was followed by a revive, i.e.
// the player left the downed state and did not die right after, and returns the transition out
// of the downed state. A death only finishes the down if it comes before the player's next down:
// feed is the index of the DBNO being resolved, and a Kill after the target's next DBNO in the
// feed belongs to that one.
func (r *Reader) downRevived(down, feed int) (healthTransition, bool) {
	var none healthTransition
	player := r.healthTransitions[down].player
	if player < 0 || player >= len(r.Header.Players) {
		return none, false
	}
	var next []healthTransition
	for _, t := range r.healthTransitions[down+1:] {
		if t.player == player {
			next = append(next, t)
			if len(next) == 2 {
				break
			}
		}
	}
	if len(next) == 0 || next[0].state == healthStateDead {
		return none, false
	}
	if len(next) == 2 && next[1].state == healthStateDead && next[1].elapsed-next[0].elapsed <= healthDeathWindow {
		return none, false
	}
	if len(next) == 2 && next[1].state == healthStateDowned {
		return next[0], true
	}
	target := r.Header.Players[player].Username
	for _, u := range r.MatchFeedback[feed+1:] {
		if u.Type == DBNO && u.Target == target {
			break
		}
		if u.Type == Kill && u.Target == target && u.ElapsedSeconds >= next[0].elapsed && u.ElapsedSeconds-next[0].elapsed <= healthDeathWindow {
			return none, false
		}
	}
	return next[0], true
}

// takeScoreReviver finds the teammate whose +50 score update is closest to a revive of target
// and consumes it, so it is not used for another revive.
func (r *Reader) takeScoreReviver(target int, at float64) string {
	best, bestDiff := -1, healthReviveWindow
	for i, c := range r.scoreRevives {
		if c.player < 0 || c.player >= len(r.Header.Players) || c.player == target ||
			r.Header.Players[c.player].TeamIndex != r.Header.Players[target].TeamIndex {
			continue
		}
		diff := c.elapsed - at
		if diff < 0 {
			diff = -diff
		}
		if diff <= bestDiff {
			best, bestDiff = i, diff
		}
	}
	if best < 0 {
		return ""
	}
	username := r.Header.Players[r.scoreRevives[best].player].Username
	r.scoreRevives = append(r.scoreRevives[:best], r.scoreRevives[best+1:]...)
	return username
}

// resolveHealthEvents completes the downs read from the health state once the round is read.
// A down that ends in the player's death is credited to the player the kill feed credits with
// the kill. A revived down has no known downer (the replay does not say who dealt the damage);
// a Revive event is added for it, credited to the teammate with a matching +50 score update.
func (r *Reader) resolveHealthEvents() {
	var revives []MatchUpdate
	for i := range r.MatchFeedback {
		d := &r.MatchFeedback[i]
		if d.Type != DBNO || d.healthTransition <= 0 || d.healthTransition > len(r.healthTransitions) {
			continue
		}
		if t, revived := r.downRevived(d.healthTransition-1, i); revived {
			revives = append(revives, MatchUpdate{
				Type:           Revive,
				Username:       r.takeScoreReviver(t.player, t.elapsed),
				Target:         r.Header.Players[t.player].Username,
				Time:           t.timeRaw,
				TimeInSeconds:  t.time,
				Phase:          t.phase,
				ElapsedSeconds: t.elapsed,

				healthTransition: d.healthTransition,
			})
			continue
		}
		for j := i + 1; j < len(r.MatchFeedback); j++ {
			u := r.MatchFeedback[j]
			if (u.Type == DBNO || u.Type == Revive) && u.Target == d.Target {
				break
			}
			if u.Type == Kill && u.Target == d.Target {
				d.Username = u.Username
				d.TeamKill = u.TeamKill
				break
			}
			if u.Type == Death && u.Username == d.Target {
				break
			}
		}
	}
	for _, u := range revives {
		r.insertByElapsed(u)
	}
}

// readHealthValue records a current-health value. Unlike readHealthState it also records values
// read before the round clock starts: they hold the defenders' spawn health.
func readHealthValue(r *Reader) error {
	start := r.offset - len(healthValueIndicator)
	if r.offset+4 > len(r.b) {
		return nil
	}
	owner, ok := r.healthRecordOwner(start)
	if !ok {
		return nil
	}
	s := healthSample{
		health:  owner,
		hp:      binary.LittleEndian.Uint32(r.b[r.offset : r.offset+4]),
		started: r.clock.started,
	}
	if s.started {
		s.phase = r.clock.phase
		s.elapsed = r.elapsed()
	}
	r.healthSamples = append(r.healthSamples, s)
	return nil
}

// damageTakenFromSeries returns the HP a player lost, from the player's health values in read
// order. Attackers start from their last pre-action value (the round file opens with the value
// of the previous round, followed by the prep reset); defenders start from their first. Increases
// (revive, Rook armor, boost) are not damage, and neither is boost health decaying 1 HP/s above
// the starting value. A player who died and whose last value is above 0 lost the rest as well.
func damageTakenFromSeries(series []healthSample, attacker, died bool) int {
	if len(series) == 0 {
		return 0
	}
	base := 0
	if attacker {
		for i, s := range series {
			if !s.started || s.phase == PrepPhase {
				base = i
			}
		}
	}
	dmg := 0
	for i := base + 1; i < len(series); i++ {
		a, b := series[i-1].hp, series[i].hp
		if b >= a {
			continue
		}
		if a-b == 1 && a > series[base].hp {
			continue
		}
		dmg += int(a - b)
	}
	if last := series[len(series)-1].hp; died && last > 0 {
		dmg += int(last)
	}
	return dmg
}

// damageTaken returns the HP lost per player index, for players with at least one health value.
func (r *Reader) damageTaken() map[int]int {
	series := make(map[int][]healthSample)
	for _, s := range r.healthSamples {
		if i := r.playerIndexByHealth(s.health); i >= 0 {
			series[i] = append(series[i], s)
		}
	}
	died := make(map[string]bool)
	for _, u := range r.MatchFeedback {
		switch u.Type {
		case Kill:
			died[u.Target] = true
		case Death:
			died[u.Username] = true
		}
	}
	out := make(map[int]int, len(series))
	for i, s := range series {
		p := r.Header.Players[i]
		attacker := p.TeamIndex >= 0 && p.TeamIndex < len(r.Header.Teams) && r.Header.Teams[p.TeamIndex].Role == Attack
		out[i] = damageTakenFromSeries(s, attacker, died[p.Username])
	}
	return out
}
