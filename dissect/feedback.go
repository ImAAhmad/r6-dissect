package dissect

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/rs/zerolog/log"
)

type MatchUpdateType int

//go:generate stringer -type=MatchUpdateType
const (
	Kill MatchUpdateType = iota
	Death
	DefuserPlantStart
	DefuserPlantComplete
	DefuserDisableStart
	DefuserDisableComplete
	LocateObjective
	OperatorSwap
	Battleye
	PlayerLeave
	Other
)

type MatchUpdate struct {
	Type                   MatchUpdateType `json:"type"`
	Username               string          `json:"username,omitempty"`
	Target                 string          `json:"target,omitempty"`
	Headshot               *bool           `json:"headshot,omitempty"`
	Time                   string          `json:"time"`
	TimeInSeconds          float64         `json:"timeInSeconds"`
	Message                string          `json:"message,omitempty"`
	Operator               Operator        `json:"operator,omitempty"`
	usernameFromScoreboard string
}

func (i MatchUpdateType) MarshalJSON() (text []byte, err error) {
	return json.Marshal(stringerIntMarshal{
		Name: i.String(),
		ID:   int(i),
	})
}

func (i *MatchUpdateType) UnmarshalJSON(data []byte) (err error) {
	var x stringerIntMarshal
	if err = json.Unmarshal(data, &x); err != nil {
		return
	}
	*i = MatchUpdateType(x.ID)
	return
}

var activity2 = []byte{0x00, 0x00, 0x00, 0x22, 0xe3, 0x09, 0x00, 0x79}
var killIndicator = []byte{0x22, 0xd9, 0x13, 0x3c, 0xba}

