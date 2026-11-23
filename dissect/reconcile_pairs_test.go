package dissect

import (
	"strings"
	"testing"
)

// reconcileRuns is how often each reconciliation test repeats: the over- and under-credited
// lists are built by ranging over a map, so a single run can pass by luck.
const reconcileRuns = 200

// reconcileReader builds a Y10S4+ round for reconcileKillsFromScoreboard. Player i owns the
// scoreboard entity {i+1}, every kill counter starts at 0, gained[name] is the counter at the
// end of the round (a player not listed gains nothing), and feed is the kill feed.
func reconcileReader(players []Player, gained map[string]uint32, feed []MatchUpdate) *Reader {
	r := &Reader{
		scoreboardIDToPlayer:   map[[4]byte]int{},
		scoreboardInitialKills: map[[4]byte]uint32{},
		scoreboardFinalKills:   map[int]uint32{},
	}
	r.Header.Players = players
	for i, p := range players {
		sbID := [4]byte{byte(i + 1)}
		r.scoreboardIDToPlayer[sbID] = i
		r.scoreboardInitialKills[sbID] = 0
		if kills, ok := gained[p.Username]; ok {
			r.scoreboardFinalKills[i] = kills
		}
	}
	r.MatchFeedback = feed
	return r
}

// feedKillers is the kill feed as "killer>target " pairs, in feed order.
func feedKillers(r *Reader) string {
	var b strings.Builder
	for _, u := range r.MatchFeedback {
		b.WriteString(u.Username + ">" + u.Target + " ")
	}
	return b.String()
}

// players O1, O2, U1, U2 on team 0 (indices 0-3) and E1, E2 on team 1.
func twoPairPlayers() []Player {
	return []Player{
		{Username: "O1"}, {Username: "O2"}, {Username: "U1"}, {Username: "U2"},
		{Username: "E1", TeamIndex: 1}, {Username: "E2", TeamIndex: 1},
	}
}

// Two over-credited (O1, O2) and two under-credited (U1, U2) players, and each kill directly
// followed its under-credited player's scoreboard increment: each kill moves to its own timing
// match, in whatever order the players are processed.
func TestReconcileKillsFromScoreboard_TwoPairsTimingMatched(t *testing.T) {
	for run := 0; run < reconcileRuns; run++ {
		r := reconcileReader(twoPairPlayers(), map[string]uint32{"U1": 1, "U2": 1}, []MatchUpdate{
			{Type: Kill, Username: "O1", Target: "E1", scoreboardKiller: "U1"},
			{Type: Kill, Username: "O2", Target: "E2", scoreboardKiller: "U2"},
		})
		r.reconcileKillsFromScoreboard()
		if a, b := r.MatchFeedback[0].Username, r.MatchFeedback[1].Username; a != "U1" || b != "U2" {
			t.Fatalf("run %d: killers %q, %q; want U1, U2", run, a, b)
		}
	}
}

// From PR #12 (@ElyasYassin): the same players with no timing match, so the fallback picks the
// kills. The scoreboard gives U1 and U2 one kill each and O1 and O2 none, so both kills move. The
// lists are sorted by player index, so U1 (index 2) takes O1's kill and U2, finding O1 exhausted,
// takes O2's, on every run.
func TestReconcileKillsFromScoreboard_Deterministic(t *testing.T) {
	first := ""
	for i := 0; i < reconcileRuns; i++ {
		r := reconcileReader(twoPairPlayers(), map[string]uint32{"U1": 1, "U2": 1}, []MatchUpdate{
			{Type: Kill, Username: "O1", Target: "E1"},
			{Type: Kill, Username: "O2", Target: "E2"},
		})
		r.reconcileKillsFromScoreboard()
		got := feedKillers(r)
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("run %d attributed kills as %q, run 0 as %q", i, got, first)
		}
	}
	if want := "U1>E1 U2>E2 "; first != want {
		t.Fatalf("attribution %q, want %q", first, want)
	}
}

