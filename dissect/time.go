package dissect

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// RoundPhase identifies which countdown the in-game timer is showing.
type RoundPhase string

const (
	PrepPhase      RoundPhase = "Prep"
	ActionPhase    RoundPhase = "Action"
	PostPlantPhase RoundPhase = "PostPlant"
)

// the timer restarts its countdown at every phase change (prep -> action -> defuser planted),
// so the raw timer value alone is ambiguous. We track the phase and a monotonic clock instead.
type roundClock struct {
	phase        RoundPhase
	phaseStart   float64 // timer value when the current phase started
	phaseElapsed float64 // elapsed seconds when the current phase started
	started      bool
	tick         float64 // last timer value that counted as a tick (differs from r.time after a round-end reset)
	// set after a plant until the timer shows the defuser countdown
	awaitCountdown bool
	// the last action-phase tick, and whether the clock left the action phase through a
	// timer jump (rather than a completed plant), see stampPlantComplete
	last         actionTick
	jumpedToPost bool
}

type actionTick struct {
	valid   bool
	time    float64
	timeRaw string
	elapsed float64
}

// prepPhaseMax is the longest timer value we consider to be a preparation phase countdown.
const prepPhaseMax = 60

func (r *Reader) updateClock(t float64) {
	c := &r.clock
	// when a round ends early the game sends 0 right after a higher value. That is a reset,
	// not a tick (in the post-plant phase it is the defuser timer), so events after it
	// belong to the last real tick. Natural expiry (1 -> 0) still counts.
	if c.started && t == 0 && c.tick > 1 {
		r.time = t // the reported time stays the raw timer value
		return
	}
	if c.awaitCountdown {
		if t != c.tick {
			c.phaseStart = t
			c.awaitCountdown = false
		}
		r.time = t
		c.tick = t
		return
	}
	// a lone 0 before the first countdown is a placeholder, not a countdown
	if !c.started && t == 0 {
		r.time = t
		c.tick = t
		return
	}
	// a new countdown starts with the first timer value or whenever the timer jumps up
	if !c.started || t > c.tick+1 {
		switch {
		case c.phase == ActionPhase || c.phase == PostPlantPhase:
			if c.phase == ActionPhase {
				c.jumpedToPost = true
			}
			// the only countdown after the action phase is the defuser timer
			c.phaseElapsed = r.elapsed()
			c.phase = PostPlantPhase
		case t <= prepPhaseMax:
			c.phase = PrepPhase
		default:
			c.phase = ActionPhase
			c.phaseElapsed = 0
		}
		c.phaseStart = t
		c.started = true
	}
	r.time = t
	c.tick = t
}

// startPostPlant switches the clock to the post-plant phase when the defuser was planted.
// The timer then shows the defuser countdown, which is not always an upward jump
// (e.g. when the defuser is planted with more than 45 seconds left).
func (r *Reader) startPostPlant() {
	c := &r.clock
	if !c.started || c.phase != ActionPhase {
		return
	}
	c.phaseElapsed = r.elapsed()
	c.phase = PostPlantPhase
	c.phaseStart = c.tick
	c.jumpedToPost = false
	c.awaitCountdown = true
}

// elapsed returns the seconds since the start of the action phase.
// The value is negative during the preparation phase.
func (r *Reader) elapsed() float64 {
	if r.clock.phase == PrepPhase {
		return -r.clock.tick
	}
	return r.clock.phaseElapsed + r.clock.phaseStart - r.clock.tick
}

// stamp sets the round phase and monotonic time of u to the current clock.
func (r *Reader) stamp(u *MatchUpdate) {
	if !r.clock.started {
		return
	}
	u.Phase = r.clock.phase
	u.ElapsedSeconds = r.elapsed()
}

// noteTick remembers the last action-phase tick. It runs after the raw time is set.
func (r *Reader) noteTick() {
	if r.clock.started && r.clock.phase == ActionPhase && r.clock.tick == r.time {
		r.clock.last = actionTick{true, r.time, r.timeRaw, r.elapsed()}
	}
}

