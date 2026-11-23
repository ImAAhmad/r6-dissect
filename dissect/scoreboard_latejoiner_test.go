package dissect

import (
	"encoding/binary"
	"testing"
)

// lateJoinerReader returns the state readPlayer leaves behind when an 11th player is read
// after deriveTeamRoles ran: 11 entries in Header.Players, 10 scoreboard rows.
func lateJoinerReader(codeVersion int) *Reader {
	r := &Reader{
		Header:                 Header{CodeVersion: codeVersion},
		pendingDefuserPlantIdx: -1,
		lastDefuserPlayerIndex: -1,
		scoreboardIDToPlayer:   make(map[[4]byte]int),
		scoreboardInitialKills: make(map[[4]byte]uint32),
		scoreboardFinalKills:   make(map[int]uint32),
		lastPlayerScores:       make(map[int]uint32),
	}
	r.Header.Teams[0].Won = true
	for i, name := range []string{"a0", "a1", "a2", "a3", "a4", "d0", "d1", "d2", "d3", "d4"} {
		r.Header.Players = append(r.Header.Players, Player{Username: name, TeamIndex: i / 5, Operator: Recruit, DissectID: []byte{byte(i + 1), 0, 0, 0}})
		r.Scoreboard.Players = append(r.Scoreboard.Players, ScoreboardPlayer{ID: []byte{byte(i + 1), 0, 0, 0}})
	}
	// what readPlayer does for a username it has not seen: append to Header.Players only
	r.Header.Players = append(r.Header.Players, Player{Username: "late", TeamIndex: 1, Operator: Recruit, DissectID: []byte{0x0B, 0, 0, 0}})
	return r
}

