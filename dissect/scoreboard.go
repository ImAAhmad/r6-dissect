package dissect

import (
	"bytes"
	"encoding/hex"
	"sort"

	"github.com/rs/zerolog/log"
)

// Y10S4+: every player object references its scoreboard entity,
//
//	1B <player obj> EB219B38 <scoreboard entity> 18A591A1
//
// and the first 4 bytes of the entity handle are the sbID its scoreboard fields are written
// under (23 <sbID> 00000000 1CD2B19D ...). The player object is the one the loadout code
// tracks (playerObjectField), so the owner resolves through playerIndexByObject. A player who
// left the match keeps an entity but has no player packet. Its reference can be the 1A form,
// which has no owner handle, or name a player object no player packet resolves; either way the
// entity stays unmapped instead of shifting the players after it.
var (
	scoreboardEntityField = []byte{0xEB, 0x21, 0x9B, 0x38}
	scoreboardEntityClass = []byte{0x18, 0xA5, 0x91, 0xA1}
)

// readScoreboardEntity reads 1B <player obj> EB219B38 <entity> 18A591A1 and records the
// entity's owner. The owner is resolved later, in ensureScoreboardMapping, because the
// reference comes before the owner's player packet.
func readScoreboardEntity(r *Reader) error {
	start := r.offset - len(scoreboardEntityField)
	if start < handleSize+1 || start+slotTokenSize > len(r.b) || r.b[start-handleSize-1] != 0x1B {
		return nil
	}
	if !bytes.Equal(r.b[start+4+handleSize:start+slotTokenSize], scoreboardEntityClass) || !isZero(r.b[start+8:start+12]) {
		return nil
	}
	owner := readHandle(r.b[start-handleSize : start])
	var entity [4]byte
	copy(entity[:], r.b[start+4:start+8])
	if owner == 0 || entity == [4]byte{} {
		return nil
	}
	r.scoreboardEntityOwner[entity] = owner
	return nil
}

type Scoreboard struct {
	Players []ScoreboardPlayer
}

type ScoreboardPlayer struct {
	ID               []byte
	Score            uint32
	Assists          uint32
	AssistsFromRound uint32
}

// scoreboardRow returns the scoreboard row of the player at idx in Header.Players. It adds the
// missing rows, so a player first read after deriveTeamRoles (a late joiner) gets one too and
// Scoreboard.Players stays parallel to Header.Players. idx must be a valid player index.
func (r *Reader) scoreboardRow(idx int) *ScoreboardPlayer {
	for len(r.Scoreboard.Players) <= idx {
		p := r.Header.Players[len(r.Scoreboard.Players)]
		r.Scoreboard.Players = append(r.Scoreboard.Players, ScoreboardPlayer{ID: p.DissectID})
	}
	return &r.Scoreboard.Players[idx]
}

// movePlayerIndices moves the scoreboard state keyed by player index to the indices the players
// have after deriveTeamRoles removed some: newIndex[i] is the new index of the player at i, or
// -1 if they were removed. readPlayer and scoreboard packets can come before deriveTeamRoles.
func (r *Reader) movePlayerIndices(newIndex []int) {
	moved := func(i int) int {
		if i < 0 || i >= len(newIndex) {
			return -1
		}
		return newIndex[i]
	}
	// a removed player keeps their place, so the zip still pairs the others with their sbIDs
	for k, i := range r.readPlayerOrder {
		r.readPlayerOrder[k] = moved(i)
	}
	for id, i := range r.scoreboardIDToPlayer {
		if j := moved(i); j >= 0 {
			r.scoreboardIDToPlayer[id] = j
		} else {
			delete(r.scoreboardIDToPlayer, id)
		}
	}
	r.scoreboardFinalKills = moveByPlayer(r.scoreboardFinalKills, moved)
	r.lastPlayerScores = moveByPlayer(r.lastPlayerScores, moved)
	revives := r.scoreRevives[:0]
	for _, c := range r.scoreRevives {
		if c.player = moved(c.player); c.player >= 0 {
			revives = append(revives, c)
		}
	}
	r.scoreRevives = revives
	r.lastDefuserPlayerIndex = moved(r.lastDefuserPlayerIndex)
}

func moveByPlayer(m map[int]uint32, moved func(int) int) map[int]uint32 {
	out := make(map[int]uint32, len(m))
	for i, v := range m {
		if j := moved(i); j >= 0 {
			out[j] = v
		}
	}
	return out
}