// stampPlantComplete stamps a plant completion. The timer can jump up to the defuser
// countdown before the packet that completes the plant is read; the plant still happened
// at the last action-phase tick.
func (r *Reader) stampPlantComplete(u *MatchUpdate) {
	c := &r.clock
	if c.jumpedToPost && c.phase == PostPlantPhase && c.last.valid {
		u.Phase = ActionPhase
		u.Time = c.last.timeRaw
		u.TimeInSeconds = c.last.time
		u.ElapsedSeconds = c.last.elapsed
		return
	}
	r.stamp(u)
}

// backfillLeadingUpdates gives events recorded before the clock started the phase and
// elapsed time of the first stamped event, so elapsedSeconds never decreases.
func (r *Reader) backfillLeadingUpdates() {
	first := -1
	for i := range r.MatchFeedback {
		if r.MatchFeedback[i].Phase != "" {
			first = i
			break
		}
	}
	for i := 0; i < first; i++ {
		r.MatchFeedback[i].Phase = r.MatchFeedback[first].Phase
		r.MatchFeedback[i].ElapsedSeconds = r.MatchFeedback[first].ElapsedSeconds
	}
}

// relabelPostPlantWithoutPlant labels events Action when the round had no plant. Any upward
// timer jump in the action phase is read as the defuser countdown, but when the action timer
// runs out without a plant, the next countdown is the end of the round.
func (r *Reader) relabelPostPlantWithoutPlant() {
	for _, u := range r.MatchFeedback {
		if u.Type == DefuserPlantComplete {
			return
		}
	}
	for i := range r.MatchFeedback {
		if r.MatchFeedback[i].Phase == PostPlantPhase {
			r.MatchFeedback[i].Phase = ActionPhase
		}
	}
}

func readTime(r *Reader) error {
	time, err := r.Uint32()
	if err != nil {
		return err
	}
	r.updateClock(float64(time))
	r.timeRaw = fmt.Sprintf("%d:%02d", time/60, time%60)
	r.noteTick()
	return nil
}

func readY7Time(r *Reader) error {
	time, err := r.String()
	if err != nil {
		return err
	}
	parts := strings.Split(time, ":")
	if len(parts) == 1 {
		seconds, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return err
		}
		r.updateClock(seconds)
		r.timeRaw = parts[0]
		r.noteTick()
		return nil
	}
	minutes, err := strconv.Atoi(parts[0])
	if err != nil {
		return err
	}
	seconds, err := strconv.Atoi(parts[1])
	if err != nil {
		return err
	}
	r.updateClock(float64((minutes * 60) + seconds))
	r.timeRaw = time
	r.noteTick()
	return nil
}

// headerWinner returns the index of the only team whose header score rose this
// round, or -1 when neither score (or both) rose: the round was abandoned,
// remade, or recorded without its end.
func (r *Reader) headerWinner() int {
	winner := -1
	for i, team := range r.Header.Teams {
		if team.StartingScore < team.Score {
			if winner >= 0 {
				return -1
			}
			winner = i
		}
	}
	return winner
}