func readMatchFeedback(r *Reader) error {
	if r.Header.CodeVersion >= Y9S1Update3 {
		if err := r.Skip(38); err != nil {
			return err
		}
	} else if r.Header.CodeVersion >= Y9S1 {
		if err := r.Skip(9); err != nil {
			return err
		}
		valid, err := r.Int()
		if err != nil {
			return err
		}
		if valid != 4 {
			return errors.New("match feedback failed valid check")
		}
		if err := r.Skip(24); err != nil {
			return err
		}
	} else {
		if err := r.Skip(1); err != nil {
			return err
		}
		if err := r.Seek(activity2); err != nil {
			return err
		}
	}
	size, err := r.Int()
	if err != nil {
		return err
	}
	if size == 0 { // kill or an unknown indicator at start of match
		killTrace, err := r.Bytes(5)
		if err != nil {
			return err
		}
		if !bytes.Equal(killTrace, killIndicator) {
			log.Debug().Hex("killTrace", killTrace).Send()
			return nil
		}
		username, err := r.String()
		if err != nil {
			return err
		}
		empty := len(username) == 0
		if empty {
			log.Debug().Str("warn", "kill username empty").Send()
		}
		if err = r.Skip(15); err != nil {
			return err
		}
		target, err := r.String()
		if err != nil {
			return err
		}
		log.Debug().Str("target", target).Msg("kill target parsed")
		if empty {
			if len(target) > 0 {
				u := MatchUpdate{
					Type:          Death,
					Username:      target,
					Time:          r.timeRaw,
					TimeInSeconds: r.time,
				}
				r.MatchFeedback = append(r.MatchFeedback, u)
				log.Debug().Interface("match_update", u).Send()
				log.Debug().Msg("kill username empty because of death")
			}
			return nil
		}
		u := MatchUpdate{
			Type:          Kill,
			Username:      username,
			Target:        target,
			Time:          r.timeRaw,
			TimeInSeconds: r.time,
		}
		if err = r.Skip(56); err != nil {
			return err
		}
		headshot, err := r.Int()
		if err != nil {
			return err
		}
		headshotPtr := new(bool)
		if headshot == 1 {
			*headshotPtr = true
		}
		u.Headshot = headshotPtr
		// Validate teams: killer and target must be on different teams
		killerIdx := r.PlayerIndexByUsername(u.Username)
		targetIdx := r.PlayerIndexByUsername(u.Target)
		if killerIdx >= 0 && targetIdx >= 0 {
			killerTeam := r.Header.Players[killerIdx].TeamIndex
			targetTeam := r.Header.Players[targetIdx].TeamIndex
			if killerTeam == targetTeam {
				log.Debug().
					Str("killer", u.Username).
					Str("target", u.Target).
					Int("team", killerTeam).
					Msg("kill filtered (same team)")
				return nil
			}
		}
		// Filter duplicate kills: if the target has already been killed in this round,
		// it's a duplicate (replays sometimes emit the same kill event multiple times,
		// especially after defuser plant when the timer resets).
		// Exception: overtime after defuser allows ONE "re-kill" per target (DBNO revive scenario).
		// We detect overtime by checking if time jumps up (timer reset after defuser plant).
		// 
		// Special case: kills that occur exactly at defuser plant time are "plant-boundary kills"
		// and are more likely to be duplicated by the replay system. For these, we require
		// the re-kill to be by a DIFFERENT killer to count as legitimate.
		inOvertime := false
		defuserPlantTime := float64(-1)
		for i := len(r.MatchFeedback) - 1; i >= 0; i-- {
			val := r.MatchFeedback[i]
			// Track defuser plant time
			if val.Type == DefuserPlantComplete {
				defuserPlantTime = val.TimeInSeconds
			}
			// Detect if we're in overtime: time has jumped up (timer reset after defuser)
			// Check ALL events for time jumps, not just kills
			if u.TimeInSeconds > val.TimeInSeconds+5 {
				inOvertime = true
			}
			// Only check kills/deaths for duplicate detection
			if val.Type != Kill && val.Type != Death {
				continue
			}
			// Check if this target has already been killed/died in this round
			targetAlreadyDead := (val.Type == Kill && val.Target == u.Target) ||
				(val.Type == Death && val.Username == u.Target)
			if targetAlreadyDead {
				sameKiller := val.Type == Kill && val.Username == u.Username
				// Check if original kill was at plant-boundary (at or within 1 second AFTER defuser plant)
				// Note: time counts DOWN, so val.TimeInSeconds <= defuserPlantTime means kill was at/after plant
				isPlantBoundaryKill := defuserPlantTime >= 0 && val.TimeInSeconds <= defuserPlantTime && val.TimeInSeconds >= defuserPlantTime-1
				// In overtime, allow re-kills with these conditions:
				// - If same killer: only allow if NOT a plant-boundary kill (those are likely duplicates)
				// - If different killer: always allow (DBNO finished by teammate, now actually killed)
				if inOvertime {
					if !sameKiller {
						log.Debug().
							Str("killer", u.Username).
							Str("target", u.Target).
							Str("original_killer", val.Username).
							Float64("existing_time", val.TimeInSeconds).
							Float64("new_time", u.TimeInSeconds).
							Msg("overtime re-kill allowed (different killer)")
						break
					}
					if !isPlantBoundaryKill {
						log.Debug().
							Str("killer", u.Username).
							Str("target", u.Target).
							Float64("existing_time", val.TimeInSeconds).
							Float64("new_time", u.TimeInSeconds).
							Float64("defuser_plant_time", defuserPlantTime).
							Msg("overtime re-kill allowed (same killer, not plant-boundary)")
						break
					}
				}
				log.Debug().
					Str("killer", u.Username).
					Str("target", u.Target).
					Float64("existing_time", val.TimeInSeconds).
					Float64("new_time", u.TimeInSeconds).
					Bool("plant_boundary", isPlantBoundaryKill).
					Msg("duplicate kill filtered (target already dead)")
				return nil
			}
		}
		// removing the elimination username for now
		if r.lastKillerFromScoreboard != username {
			u.usernameFromScoreboard = r.lastKillerFromScoreboard
		}
		r.MatchFeedback = append(r.MatchFeedback, u)
		log.Debug().Interface("match_update", u).Send()
		return nil
	}
	// TODO: Y9S1 may have removed or modified other match feedback options
	if r.Header.CodeVersion >= Y9S1 {
		return nil
	}
	b, err := r.Bytes(size)
	if err != nil {
		return err
	}
	msg := string(b)
	t := Other
	if strings.Contains(msg, "bombs") || strings.Contains(msg, "objective") {
		t = LocateObjective
	}
	if strings.Contains(msg, "BattlEye") {
		t = Battleye
	}
	if strings.Contains(msg, "left") {
		t = PlayerLeave
	}
	username := strings.Split(msg, " ")[0]
	if t == Other {
		username = ""
	} else {
		msg = ""
	}
	u := MatchUpdate{
		Type:          t,
		Username:      username,
		Target:        "",
		Time:          r.timeRaw,
		TimeInSeconds: r.time,
		Message:       msg,
	}
	r.MatchFeedback = append(r.MatchFeedback, u)
	log.Debug().Interface("match_update", u).Send()
	return nil
}