func scoreboardEntryPlayerID(r *Reader) (id [4]byte, ok bool) {
	start := r.offset - 17
	if start < 0 || start+4 > len(r.b) {
		return id, false
	}
	copy(id[:], r.b[start:start+4])
	return id, true
}

func scoreboardEntryHasMarker(r *Reader) bool {
	pos := r.offset - 18
	return pos >= 0 && r.b[pos] == 0x23
}

// this function fixes kills that were previously recorded as elims
func readScoreboardKills(r *Reader) error {
	kills, err := r.Uint32()
	if err != nil {
		return err
	}

	if r.Header.CodeVersion >= Y10S4 {
		return readScoreboardKillsY10S4(r, kills)
	}

	if err := r.Skip(30); err != nil {
		return err
	}
	id, err := r.Bytes(4)
	if err != nil {
		return err
	}
	idx := r.PlayerIndexByID(id)
	if idx != -1 {
		username := r.Header.Players[idx].Username
		log.Warn().
			Str("username", username).
			Uint32("kills", kills).
			Msg("scoreboard_kill")
	}
	return nil
}

// ensureScoreboardMapping maps the sbIDs seen so far to player indices. In a round with
// scoreboard entity links, a linked sbID maps only to the player owning its player object, and an
// unlinked one only through the +4 rule; a sbID whose owner is not a known player yet is left for
// a later call. A round without links falls back to zipping first-seen order with readPlayer
// order, which shifts every later player when one entity has no player packet.
func (r *Reader) ensureScoreboardMapping() {
	if len(r.scoreboardEntityOwner) == 0 {
		r.zipScoreboardMapping()
		return
	}
	mappedPlayers := make(map[int]bool)
	for _, idx := range r.scoreboardIDToPlayer {
		mappedPlayers[idx] = true
	}
	for pass := 0; pass < 2; pass++ {
		for _, sbID := range r.pendingSBIDs {
			if _, exists := r.scoreboardIDToPlayer[sbID]; exists {
				continue
			}
			owner, linked := r.scoreboardEntityOwner[sbID]
			idx := -1
			switch {
			case pass == 0 && linked:
				idx = r.playerIndexByObject(owner)
			case pass == 1 && !linked:
				idx = r.PlayerIndexByID([]byte{sbID[0] + 4, sbID[1], sbID[2], sbID[3]})
			}
			if idx >= 0 && !mappedPlayers[idx] {
				r.scoreboardIDToPlayer[sbID] = idx
				mappedPlayers[idx] = true
			}
		}
	}
}

// zipScoreboardMapping is the mapping of a round without scoreboard entity links: the n-th sbID
// seen goes to the n-th player read, and the rest use the +4 rule.
func (r *Reader) zipScoreboardMapping() {
	maxZip := len(r.pendingSBIDs)
	if maxZip > len(r.readPlayerOrder) {
		maxZip = len(r.readPlayerOrder)
	}
	for i := 0; i < maxZip; i++ {
		if _, exists := r.scoreboardIDToPlayer[r.pendingSBIDs[i]]; !exists {
			r.scoreboardIDToPlayer[r.pendingSBIDs[i]] = r.readPlayerOrder[i]
		}
	}

	mappedPlayers := make(map[int]bool)
	for _, idx := range r.scoreboardIDToPlayer {
		mappedPlayers[idx] = true
	}
	for i := maxZip; i < len(r.pendingSBIDs); i++ {
		sbID := r.pendingSBIDs[i]
		if _, exists := r.scoreboardIDToPlayer[sbID]; exists {
			continue
		}
		candidateID := []byte{sbID[0] + 4, sbID[1], sbID[2], sbID[3]}
		idx := r.PlayerIndexByID(candidateID)
		if idx >= 0 && !mappedPlayers[idx] {
			r.scoreboardIDToPlayer[sbID] = idx
			mappedPlayers[idx] = true
		}
	}
}

