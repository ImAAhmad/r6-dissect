package dissect

import (
	"encoding/json"
	"strings"
	"testing"
)

func kill(killer, target string, elapsed float64) MatchUpdate {
	return MatchUpdate{Type: Kill, Username: killer, Target: target, Phase: ActionPhase, ElapsedSeconds: elapsed}
}

func teamKill(killer, target string, elapsed float64) MatchUpdate {
	u := kill(killer, target, elapsed)
	u.TeamKill = true
	return u
}

func TestOpeningKill_SkipsTeamKill(t *testing.T) {
	r := &Reader{MatchFeedback: []MatchUpdate{teamKill("a1", "a2", 5), kill("a1", "d1", 8)}}
	if got := r.OpeningKill().Target; got != "d1" {
		t.Fatalf("want the first non-teamkill (target d1), got target %q", got)
	}
}

// other events between two kills do not break or create a trade.
func TestTrades_KillsOnly(t *testing.T) {
	stop := MatchUpdate{Type: DefuserPlantStop, Username: "a2", Phase: ActionPhase, ElapsedSeconds: 11}
	r := &Reader{MatchFeedback: []MatchUpdate{kill("d1", "a1", 10), stop, kill("a2", "d1", 12)}}
	trades := r.Trades()
	if len(trades) != 1 || trades[0][0].Target != "a1" || trades[0][1].Target != "d1" {
		t.Fatalf("want one trade [d1→a1, a2→d1], got %+v", trades)
	}
}

func TestTrades_SkipsTeamKill(t *testing.T) {
	r := &Reader{MatchFeedback: []MatchUpdate{kill("d1", "a1", 10), teamKill("a2", "a3", 11), kill("a2", "d1", 12)}}
	trades := r.Trades()
	if len(trades) != 1 || trades[0][0].Target != "a1" || trades[0][1].Target != "d1" {
		t.Fatalf("want one trade [d1→a1, a2→d1] without the teamkill, got %+v", trades)
	}
}

func threeVThree() Header {
	return Header{
		Teams: [2]Team{{Role: Attack}, {Role: Defense, Won: true}},
		Players: []Player{
			{Username: "a1", TeamIndex: 0}, {Username: "a2", TeamIndex: 0}, {Username: "a3", TeamIndex: 0},
			{Username: "d1", TeamIndex: 1}, {Username: "d2", TeamIndex: 1}, {Username: "d3", TeamIndex: 1},
		},
	}
}

func statsOf(stats []PlayerRoundStats, username string) PlayerRoundStats {
	for _, s := range stats {
		if s.Username == username {
			return s
		}
	}
	return PlayerRoundStats{}
}

// an opponent who dies without a killer during the clutch was still part of it.
func TestPlayerStats_OneVxMatchesClutch(t *testing.T) {
	r := &Reader{Header: threeVThree(), MatchFeedback: []MatchUpdate{
		kill("d1", "a1", 10), kill("a2", "d2", 20), kill("a3", "d3", 25),
		{Type: Death, Username: "a2", Phase: ActionPhase, ElapsedSeconds: 30},
		kill("d1", "a3", 40),
	}}
	s := statsOf(r.PlayerStats(), "d1")
	if s.ClutchDetail == nil || s.ClutchDetail.OpponentsAlive != 2 || s.OneVx != 2 {
		t.Fatalf("want 1vX 2 and opponentsAlive 2, got 1vX %d, clutch %+v", s.OneVx, s.ClutchDetail)
	}
}

func TestPlayerStats_OneVxNoClutchFallback(t *testing.T) {
	r := &Reader{Header: threeVThree(), MatchFeedback: []MatchUpdate{kill("d1", "a1", 10)}}
	for _, s := range r.PlayerStats() {
		if s.OneVx != 0 || s.ClutchDetail != nil {
			t.Fatalf("no clutch: want 1vX 0 for %s, got %d, clutch %+v", s.Username, s.OneVx, s.ClutchDetail)
		}
	}
}

// damageReader builds a round whose players have health series: totals[i] HP lost by player i
// as a defender-style series [spawn, spawn-total]; a negative total gives the player no samples.
func damageReader(names []string, totals []int) *Reader {
	r := &Reader{healthToSlot: map[[4]byte][4]byte{}, Header: Header{Teams: [2]Team{{Role: Attack}, {Role: Defense}}}}
	for i, n := range names {
		slot, health := [4]byte{byte(i + 1)}, [4]byte{0xA0, byte(i + 1)}
		r.Header.Players = append(r.Header.Players, Player{Username: n, TeamIndex: 1, slotID: slot[:]})
		if totals[i] < 0 {
			continue
		}
		r.healthToSlot[health] = slot
		r.healthSamples = append(r.healthSamples,
			healthSample{health: health, hp: 110},
			healthSample{health: health, hp: uint32(110 - totals[i]), started: true, phase: ActionPhase})
	}
	return r
}

func TestPlayerStats_DamageTakenOmittedWithoutHealth(t *testing.T) {
	r := damageReader([]string{"a", "b"}, []int{-1, -1})
	for _, s := range r.PlayerStats() {
		if s.DamageTaken != nil {
			t.Fatalf("%s: want nil damage, got %d", s.Username, *s.DamageTaken)
		}
		b, _ := json.Marshal(s)
		if strings.Contains(string(b), "damageTaken") {
			t.Fatalf("%s: damageTaken serialised: %s", s.Username, b)
		}
	}
}

func TestPlayerStats_DamageTakenZeroIsKept(t *testing.T) {
	r := damageReader([]string{"a"}, []int{0})
	s := r.PlayerStats()[0]
	b, _ := json.Marshal(s)
	if s.DamageTaken == nil || *s.DamageTaken != 0 || !strings.Contains(string(b), `"damageTaken":0`) {
		t.Fatalf("an untouched player must report 0, not unknown: %s", b)
	}
}

// each row owns its value: no two rows share a pointer.
func TestPlayerStats_DamageTakenRowsDistinct(t *testing.T) {
	r := damageReader([]string{"a", "b"}, []int{10, 20})
	st := r.PlayerStats()
	if *st[0].DamageTaken != 10 || *st[1].DamageTaken != 20 || st[0].DamageTaken == st[1].DamageTaken {
		t.Fatalf("rows share or mix values: %d %d", *st[0].DamageTaken, *st[1].DamageTaken)
	}
}

func TestMatchPlayerStats_DamageTakenSums(t *testing.T) {
	r1 := damageReader([]string{"a", "b", "c"}, []int{40, -1, -1})
	r2 := damageReader([]string{"a", "b", "c"}, []int{110, 30, -1})
	got := (&MatchReader{rounds: []*Reader{r1, r2}}).PlayerStats()
	want := map[string]*int{"a": ptr(150), "b": ptr(30), "c": nil}
	for _, s := range got {
		w := want[s.Username]
		if (w == nil) != (s.DamageTaken == nil) || (w != nil && *w != *s.DamageTaken) {
			t.Errorf("%s: want %v got %v", s.Username, w, s.DamageTaken)
		}
	}
	b, _ := json.Marshal(got[2])
	if strings.Contains(string(b), "damageTaken") {
		t.Errorf("match row without data serialised damageTaken: %s", b)
	}
}

func ptr(v int) *int { return &v }
