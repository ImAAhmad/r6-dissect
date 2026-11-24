package dissect

import "testing"

func roundEndPlayers() []Player {
	return []Player{
		{Username: "a1", TeamIndex: 0}, {Username: "a2", TeamIndex: 0},
		{Username: "d1", TeamIndex: 1}, {Username: "d2", TeamIndex: 1},
	}
}

// plant with unresolved planter (username "") must still count as a plant.
func TestRoundEnd_UnattributedPlantDefenseWin(t *testing.T) {
	r := &Reader{Header: Header{CodeVersion: Y10S4, GameMode: Bomb,
		Teams:   [2]Team{{Role: Attack, StartingScore: 0, Score: 0}, {Role: Defense, StartingScore: 0, Score: 1}},
		Players: roundEndPlayers()},
		MatchFeedback: []MatchUpdate{{Type: DefuserPlantComplete, Username: ""}},
	}
	r.roundEnd()
	if got := r.Header.Teams[1].WinCondition; got != DisabledDefuser {
		t.Fatalf("defense won after a plant: want DisabledDefuser, got %q", got)
	}
}

// duplicate Death events for the same player must not count twice.
func TestRoundEnd_DuplicateDeathCountedOnce(t *testing.T) {
	r := &Reader{Header: Header{CodeVersion: Y10S4, GameMode: Bomb,
		Teams:   [2]Team{{Role: Attack, StartingScore: 0, Score: 0}, {Role: Defense, StartingScore: 0, Score: 1}},
		Players: roundEndPlayers()},
		MatchFeedback: []MatchUpdate{
			{Type: Death, Username: "a1", ElapsedSeconds: 25},
			{Type: Death, Username: "a1", ElapsedSeconds: 26},
		},
	}
	r.roundEnd()
	if got := r.Header.Teams[1].WinCondition; got == KilledOpponents {
		t.Fatalf("only 1 of 2 attackers died: got KilledOpponents")
	}
}

// neither header score moved: the round has no winner and no win condition.
func TestRoundEnd_NeitherScoreMovedHasNoWinner(t *testing.T) {
	r := &Reader{Header: Header{CodeVersion: Y10S4, GameMode: Bomb,
		Teams:   [2]Team{{Role: Attack, StartingScore: 2, Score: 2}, {Role: Defense, StartingScore: 2, Score: 2}},
		Players: roundEndPlayers()},
	}
	r.roundEnd()
	for i, team := range r.Header.Teams {
		if team.Won || team.WinCondition != "" {
			t.Fatalf("team %d: want no result, got won=%v condition=%q", i, team.Won, team.WinCondition)
		}
	}
}

// a decided round keeps its winner: team 0 scored, then team 1 scored.
func TestRoundEnd_ScoredTeamWins(t *testing.T) {
	for winner := 0; winner < 2; winner++ {
		r := &Reader{Header: Header{CodeVersion: Y10S4, GameMode: Bomb,
			Teams:   [2]Team{{Role: Defense, StartingScore: 1, Score: 1}, {Role: Defense, StartingScore: 1, Score: 1}},
			Players: roundEndPlayers()},
		}
		r.Header.Teams[winner].Score++
		r.roundEnd()
		if !r.Header.Teams[winner].Won || r.Header.Teams[winner^1].Won {
			t.Fatalf("team %d scored: want it to win alone, got %v / %v", winner, r.Header.Teams[0].Won, r.Header.Teams[1].Won)
		}
		if got := r.Header.Teams[winner].WinCondition; got != Time {
			t.Fatalf("team %d: want Time, got %q", winner, got)
		}
	}
}

// a plant and a disable in a round without a result: no condition, no inferred disable.
func TestRoundEnd_UndecidedRoundKeepsFeedAsIs(t *testing.T) {
	feed := []MatchUpdate{{Type: DefuserPlantComplete, Username: "a1"}, {Type: DefuserDisableComplete, Username: "d1"}}
	r := &Reader{Header: Header{CodeVersion: Y10S4, GameMode: Bomb,
		Teams:   [2]Team{{Role: Attack, StartingScore: 3, Score: 3}, {Role: Defense, StartingScore: 1, Score: 1}},
		Players: roundEndPlayers()},
		MatchFeedback: append([]MatchUpdate(nil), feed...),
	}
	r.roundEnd()
	if len(r.MatchFeedback) != len(feed) {
		t.Fatalf("feed changed: want %d events, got %d", len(feed), len(r.MatchFeedback))
	}
	for i, team := range r.Header.Teams {
		if team.Won || team.WinCondition != "" {
			t.Fatalf("team %d: want no result, got won=%v condition=%q", i, team.Won, team.WinCondition)
		}
	}
}

// a plant with no disable event in a round without a result: no inferred disable.
func TestRoundEnd_UndecidedPlantInfersNothing(t *testing.T) {
	r := &Reader{Header: Header{CodeVersion: Y10S4, GameMode: Bomb,
		Teams:   [2]Team{{Role: Attack, StartingScore: 3, Score: 3}, {Role: Defense, StartingScore: 1, Score: 1}},
		Players: roundEndPlayers()},
		MatchFeedback: []MatchUpdate{{Type: DefuserPlantComplete, Username: "a1"}},
	}
	r.roundEnd()
	if len(r.MatchFeedback) != 1 || r.Header.Teams[0].WinCondition != "" || r.Header.Teams[1].WinCondition != "" {
		t.Fatalf("want an untouched feed and no condition, got %d events, %q / %q",
			len(r.MatchFeedback), r.Header.Teams[0].WinCondition, r.Header.Teams[1].WinCondition)
	}
}