func readScoreboardKillsY10S4(r *Reader, kills uint32) error {
	sbID, ok := scoreboardEntryPlayerID(r)
	if !ok {
		return nil
	}

	isKnown := false
	for _, pending := range r.pendingSBIDs {
		if pending == sbID {
			isKnown = true
			break
		}
	}

	if !isKnown {
		r.pendingSBIDs = append(r.pendingSBIDs, sbID)
		r.scoreboardInitialKills[sbID] = kills
		log.Debug().
			Str("sbID", hex.EncodeToString(sbID[:])).
			Uint32("kills", kills).
			Int("pendingIdx", len(r.pendingSBIDs)-1).
			Msg("scoreboard_kill_header")
		return nil
	}

	r.ensureScoreboardMapping()

	idx, found := r.scoreboardIDToPlayer[sbID]
	if !found || idx < 0 || idx >= len(r.Header.Players) {
		log.Warn().
			Str("sbID", hex.EncodeToString(sbID[:])).
			Uint32("kills", kills).
			Msg("scoreboard_kill_unknown_player")
		return nil
	}

	username := r.Header.Players[idx].Username
	r.scoreboardFinalKills[idx] = kills
	r.lastScoreboardKiller = username
	log.Warn().
		Str("username", username).
		Uint32("kills", kills).
		Str("sbID", hex.EncodeToString(sbID[:])).
		Msg("scoreboard_kill")

	return nil
}

func readScoreboardAssists(r *Reader) error {
	assists, err := r.Uint32()
	if err != nil {
		return err
	}
	if assists == 0 {
		return nil
	}

	if r.Header.CodeVersion >= Y10S4 {
		return readScoreboardAssistsY10S4(r, assists)
	}

	if err = r.Skip(30); err != nil {
		return err
	}
	id, err := r.Bytes(4)
	if err != nil {
		return err
	}
	idx := r.PlayerIndexByID(id)
	username := "N/A"
	if idx != -1 {
		username = r.Header.Players[idx].Username
		row := r.scoreboardRow(idx)
		row.Assists = assists
		row.AssistsFromRound++
	}
	log.Debug().
		Uint32("assists", assists).
		Str("username", username).
		Msg("scoreboard_assists")
	return nil
}

func readScoreboardAssistsY10S4(r *Reader, assists uint32) error {
	if !scoreboardEntryHasMarker(r) {
		return nil
	}
	r.ensureScoreboardMapping()
	sbID, ok := scoreboardEntryPlayerID(r)
	if !ok {
		return nil
	}

	idx, found := r.scoreboardIDToPlayer[sbID]
	username := "N/A"
	if found && idx >= 0 && idx < len(r.Header.Players) {
		username = r.Header.Players[idx].Username
		row := r.scoreboardRow(idx)
		row.Assists = assists
		row.AssistsFromRound++
	}
	log.Debug().
		Uint32("assists", assists).
		Str("username", username).
		Msg("scoreboard_assists")
	return nil
}

func readScoreboardScore(r *Reader) error {
	score, err := r.Uint32()
	if err != nil {
		return err
	}
	if score == 0 {
		return nil
	}

	if r.Header.CodeVersion >= Y10S4 {
		return readScoreboardScoreY10S4(r, score)
	}

	if err = r.Skip(13); err != nil {
		return err
	}
	id, err := r.Bytes(4)
	if err != nil {
		return err
	}
	idx := r.PlayerIndexByID(id)
	username := "N/A"
	if idx != -1 {
		username = r.Header.Players[idx].Username
		r.scoreboardRow(idx).Score = score
	}
	log.Debug().
		Uint32("score", score).
		Str("username", username).
		Msg("scoreboard_score")
	return nil
}

func readScoreboardScoreY10S4(r *Reader, score uint32) error {
	if !scoreboardEntryHasMarker(r) {
		return nil
	}
	r.ensureScoreboardMapping()
	sbID, ok := scoreboardEntryPlayerID(r)
	if !ok {
		return nil
	}

	idx, found := r.scoreboardIDToPlayer[sbID]
	username := "N/A"
	if found && idx >= 0 && idx < len(r.Header.Players) {
		username = r.Header.Players[idx].Username
		r.scoreboardRow(idx).Score = score

		// +100 score bonus identifies defuser planter/disabler
		if r.pendingDefuserPlantIdx >= 0 {
			prevScore := r.lastPlayerScores[idx]
			delta := score - prevScore
			correctTeam := false
			if r.pendingDefuserIsPlant {
				atkIdx := r.getTeamByRole(Attack)
				correctTeam = atkIdx >= 0 && r.Header.Players[idx].TeamIndex == atkIdx
			} else {
				defIdx := r.getTeamByRole(Defense)
				correctTeam = defIdx >= 0 && r.Header.Players[idx].TeamIndex == defIdx
			}
			if prevScore > 0 && delta == 100 && correctTeam {
				r.MatchFeedback[r.pendingDefuserPlantIdx].Username = username
				r.lastDefuserPlayerIndex = idx
				log.Debug().
					Str("username", username).
					Uint32("prevScore", prevScore).
					Uint32("newScore", score).
					Msg("defuser_player_identified_by_score")
				r.pendingDefuserPlantIdx = -1
			}
		}

		// +50 score bonus, matched with revives read from the health state
		if prev, seen := r.lastPlayerScores[idx]; seen && score-prev == 50 && r.clock.started {
			r.scoreRevives = append(r.scoreRevives, scoreRevive{player: idx, elapsed: r.elapsed()})
		}

		r.lastPlayerScores[idx] = score
	}
	log.Debug().
		Uint32("score", score).
		Str("username", username).
		Msg("scoreboard_score")
	return nil
}

