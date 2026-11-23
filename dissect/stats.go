package dissect

// ClutchDetail describes the situation of the last player alive on the winning team.
type ClutchDetail struct {
	OpponentsAlive int `json:"opponentsAlive"` // opponents alive when the clutch started
	// ElapsedSeconds is when the clutch started, in seconds since the start of the action phase.
	ElapsedSeconds float64 `json:"elapsedSeconds"`
	Kills          int     `json:"kills"`
}

type PlayerRoundStats struct {
	Username           string        `json:"username"`
	TeamIndex          int           `json:"-"`
	Score              int           `json:"score"`
	Operator           string        `json:"-"`
	Kills              int           `json:"kills"`
	Died               bool          `json:"died"`
	Assists            int           `json:"assists"`
	Headshots          int           `json:"headshots"`
	HeadshotPercentage float64       `json:"headshotPercentage"`
	OneVx              int           `json:"1vX,omitempty"`
	ClutchDetail       *ClutchDetail `json:"clutchDetail,omitempty"`
	// DamageTaken is the HP the player lost this round from any source, read from the health
	// component (Y10S4+). Nil, and omitted from JSON, when the round has no health data.
	DamageTaken *int `json:"damageTaken,omitempty"`
}

type PlayerMatchStats struct {
	Username           string  `json:"username"`
	TeamIndex          int     `json:"-"`
	Rounds             int     `json:"rounds"`
	Kills              int     `json:"kills"`
	Deaths             int     `json:"deaths"`
	Assists            int     `json:"assists"`
	Headshots          int     `json:"headshots"`
	HeadshotPercentage float64 `json:"headshotPercentage"`
	// DamageTaken is the sum over the rounds that have it; nil when no round does.
	DamageTaken *int `json:"damageTaken,omitempty"`
}

// OpeningKill returns the first player to kill. Teamkills do not count.
func (r *Reader) OpeningKill() MatchUpdate {
	for _, a := range r.MatchFeedback {
		if a.Type == Kill && !a.TeamKill {
			return a
		}
	}
	return MatchUpdate{}
}

// OpeningDeath returns the first player to die (KILL or DEATH activity).
func (r *Reader) OpeningDeath() MatchUpdate {
	for _, a := range r.MatchFeedback {
		if a.Type == Kill || a.Type == Death {
			return a
		}
	}
	return MatchUpdate{}
}

// Trades returns pairs of consecutive kills that are trades. Teamkills and other events
// are not considered.
func (r *Reader) Trades() [][]MatchUpdate {
	trades := make([][]MatchUpdate, 0)
	var previous = MatchUpdate{}
	for _, a := range r.MatchFeedback {
		if a.Type != Kill || a.TeamKill {
			continue
		}
		var samePlayers = (previous.Target == a.Username || previous.Username == a.Target)
		var withinThreshold = (a.ElapsedSeconds - previous.ElapsedSeconds) <= 3
		if samePlayers && withinThreshold {
			trades = append(trades, []MatchUpdate{previous, a})
		}
		previous = a
	}
	return trades
}

func (r *Reader) KillsAndDeaths() []MatchUpdate {
	MatchFeedback := make([]MatchUpdate, 0)
	for _, a := range r.MatchFeedback {
		if a.Type == Kill || a.Type == Death {
			MatchFeedback = append(MatchFeedback, a)
		}
	}
	return MatchFeedback
}

func (r *Reader) NumPlayers(team int) int {
	n := 0
	for _, p := range r.Header.Players {
		if p.TeamIndex == team {
			n++
		}
	}
	return n
}

// roundDecided reports whether a team won the round.
func (r *Reader) roundDecided() bool {
	return r.Header.Teams[0].Won || r.Header.Teams[1].Won
}