func TestHeaderWinner(t *testing.T) {
	cases := []struct{ s0, e0, s1, e1, want int }{
		{0, 1, 0, 0, 0}, {4, 4, 2, 3, 1}, {2, 2, 2, 2, -1}, {1, 2, 1, 2, -1}, {0, 0, 0, 0, -1},
	}
	for _, c := range cases {
		r := &Reader{Header: Header{Teams: [2]Team{{StartingScore: c.s0, Score: c.e0}, {StartingScore: c.s1, Score: c.e1}}}}
		if got := r.headerWinner(); got != c.want {
			t.Errorf("%d->%d | %d->%d: want %d, got %d", c.s0, c.e0, c.s1, c.e1, c.want, got)
		}
	}
}

// both sides are two deep; team 1 loses one, so team 1's last player would be a clutch if the round were decided.
func clutchReader(team1Score int) *Reader {
	return &Reader{Header: Header{CodeVersion: Y10S4, GameMode: Bomb,
		Teams:   [2]Team{{Role: Attack, StartingScore: 2, Score: 2}, {Role: Defense, StartingScore: 2, Score: team1Score}},
		Players: []Player{{Username: "a1", TeamIndex: 0}, {Username: "a2", TeamIndex: 0}, {Username: "d1", TeamIndex: 1}, {Username: "d2", TeamIndex: 1}}},
		MatchFeedback: []MatchUpdate{{Type: Death, Username: "d2", ElapsedSeconds: 30}},
	}
}

func oneVxTotal(r *Reader) (n int) {
	for _, s := range r.PlayerStats() {
		n += s.OneVx
		if s.OneVx == 0 && s.ClutchDetail != nil {
			n++
		}
	}
	return n
}

// a round without a result credits no clutch to either team; the decided twin still does.
func TestPlayerStats_UndecidedRoundHasNoClutch(t *testing.T) {
	decided := clutchReader(3)
	decided.roundEnd()
	if oneVxTotal(decided) == 0 {
		t.Fatal("control: a decided round with a last player standing should credit a 1vX")
	}
	r := clutchReader(2)
	r.roundEnd()
	for _, s := range r.PlayerStats() {
		if s.OneVx != 0 || s.ClutchDetail != nil {
			t.Fatalf("%s: want no clutch in a round without a result, got 1vX=%d detail=%v", s.Username, s.OneVx, s.ClutchDetail)
		}
	}
}

func TestRoundDecided(t *testing.T) {
	r := &Reader{}
	if r.roundDecided() {
		t.Fatal("no team won: want false")
	}
	r.Header.Teams[1].Won = true
	if !r.roundDecided() {
		t.Fatal("team 1 won: want true")
	}
}

// the disable branch of a round without a result must not set a condition, whichever role team 0 has.
func TestRoundEnd_UndecidedDisableIgnoredForDefenseTeam0(t *testing.T) {
	r := &Reader{Header: Header{CodeVersion: Y10S4, GameMode: Bomb,
		Teams:   [2]Team{{Role: Defense, StartingScore: 1, Score: 1}, {Role: Attack, StartingScore: 3, Score: 3}},
		Players: roundEndPlayers()},
		MatchFeedback: []MatchUpdate{{Type: DefuserPlantComplete, Username: "d1"}, {Type: DefuserDisableComplete, Username: "a1"}},
	}
	r.roundEnd()
	for i, team := range r.Header.Teams {
		if team.Won || team.WinCondition != "" {
			t.Fatalf("team %d: want no result, got won=%v condition=%q", i, team.Won, team.WinCondition)
		}
	}
}

// the exits below the undecided guard still run for a decided round.
func TestRoundEnd_DecidedConditions(t *testing.T) {
	cases := []struct {
		name string
		feed []MatchUpdate
		want WinCondition
	}{
		{"wipe", []MatchUpdate{{Type: Death, Username: "a1"}, {Type: Death, Username: "a2"}}, KilledOpponents},
		{"plant, attack wins", []MatchUpdate{{Type: DefuserPlantComplete, Username: "a1"}}, DefusedBomb},
	}
	for _, c := range cases {
		r := &Reader{Header: Header{CodeVersion: Y10S4, GameMode: Bomb,
			Teams:   [2]Team{{Role: Attack, StartingScore: 0, Score: 0}, {Role: Defense, StartingScore: 0, Score: 1}},
			Players: roundEndPlayers()},
			MatchFeedback: c.feed,
		}
		if c.name == "plant, attack wins" {
			r.Header.Teams[0].Score, r.Header.Teams[1].Score = 1, 0
		}
		r.roundEnd()
		winner := 1
		if c.name == "plant, attack wins" {
			winner = 0
		}
		if got := r.Header.Teams[winner].WinCondition; got != c.want {
			t.Errorf("%s: want %q, got %q", c.name, c.want, got)
		}
	}
}

// PlayerStats alone, with no team marked as winner: team 0's last player alive is not a clutch.
func TestPlayerStats_NoWinnerNoClutchForTeam0(t *testing.T) {
	r := clutchReader(2)
	r.MatchFeedback = []MatchUpdate{{Type: Death, Username: "a2", ElapsedSeconds: 30}}
	for _, s := range r.PlayerStats() {
		if s.OneVx != 0 || s.ClutchDetail != nil {
			t.Fatalf("%s: want no clutch, got 1vX=%d detail=%v", s.Username, s.OneVx, s.ClutchDetail)
		}
	}
	r.Header.Teams[0].Won = true
	if oneVxTotal(r) == 0 {
		t.Fatal("control: with team 0 marked as winner the same feed should credit a 1vX")
	}
}
