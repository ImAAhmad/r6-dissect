package dissect

import (
	"fmt"
	"testing"
)

// y10s4Entry is a Y10S4+ scoreboard field record, 23 <sbID> <8 bytes> <u32>, with the offset the
// score and assists listeners start at (the value's size byte).
func y10s4Entry(r *Reader, id [4]byte, v uint32) {
	r.b = append(append(append(append([]byte{0x23}, id[:]...), make([]byte, 8)...), u32(v)...), 0)
	r.offset = 13
}

func writeScore(t *testing.T, r *Reader, id [4]byte, score uint32) {
	t.Helper()
	y10s4Entry(r, id, score)
	if err := readScoreboardScore(r); err != nil {
		t.Fatal(err)
	}
}

func writeAssists(t *testing.T, r *Reader, id [4]byte, assists uint32) {
	t.Helper()
	y10s4Entry(r, id, assists)
	if err := readScoreboardAssists(r); err != nil {
		t.Fatal(err)
	}
}

var ghostRound = []string{"a0", "ghost", "a2", "a3", "a4", "d0", "d1", "d2", "d3", "d4"}

func ghostEntity(name string) [4]byte { return [4]byte{0xE0, name[0], name[len(name)-1], 0xF0} }
func ghostObject(name string) uint64 {
	return 0xF0000000 | uint64(name[0])<<8 | uint64(name[len(name)-1])
}
func ghostScore(name string) uint32 { return 100 + 10*uint32(name[0]) + uint32(name[len(name)-1]) }

// ghostReader returns a Y10S4 round whose header lists a player ("ghost", second) who has no
// player packet. readPlayer reads the other nine, in header order, and each has a scoreboard
// entity (linked to their player object when links is set). When scoreFirst is set, a score
// packet for a2 is read before deriveTeamRoles, so the mapping is built before the ghost is
// removed. Then deriveTeamRoles removes the ghost, and a tenth player ("late") is read.
func ghostReader(t *testing.T, links, scoreFirst bool) *Reader {
	t.Helper()
	r := linkReader()
	r.Header.Players = nil
	r.readPlayerOrder = nil
	r.inventory = newInventoryState()
	for i, name := range ghostRound { // the header's players: no operator yet
		r.Header.Players = append(r.Header.Players, Player{Username: name, TeamIndex: i / 5})
	}
	read := func(name string) {
		idx := r.PlayerIndexByUsername(name)
		if idx < 0 {
			r.Header.Players = append(r.Header.Players, Player{Username: name, TeamIndex: 1})
			idx = len(r.Header.Players) - 1
		}
		id := ghostEntity(name)
		r.Header.Players[idx].Operator = Recruit
		r.Header.Players[idx].DissectID = []byte{id[0] + 4, id[1], id[2], id[3]} // the +4 rule holds
		r.readPlayerOrder = append(r.readPlayerOrder, idx)
		r.trackPlayerObject(ghostObject(name), r.Header.Players[idx])
		r.pendingSBIDs = append(r.pendingSBIDs, id)
		if links {
			r.scoreboardEntityOwner[id] = ghostObject(name)
		}
	}
	for _, name := range ghostRound {
		if name != "ghost" {
			read(name)
		}
	}
	if scoreFirst {
		writeScore(t, r, ghostEntity("a2"), 1)
	}
	r.deriveTeamRoles()
	if len(r.Header.Players) != 9 {
		t.Fatalf("%d players after deriveTeamRoles, want 9", len(r.Header.Players))
	}
	read("late")
	return r
}

// On Y10S4+, when deriveTeamRoles removes a header-only player who is not last, every player keeps
// their own score and assists, the player read after that (a late joiner) included.
func TestDeriveTeamRoles_GhostKeepsScoresOnTheirPlayers(t *testing.T) {
	for _, links := range []bool{true, false} {
		for _, scoreFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("links=%v,scoreFirst=%v", links, scoreFirst), func(t *testing.T) {
				r := ghostReader(t, links, scoreFirst)
				for _, p := range r.Header.Players {
					writeScore(t, r, ghostEntity(p.Username), ghostScore(p.Username))
				}
				writeAssists(t, r, ghostEntity("late"), 2)
				writeAssists(t, r, ghostEntity("d4"), 1)
				stats := r.PlayerStats()
				if len(stats) != 10 || len(r.Scoreboard.Players) != 10 {
					t.Fatalf("%d players, %d rows; want 10 and 10", len(stats), len(r.Scoreboard.Players))
				}
				for _, s := range stats {
					if want := int(ghostScore(s.Username)); s.Score != want {
						t.Errorf("%s: score %d, want %d", s.Username, s.Score, want)
					}
					if want := map[string]int{"late": 1, "d4": 1}[s.Username]; s.Assists != want {
						t.Errorf("%s: assists %d, want %d", s.Username, s.Assists, want)
					}
				}
			})
		}
	}
}