func (r *Reader) roundEnd() {
	log.Debug().Msg("round_end")

	planter := -1
	disabler := -1
	planted := false
	dead := make(map[string]bool)
	deaths := make(map[int]int)
	sizes := make(map[int]int)
	roles := make(map[int]TeamRole)

	for _, p := range r.Header.Players {
		sizes[p.TeamIndex] += 1
		roles[p.TeamIndex] = r.Header.Teams[p.TeamIndex].Role
	}

	decided := true
	if r.Header.CodeVersion >= Y9S4 {
		winner := r.headerWinner()
		decided = winner >= 0
		r.Header.Teams[0].Won = winner == 0
		r.Header.Teams[1].Won = winner == 1
	}

	if r.Header.CodeVersion >= Y10S4 {
		r.reconcileKillsFromScoreboard()
	}

	for i := range r.MatchFeedback {
		u := &r.MatchFeedback[i]
		switch u.Type {
		case Kill:
			if ti := r.PlayerIndexByUsername(u.Target); ti >= 0 && !dead[u.Target] {
				dead[u.Target] = true
				deaths[r.Header.Players[ti].TeamIndex]++
			}
		case Death:
			if ti := r.PlayerIndexByUsername(u.Username); ti >= 0 && !dead[u.Username] {
				dead[u.Username] = true
				deaths[r.Header.Players[ti].TeamIndex]++
			}
		case DefuserPlantComplete:
			// the planter may be unknown, the defuser was planted regardless
			planted = true
			planter = r.PlayerIndexByUsername(u.Username)
		case DefuserDisableStart:
			disabler = r.PlayerIndexByUsername(u.Username)
		case DefuserDisableComplete:
			if r.Header.CodeVersion >= Y9S4 {
				if !decided {
					continue
				}
				winningTeam := 0
				if r.Header.Teams[1].Won {
					winningTeam = 1
				}
				if roles[winningTeam] != Defense {
					log.Debug().Msg("warn: defuser disabled but attack won the round")
					continue
				}
				r.Header.Teams[winningTeam].WinCondition = DisabledDefuser
			} else {
				playerIdx := r.PlayerIndexByUsername(u.Username)
				if playerIdx < 0 || playerIdx >= len(r.Header.Players) {
					log.Debug().Msg("warn: defuser disable player not found")
					return
				}
				ti := r.Header.Players[playerIdx].TeamIndex
				r.Header.Teams[ti].Won = true
				r.Header.Teams[ti].WinCondition = DisabledDefuser
			}
			return
		}
	}

	// since Y9S4, the header tells us who won. Only the win condition needs to be determined.
	if r.Header.CodeVersion >= Y9S4 {
		if !decided {
			log.Debug().Msg("round has no result: neither header score moved")
			return
		}
		winner := 0
		if r.Header.Teams[1].Won {
			winner = 1
		}
		loser := winner ^ 1
		switch {
		case planted && roles[winner] == Defense:
			// once the defuser is planted, defense can only win by disabling it
			username := ""
			if disabler >= 0 && disabler < len(r.Header.Players) {
				username = r.Header.Players[disabler].Username
			}
			u := MatchUpdate{
				Type:          DefuserDisableComplete,
				Username:      username,
				Time:          r.timeRaw,
				TimeInSeconds: r.time,
			}
			r.MatchFeedback = append(r.MatchFeedback, u)
			log.Debug().Interface("match_update", u).Msg("inferred DefuserDisableComplete")
			r.Header.Teams[winner].WinCondition = DisabledDefuser
		case planted:
			r.Header.Teams[winner].WinCondition = DefusedBomb
		case sizes[loser] > 0 && deaths[loser] >= sizes[loser]:
			r.Header.Teams[winner].WinCondition = KilledOpponents
		case roles[winner] == Defense && r.Header.GameMode != Hostage:
			r.Header.Teams[winner].WinCondition = Time
		}
		return
	}

	if planted {
		// the planter's team plants the defuser; without a known planter it is the attacking team
		team := 0
		if planter > -1 {
			team = r.Header.Players[planter].TeamIndex
		} else if roles[1] == Attack {
			team = 1
		}
		r.Header.Teams[team].Won = true
		r.Header.Teams[team].WinCondition = DefusedBomb
		return
	}

	if deaths[0] == sizes[0] {
		if planter > -1 && roles[0] == Attack { // ignore attackers killed post-plant
			return
		}
		r.Header.Teams[1].Won = true
		r.Header.Teams[1].WinCondition = KilledOpponents
		return
	}
	if deaths[1] == sizes[1] {
		if planter > -1 && roles[1] == Attack { // ignore attackers killed post-plant
			return
		}
		r.Header.Teams[0].Won = true
		r.Header.Teams[0].WinCondition = KilledOpponents
		return
	}

	i := 0
	if roles[1] == Defense {
		i = 1
	}

	r.Header.Teams[i].Won = true
	r.Header.Teams[i].WinCondition = Time
}
