package dissect

import "testing"

// the timer jumps to 0 when a round ends early; events after it happened at the last real tick.
func TestClock_RoundEndZeroIsNotElapsedTime(t *testing.T) {
	r := &Reader{}
	for v := 180.0; v >= 22; v-- {
		r.updateClock(v)
	}
	r.updateClock(0)
	u := MatchUpdate{}
	r.stamp(&u)
	if u.ElapsedSeconds != 158 {
		t.Fatalf("want elapsedSeconds 158 (last tick 0:22), got %v", u.ElapsedSeconds)
	}
}

// natural expiry (1 -> 0) is a real tick.
func TestClock_NaturalExpiryCounts(t *testing.T) {
	r := &Reader{}
	for v := 180.0; v >= 1; v-- {
		r.updateClock(v)
	}
	r.updateClock(0)
	u := MatchUpdate{}
	r.stamp(&u)
	if u.ElapsedSeconds != 180 {
		t.Fatalf("want elapsedSeconds 180 (timer 0:00), got %v", u.ElapsedSeconds)
	}
}

// after the action timer runs out without a plant, the next countdown is not a defuser timer.
func TestRelabelPostPlant_NoPlant(t *testing.T) {
	r := &Reader{MatchFeedback: []MatchUpdate{
		{Type: Kill, Phase: ActionPhase, ElapsedSeconds: 100},
		{Type: PlayerLeave, Phase: PostPlantPhase, ElapsedSeconds: 183},
	}}
	r.relabelPostPlantWithoutPlant()
	for _, u := range r.MatchFeedback {
		if u.Phase != ActionPhase {
			t.Fatalf("want Action without a plant, got %q", u.Phase)
		}
	}
	if r.MatchFeedback[1].ElapsedSeconds != 183 {
		t.Fatalf("elapsed changed: %v", r.MatchFeedback[1].ElapsedSeconds)
	}
}

func TestRelabelPostPlant_Planted(t *testing.T) {
	r := &Reader{MatchFeedback: []MatchUpdate{
		{Type: DefuserPlantComplete, Phase: ActionPhase, ElapsedSeconds: 100},
		{Type: Kill, Phase: PostPlantPhase, ElapsedSeconds: 120},
	}}
	r.relabelPostPlantWithoutPlant()
	if r.MatchFeedback[1].Phase != PostPlantPhase {
		t.Fatalf("want PostPlant after a plant, got %q", r.MatchFeedback[1].Phase)
	}
}