// The limits of the pairing: one over-credited player against two under-credited ones and the
// reverse, a timing match that names a player the scoreboard does not under-credit, players on
// both teams, an over-credited player's surplus, and the player-index order of the fallback.
// Every case must give the same feed on every run.
func TestReconcileKillsFromScoreboard_PairLimits(t *testing.T) {
	enemies := func(names ...string) []Player {
		var ps []Player
		for _, n := range names {
			ps = append(ps, Player{Username: n, TeamIndex: 1})
		}
		return ps
	}
	team0 := func(names ...string) []Player {
		var ps []Player
		for _, n := range names {
			ps = append(ps, Player{Username: n})
		}
		return ps
	}
	kill := func(killer, target, sbKiller string) MatchUpdate {
		return MatchUpdate{Type: Kill, Username: killer, Target: target, scoreboardKiller: sbKiller}
	}
	cases := []struct {
		name    string
		players []Player
		gained  map[string]uint32
		feed    []MatchUpdate
		want    string
	}{
		{
			name:    "one over, two under: U1's fallback leaves U2 its timing match",
			players: append(team0("O1", "U1", "U2"), enemies("E1", "E2")...),
			gained:  map[string]uint32{"U1": 1, "U2": 1},
			feed:    []MatchUpdate{kill("O1", "E1", "U2"), kill("O1", "E2", "")},
			want:    "U2>E1 U1>E2 ",
		},
		{
			name:    "one over, two under, no timing match: each under-credited player takes one kill",
			players: append(team0("O1", "U1", "U2"), enemies("E1", "E2")...),
			gained:  map[string]uint32{"U1": 1, "U2": 1},
			feed:    []MatchUpdate{kill("O1", "E1", ""), kill("O1", "E2", "")},
			want:    "U1>E1 U2>E2 ",
		},
		{
			name:    "two over, one under: O2's timing match beats O1's kill",
			players: append(team0("O1", "O2", "U1"), enemies("E1", "E2")...),
			gained:  map[string]uint32{"U1": 1},
			feed:    []MatchUpdate{kill("O1", "E1", ""), kill("O2", "E2", "U1")},
			want:    "O1>E1 U1>E2 ",
		},
		{
			name:    "the timing match names a player who is not under-credited: the fallback moves the kill",
			players: append(team0("O1", "X", "U1"), enemies("E1", "E2")...),
			gained:  map[string]uint32{"X": 1, "U1": 1},
			feed:    []MatchUpdate{kill("O1", "E1", "X"), kill("X", "E2", "")},
			want:    "U1>E1 X>E2 ",
		},
		{
			name:    "an over-credited player on the other team: the move would be a teamkill",
			players: []Player{{Username: "U1"}, {Username: "T0"}, {Username: "OE", TeamIndex: 1}},
			gained:  map[string]uint32{"U1": 1},
			feed:    []MatchUpdate{kill("OE", "T0", "U1")},
			want:    "OE>T0 ",
		},
		{
			name: "both teams: U1 skips the exhausted O0 and reaches O1",
			players: []Player{
				{Username: "O0"}, {Username: "U0"},
				{Username: "O1", TeamIndex: 1}, {Username: "U1", TeamIndex: 1},
				{Username: "A"}, {Username: "B", TeamIndex: 1},
			},
			gained: map[string]uint32{"U0": 1, "U1": 1},
			feed:   []MatchUpdate{kill("O0", "B", ""), kill("O1", "A", "")},
			want:   "U0>B U1>A ",
		},
		{
			name:    "the fallback takes no more kills than an over-credited player's surplus",
			players: append(team0("O1", "O2", "U1"), enemies("E1", "E2", "E3", "E4")...),
			gained:  map[string]uint32{"O1": 1, "O2": 1, "U1": 2},
			feed: []MatchUpdate{
				kill("O1", "E1", ""), kill("O1", "E2", ""), kill("O2", "E3", ""), kill("O2", "E4", ""),
			},
			want: "U1>E1 O1>E2 U1>E3 O2>E4 ",
		},
		{
			name:    "the timing pass reaches O2 after O1's surplus is used up",
			players: append(team0("O1", "O2", "U1"), enemies("E1", "E2", "E3", "E4")...),
			gained:  map[string]uint32{"O1": 1, "O2": 1, "U1": 2},
			feed: []MatchUpdate{
				kill("O1", "E1", "U1"), kill("O1", "E2", ""), kill("O2", "E3", ""), kill("O2", "E4", "U1"),
			},
			want: "U1>E1 O1>E2 O2>E3 U1>E4 ",
		},
		{
			name:    "the fallback serves under-credited players by index, not by name",
			players: append(team0("O1", "O2", "Ub", "Ua"), enemies("E1", "E2")...),
			gained:  map[string]uint32{"Ub": 1, "Ua": 1},
			feed:    []MatchUpdate{kill("O1", "E1", ""), kill("O2", "E2", "")},
			want:    "Ub>E1 Ua>E2 ",
		},
		{
			name:    "the fallback takes from over-credited players by index, not by name",
			players: append(team0("Ob", "Oa", "U1", "U2"), enemies("E1", "E2")...),
			gained:  map[string]uint32{"U1": 1, "U2": 1},
			feed:    []MatchUpdate{kill("Oa", "E1", ""), kill("Ob", "E2", "")},
			want:    "U2>E1 U1>E2 ",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for run := 0; run < reconcileRuns; run++ {
				feed := append([]MatchUpdate(nil), tc.feed...)
				r := reconcileReader(tc.players, tc.gained, feed)
				r.reconcileKillsFromScoreboard()
				if got := feedKillers(r); got != tc.want {
					t.Fatalf("run %d: feed %q, want %q", run, got, tc.want)
				}
			}
		})
	}
}
