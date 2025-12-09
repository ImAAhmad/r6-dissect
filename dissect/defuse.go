package dissect

import (
	"strings"

	"github.com/rs/zerolog/log"
)

// getTeamByRole returns the team index with the specified role (Attack or Defense)
func (r *Reader) getTeamByRole(role TeamRole) int {
	for i, team := range r.Header.Teams {
		if team.Role == role {
			return i
		}
	}
	return -1
}

// getAlivePlayersByTeam returns usernames of players who haven't died yet on a team
func (r *Reader) getAlivePlayersByTeam(teamIndex int) []string {
	var alive []string
	for _, p := range r.Header.Players {
		if p.TeamIndex == teamIndex {
			// Check if player has died
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

func readDefuserTimer(r *Reader) error {
	timer, err := r.String()
	if err != nil {
		return err
	}

	var playerIndex int = -1

	if r.Header.CodeVersion >= Y10S4 {
		// Y10S4 changed packet structure - player DissectID is no longer included
		// Try to infer from team roles: attackers plant, defenders disable
		var targetRole TeamRole
		if r.planted {
			targetRole = Defense // Defender is disabling
		} else {
			targetRole = Attack // Attacker is planting
		}
		
		teamIndex := r.getTeamByRole(targetRole)
		if teamIndex >= 0 {
			alive := r.getAlivePlayersByTeam(teamIndex)
			if len(alive) == 1 {
				// Only one player alive on that team - must be them
				for i, p := range r.Header.Players {
					if p.Username == alive[0] {
						playerIndex = i
						break
					}
				}
			}
		}
	} else {
		if err = r.Skip(34); err != nil {
			return err
		}
		id, err := r.Bytes(4)
		if err != nil {
			return err
		}
		playerIndex = r.PlayerIndexByID(id)
	}

	if playerIndex > -1 && r.lastDefuserPlayerIndex != playerIndex {
		a := DefuserPlantStart
		if r.planted {
			a = DefuserDisableStart
		}
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

	// TODO: 0.00 can be present even if defuser was not disabled.
	if !strings.HasPrefix(timer, "0.00") {
		return nil
	}
	a := DefuserDisableComplete
	if !r.planted {
		a = DefuserPlantComplete
		r.planted = true
	}
	
	username := ""
	if r.lastDefuserPlayerIndex >= 0 && r.lastDefuserPlayerIndex < len(r.Header.Players) {
		username = r.Header.Players[r.lastDefuserPlayerIndex].Username
	}
	
	u := MatchUpdate{
		Type:          a,
		Username:      username,
		Time:          r.timeRaw,
		TimeInSeconds: r.time,
	}
	r.MatchFeedback = append(r.MatchFeedback, u)
	log.Debug().Interface("match_update", u).Send()
	return nil
}