func (r *Reader) reconcileKillsFromScoreboard() {
	if len(r.scoreboardInitialKills) == 0 {
		return
	}

	r.ensureScoreboardMapping()
	expectedKills := make(map[int]int)
	for sbID, playerIdx := range r.scoreboardIDToPlayer {
		initial, hasInitial := r.scoreboardInitialKills[sbID]
		final, hasFinal := r.scoreboardFinalKills[playerIdx]
		if hasInitial {
			if hasFinal {
				expectedKills[playerIdx] = int(final - initial)
			} else {
				expectedKills[playerIdx] = 0
			}
		}
	}

	actualKills := make(map[int]int)
	for _, u := range r.MatchFeedback {
		if u.Type == Kill && !u.TeamKill {
			idx := r.PlayerIndexByUsername(u.Username)
			if idx >= 0 {
				actualKills[idx]++
			}
		}
	}

	type delta struct {
		idx  int
		diff int
	}
	var overCredited, underCredited []delta
	for playerIdx, expected := range expectedKills {
		actual := actualKills[playerIdx]
		if actual > expected {
			overCredited = append(overCredited, delta{playerIdx, actual - expected})
		} else if actual < expected {
			underCredited = append(underCredited, delta{playerIdx, expected - actual})
		}
	}

	if len(overCredited) == 0 || len(underCredited) == 0 {
		return
	}
	// expectedKills is a map, so the lists come out in random order; sort them so that which
	// kill moves to whom is the same on every run (PR #12)
	sort.Slice(overCredited, func(a, b int) bool { return overCredited[a].idx < overCredited[b].idx })
	sort.Slice(underCredited, func(a, b int) bool { return underCredited[a].idx < underCredited[b].idx })

	// pass 0 moves only the kill that directly followed the under-credited player's scoreboard
	// increment, for every pair, before pass 1 falls back to any eligible kill of the
	// over-credited player
	for pass := 0; pass < 2; pass++ {
		for ui := range underCredited {
			underIdx := underCredited[ui].idx
			if underIdx < 0 || underIdx >= len(r.Header.Players) {
				continue
			}
			underUsername := r.Header.Players[underIdx].Username
			underTeam := r.Header.Players[underIdx].TeamIndex

			for oi := range overCredited {
				if underCredited[ui].diff <= 0 {
					break
				}
				// an exhausted over-credited player ends only its own pair
				if overCredited[oi].diff <= 0 {
					continue
				}
				overIdx := overCredited[oi].idx
				if overIdx < 0 || overIdx >= len(r.Header.Players) {
					continue
				}
				overUsername := r.Header.Players[overIdx].Username

				for i := range r.MatchFeedback {
					if underCredited[ui].diff <= 0 || overCredited[oi].diff <= 0 {
						break
					}
					u := &r.MatchFeedback[i]
					if u.Type != Kill || u.TeamKill || u.Username != overUsername {
						continue
					}
					if pass == 0 && u.scoreboardKiller != underUsername {
						continue
					}
					targetIdx := r.PlayerIndexByUsername(u.Target)
					if targetIdx < 0 {
						continue
					}
					targetTeam := r.Header.Players[targetIdx].TeamIndex
					if underTeam == targetTeam || underUsername == u.Target {
						continue
					}
					log.Debug().
						Str("original", overUsername).
						Str("corrected", underUsername).
						Str("target", u.Target).
						Bool("timing_match", pass == 0).
						Msg("scoreboard_reconcile_kill")
					u.Username = underUsername
					underCredited[ui].diff--
					overCredited[oi].diff--
				}
			}
		}
	}
}