// The state keyed by player index moves with the players when deriveTeamRoles removes one.
func TestDeriveTeamRoles_RemapsScoreboardState(t *testing.T) {
	r := lateJoinerReader(Y10S4)
	r.Scoreboard.Players = nil
	r.Header.Players = append([]Player{{Username: "ghost"}}, r.Header.Players[:10]...) // ghost first: everyone moves down one
	a0, a2 := [4]byte{1}, [4]byte{3}
	r.readPlayerOrder = []int{1, 3, 10}
	r.scoreboardIDToPlayer = map[[4]byte]int{a0: 1, a2: 3, {0x0C}: 0}
	r.scoreboardFinalKills = map[int]uint32{1: 2, 3: 4, 0: 9}
	r.lastPlayerScores = map[int]uint32{1: 200, 3: 400, 0: 900}
	r.scoreRevives = []scoreRevive{{player: 3, elapsed: 5}, {player: 0, elapsed: 6}}
	r.lastDefuserPlayerIndex = 10
	r.deriveTeamRoles()

	if got, want := fmt.Sprint(r.readPlayerOrder), "[0 2 9]"; got != want {
		t.Errorf("readPlayerOrder %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(r.scoreboardIDToPlayer), fmt.Sprint(map[[4]byte]int{a0: 0, a2: 2}); got != want {
		t.Errorf("scoreboardIDToPlayer %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(r.scoreboardFinalKills), "map[0:2 2:4]"; got != want {
		t.Errorf("scoreboardFinalKills %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(r.lastPlayerScores), "map[0:200 2:400]"; got != want {
		t.Errorf("lastPlayerScores %s, want %s", got, want)
	}
	if len(r.scoreRevives) != 1 || r.scoreRevives[0] != (scoreRevive{player: 2, elapsed: 5}) {
		t.Errorf("scoreRevives %+v, want [{player:2 elapsed:5}]", r.scoreRevives)
	}
	if r.lastDefuserPlayerIndex != 9 {
		t.Errorf("lastDefuserPlayerIndex %d, want 9", r.lastDefuserPlayerIndex)
	}
}

// A +50 score update after deriveTeamRoles removed a player is compared with the same player's
// previous score, read before the removal.
func TestDeriveTeamRoles_ReviveBonusAfterGhost(t *testing.T) {
	r := ghostReader(t, true, true) // a2 scored 1 before deriveTeamRoles
	r.clock.started = true
	writeScore(t, r, ghostEntity("a3"), 51) // a3 has no previous score: not a bonus
	writeScore(t, r, ghostEntity("a2"), 51)
	if len(r.scoreRevives) != 1 || r.Header.Players[r.scoreRevives[0].player].Username != "a2" {
		t.Fatalf("scoreRevives %+v, want one for a2 (index %d)", r.scoreRevives, r.PlayerIndexByUsername("a2"))
	}
}

// A sbID mapped to an index past the players (what a stale index looked like before
// deriveTeamRoles moved the mapping) is ignored by every Y10S4+ scoreboard writer.
func TestScoreboardY10S4_IndexPastPlayersIgnored(t *testing.T) {
	r := lateJoinerReader(Y10S4)
	r.Header.Players = r.Header.Players[:10]
	id := [4]byte{0xAA, 0xBB, 0xCC, 0xDD}
	r.scoreboardIDToPlayer[id] = len(r.Header.Players)
	writeScore(t, r, id, 300)
	writeAssists(t, r, id, 1)
	r.pendingSBIDs = [][4]byte{id}
	y10s4Entry(r, id, 2)
	if err := readScoreboardKills(r); err != nil {
		t.Fatal(err)
	}
	if len(r.Scoreboard.Players) != 10 || len(r.lastPlayerScores) != 0 || len(r.scoreboardFinalKills) != 0 {
		t.Errorf("%d rows, scores %v, kills %v; want 10 rows and nothing written", len(r.Scoreboard.Players), r.lastPlayerScores, r.scoreboardFinalKills)
	}
	for _, s := range r.Scoreboard.Players {
		if s.Score != 0 || s.Assists != 0 {
			t.Errorf("row %v written", s)
		}
	}
}