// u32 is a uint32 the way Reader.Uint32 reads it: a size byte, then 4 bytes little-endian.
func u32(v uint32) []byte {
	b := []byte{0x04, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(b[1:], v)
	return b
}

// Before Y10S4, a score packet is <score u32> <13 bytes> <dissect id>. Reader.Skip wants one byte past the end.
func TestLateJoiner_ScoreKept(t *testing.T) {
	r := lateJoinerReader(Y9S1)
	r.b = append(append(u32(1234), make([]byte, 13)...), 0x0B, 0, 0, 0, 0)
	if err := readScoreboardScore(r); err != nil {
		t.Fatal(err)
	}
	stats := r.PlayerStats()
	if len(stats) != 11 || stats[10].Username != "late" {
		t.Fatalf("got %d rows, want 11 with late last", len(stats))
	}
	if stats[10].Score != 1234 {
		t.Errorf("late player's score: got %d, want 1234", stats[10].Score)
	}
}

// Before Y10S4, an assist packet is <assists u32> <30 bytes> <dissect id>.
func TestLateJoiner_AssistKept(t *testing.T) {
	r := lateJoinerReader(Y9S1)
	r.b = append(append(u32(1), make([]byte, 30)...), 0x0B, 0, 0, 0, 0)
	if err := readScoreboardAssists(r); err != nil {
		t.Fatal(err)
	}
	if got := r.PlayerStats()[10].Assists; got != 1 {
		t.Errorf("late player's assists: got %d, want 1", got)
	}
}

// Y10S4+: 23 <sbID> <8 bytes> <score u32>; the listener leaves the offset at the score's size byte,
// and the marker and sbID are found 18 and 17 bytes before the end of the score.
func TestLateJoiner_ScoreKeptY10S4(t *testing.T) {
	r := lateJoinerReader(Y10S4)
	sbID := [4]byte{0xAA, 0xBB, 0xCC, 0xDD}
	r.scoreboardIDToPlayer[sbID] = 10
	r.b = append(append(append(append([]byte{0x23}, sbID[:]...), make([]byte, 8)...), u32(777)...), 0)
	r.offset = 13
	if err := readScoreboardScore(r); err != nil {
		t.Fatal(err)
	}
	if got := r.PlayerStats()[10].Score; got != 777 {
		t.Errorf("late player's score: got %d, want 777", got)
	}
}

// A score read before deriveTeamRoles stays with its player when deriveTeamRoles drops a
// header-only player (operator 0) that comes before them.
func TestLateJoiner_DeriveKeepsRowsAligned(t *testing.T) {
	r := lateJoinerReader(Y9S1)
	r.Scoreboard.Players = nil                                                         // deriveTeamRoles has not run yet
	ghost := Player{Username: "ghost", TeamIndex: 0, DissectID: []byte{0x0C, 0, 0, 0}} // listed in the header, never read
	r.Header.Players = append([]Player{ghost}, r.Header.Players[:10]...)
	r.b = append(append(u32(500), make([]byte, 13)...), 0x03, 0, 0, 0, 0) // a2
	if err := readScoreboardScore(r); err != nil {
		t.Fatal(err)
	}
	r.deriveTeamRoles()
	if len(r.Scoreboard.Players) != len(r.Header.Players) {
		t.Fatalf("%d rows for %d players", len(r.Scoreboard.Players), len(r.Header.Players))
	}
	for _, s := range r.PlayerStats() {
		want := 0
		if s.Username == "a2" {
			want = 500
		}
		if s.Score != want {
			t.Errorf("%s: score %d, want %d", s.Username, s.Score, want)
		}
	}
}

// A reconnect keeps the username and gets a new dissect id; readPlayer updates the existing
// player, so the score lands on the same row and the round has one row for them.
func TestLateJoiner_ReconnectKeepsOneRow(t *testing.T) {
	r := lateJoinerReader(Y9S1)
	r.Header.Players = r.Header.Players[:10]
	r.Header.Players[3].DissectID = []byte{0x44, 0, 0, 0} // what readPlayer does when the username matches
	r.b = append(append(u32(900), make([]byte, 13)...), 0x44, 0, 0, 0, 0)
	if err := readScoreboardScore(r); err != nil {
		t.Fatal(err)
	}
	stats := r.PlayerStats()
	if len(stats) != 10 || stats[3].Username != "a3" || stats[3].Score != 900 {
		t.Errorf("got %d rows, a3=%+v; want 10 rows and a3 with 900", len(stats), stats[3])
	}
}

// Y10S4+ assists: same entry layout as the score, the value read is the assist count.
func TestLateJoiner_AssistKeptY10S4(t *testing.T) {
	r := lateJoinerReader(Y10S4)
	sbID := [4]byte{0xAA, 0xBB, 0xCC, 0xDD}
	r.scoreboardIDToPlayer[sbID] = 10
	r.b = append(append(append(append([]byte{0x23}, sbID[:]...), make([]byte, 8)...), u32(2)...), 0)
	r.offset = 13
	if err := readScoreboardAssists(r); err != nil {
		t.Fatal(err)
	}
	if got := r.PlayerStats()[10].Assists; got != 1 {
		t.Errorf("late player's assists: got %d, want 1", got)
	}
}

// Two late joiners: a packet for the second creates the first one's row too, in order,
// and each row keeps its own player's id and score.
func TestLateJoiner_TwoLateJoiners(t *testing.T) {
	r := lateJoinerReader(Y9S1)
	r.Header.Players = append(r.Header.Players, Player{Username: "late2", TeamIndex: 1, Operator: Recruit, DissectID: []byte{0x0C, 0, 0, 0}})
	r.b = append(append(u32(300), make([]byte, 13)...), 0x0C, 0, 0, 0, 0)
	if err := readScoreboardScore(r); err != nil {
		t.Fatal(err)
	}
	r.b = append(append(u32(200), make([]byte, 13)...), 0x0B, 0, 0, 0, 0)
	r.offset = 0
	if err := readScoreboardScore(r); err != nil {
		t.Fatal(err)
	}
	if len(r.Scoreboard.Players) != 12 {
		t.Fatalf("%d rows, want 12", len(r.Scoreboard.Players))
	}
	stats := r.PlayerStats()
	if stats[10].Username != "late" || stats[10].Score != 200 || stats[11].Username != "late2" || stats[11].Score != 300 {
		t.Errorf("got late=%+v late2=%+v; want 200 and 300", stats[10], stats[11])
	}
	if string(r.Scoreboard.Players[10].ID) != string([]byte{0x0B, 0, 0, 0}) || string(r.Scoreboard.Players[11].ID) != string([]byte{0x0C, 0, 0, 0}) {
		t.Errorf("row ids %v %v", r.Scoreboard.Players[10].ID, r.Scoreboard.Players[11].ID)
	}
}

// A reconnect after the late joiner has a row still scores on that one row.
func TestLateJoiner_LateJoinerReconnects(t *testing.T) {
	r := lateJoinerReader(Y9S1)
	r.b = append(append(u32(100), make([]byte, 13)...), 0x0B, 0, 0, 0, 0)
	if err := readScoreboardScore(r); err != nil {
		t.Fatal(err)
	}
	r.Header.Players[10].DissectID = []byte{0x55, 0, 0, 0} // same username, new id
	r.b = append(append(u32(150), make([]byte, 13)...), 0x55, 0, 0, 0, 0)
	r.offset = 0
	if err := readScoreboardScore(r); err != nil {
		t.Fatal(err)
	}
	stats := r.PlayerStats()
	if len(stats) != 11 || len(r.Scoreboard.Players) != 11 || stats[10].Score != 150 {
		t.Errorf("got %d players, %d rows, late=%+v; want 11, 11, 150", len(stats), len(r.Scoreboard.Players), stats[10])
	}
}

// deriveTeamRoles on an already-aligned set of rows must not duplicate them.
func TestLateJoiner_DeriveWithRowsDoesNotDuplicate(t *testing.T) {
	r := lateJoinerReader(Y9S1)
	r.Header.Players = r.Header.Players[:10]
	r.Scoreboard.Players[4].Score = 42
	r.deriveTeamRoles()
	if len(r.Scoreboard.Players) != 10 || r.Scoreboard.Players[4].Score != 42 {
		t.Errorf("%d rows, a4 score %d; want 10 and 42", len(r.Scoreboard.Players), r.Scoreboard.Players[4].Score)
	}
}