func (r *Reader) PlayerStats() []PlayerRoundStats {
	stats := make([]PlayerRoundStats, 0)
	index := make(map[string]int)
	winningTeamIndex := 0
	if r.Header.Teams[1].Won {
		winningTeamIndex = 1
	}
	for i, p := range r.Header.Players {
		// a player no scoreboard packet named has no row
		var scorePlayer ScoreboardPlayer
		if i < len(r.Scoreboard.Players) {
			scorePlayer = r.Scoreboard.Players[i]
		}
		stats = append(stats, PlayerRoundStats{
			Username:  p.Username,
			TeamIndex: p.TeamIndex,
			Operator:  p.OperatorName(),
			Assists:   int(scorePlayer.AssistsFromRound),
			Score:     int(scorePlayer.Score),
		})
		index[p.Username] = i
	}
	// teamIndexOf returns the team of username, or -1 if the player is unknown.
	teamIndexOf := func(username string) int {
		if i, ok := index[username]; ok {
			return stats[i].TeamIndex
		}
		return -1
	}
	for i, v := range r.damageTaken() {
		stats[i].DamageTaken = &v
	}
	lastDeath := -1
	for _, a := range r.MatchFeedback {
		i, known := index[a.Username]
		switch a.Type {
		case Kill:
			if known && !a.TeamKill {
				stats[i].Kills += 1
				if a.Headshot != nil && *a.Headshot {
					stats[i].Headshots += 1
				}
				stats[i].HeadshotPercentage = headshotPercentage(stats[i].Headshots, stats[i].Kills)
			}
			if t, ok := index[a.Target]; ok {
				stats[t].Died = true
				lastDeath = t
			}
		case Death:
			if known {
				stats[i].Died = true
				lastDeath = i
			}
		}
	}
	// Calculates 1vX
	winnersLeftAlive := make([]int, 0)
	lastDeathWasWinner := false
	for i, p := range r.Header.Players {
		if p.TeamIndex != winningTeamIndex {
			continue
		}
		if !stats[i].Died {
			winnersLeftAlive = append(winnersLeftAlive, i)
		}
		if i == lastDeath {
			lastDeathWasWinner = true
		}
	}
	nWinnersLeftAlive := len(winnersLeftAlive)
	lastWinnerStanding := -1
	if nWinnersLeftAlive == 1 {
		lastWinnerStanding = winnersLeftAlive[0]
	} else if nWinnersLeftAlive == 0 && lastDeathWasWinner {
		lastWinnerStanding = lastDeath
	}
	if lastWinnerStanding > -1 && r.roundDecided() { // a round without a result has no clutch
		username := stats[lastWinnerStanding].Username
		teamLeft := r.NumPlayers(winningTeamIndex)
		opponentsAlive := r.NumPlayers(1 - winningTeamIndex)
		counted := make(map[string]bool)
		oneVx := 0
		var clutch *ClutchDetail
		for _, a := range r.MatchFeedback {
			var victim string
			switch a.Type {
			case Kill:
				victim = a.Target
			case Death, PlayerLeave:
				victim = a.Username
			}
			if counted[victim] {
				// duplicate packet for a player who already died
				victim = ""
			} else if victim != "" {
				counted[victim] = true
			}
			if victim != "" {
				if teamIndexOf(victim) == winningTeamIndex {
					teamLeft--
				} else if teamIndexOf(victim) >= 0 {
					opponentsAlive--
				}
			}
			if teamLeft == 1 && clutch == nil {
				// opponents killed by this event were killed during the clutch
				clutch = &ClutchDetail{OpponentsAlive: opponentsAlive, ElapsedSeconds: a.ElapsedSeconds}
				if victim != "" && teamIndexOf(victim) != winningTeamIndex && teamIndexOf(victim) >= 0 {
					clutch.OpponentsAlive++
				}
			}
			if a.Username != username {
				continue
			}
			if a.Type == Kill && !a.TeamKill && teamLeft < 2 {
				oneVx++
				if clutch != nil {
					clutch.Kills++
				}
			}
		}
		for _, s := range stats {
			if s.TeamIndex != winningTeamIndex && !s.Died {
				oneVx++
			}
		}
		if clutch != nil {
			// the opponents faced when the clutch started, including any who later died without a killer
			oneVx = clutch.OpponentsAlive
		}
		stats[lastWinnerStanding].OneVx = oneVx
		if oneVx > 0 && clutch != nil {
			stats[lastWinnerStanding].ClutchDetail = clutch
		}
	}
	return stats
}

func (m *MatchReader) PlayerStats() []PlayerMatchStats {
	stats := make([]PlayerMatchStats, 0)
	index := make(map[string]int)
	for i, r := range m.rounds {
		if r == nil {
			continue
		}
		for _, p := range r.PlayerStats() {
			if len(stats) == 0 || stats[index[p.Username]].Username != p.Username {
				stats = append(stats, PlayerMatchStats{
					Username:  p.Username,
					TeamIndex: p.TeamIndex,
				})
				index[p.Username] = len(index)
			}
			i = index[p.Username]
			stats[i].Rounds += 1
			stats[i].Kills += p.Kills
			if p.Died {
				stats[i].Deaths += 1
			}
			stats[i].Assists += p.Assists
			stats[i].Headshots += p.Headshots
			stats[i].HeadshotPercentage = headshotPercentage(stats[i].Headshots, stats[i].Kills)
			if p.DamageTaken != nil {
				sum := *p.DamageTaken
				if stats[i].DamageTaken != nil {
					sum += *stats[i].DamageTaken
				}
				stats[i].DamageTaken = &sum
			}
		}
	}
	return stats
}

func headshotPercentage(headshots, kills int) float64 {
	if kills == 0 {
		return 0
	}
	return float64(headshots) / float64(kills) * 100
}
