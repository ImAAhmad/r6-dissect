package dissect

import "testing"

func feedTypes(r *Reader) []MatchUpdateType {
	types := make([]MatchUpdateType, len(r.MatchFeedback))
	for i, u := range r.MatchFeedback {
		types[i] = u.Type
	}
	return types
}

func at(t MatchUpdateType, elapsed float64) MatchUpdate {
	return MatchUpdate{Type: t, Phase: ActionPhase, ElapsedSeconds: elapsed}
}

// a stop noticed after a later event belongs before it.
func TestInsertByElapsed_LateStop(t *testing.T) {
	r := &Reader{pendingDefuserPlantIdx: -1, MatchFeedback: []MatchUpdate{at(Kill, 10), at(Kill, 20)}}
	r.insertByElapsed(at(DefuserPlantStop, 15))
	got := feedTypes(r)
	want := []MatchUpdateType{Kill, DefuserPlantStop, Kill}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("want %v, got %v", want, got)
	}
}

// an event with the same time as existing ones goes after them.
func TestInsertByElapsed_Ties(t *testing.T) {
	r := &Reader{pendingDefuserPlantIdx: -1, MatchFeedback: []MatchUpdate{at(Kill, 10)}}
	r.insertByElapsed(at(DefuserPlantStop, 10))
	if got := feedTypes(r); len(got) != 2 || got[1] != DefuserPlantStop {
		t.Fatalf("want the inserted event last, got %v", got)
	}
}

// the pending defuser completion must still be found after an insert before it.
func TestInsertByElapsed_ShiftsPendingDefuser(t *testing.T) {
	plant := at(DefuserPlantComplete, 12)
	r := &Reader{pendingDefuserPlantIdx: 1, MatchFeedback: []MatchUpdate{at(Kill, 10), plant, at(Kill, 20)}}
	r.insertByElapsed(at(DefuserPlantStop, 5))
	if r.pendingDefuserPlantIdx != 2 || r.MatchFeedback[2].Type != DefuserPlantComplete {
		t.Fatalf("pendingDefuserPlantIdx %d, feed %v", r.pendingDefuserPlantIdx, feedTypes(r))
	}
}

// events without a phase (read before the clock started) stay first.
func TestInsertByElapsed_PhaseLessPrefix(t *testing.T) {
	r := &Reader{pendingDefuserPlantIdx: -1, MatchFeedback: []MatchUpdate{{Type: Other}, at(Kill, 10)}}
	r.insertByElapsed(at(DefuserPlantStop, 1))
	if got := feedTypes(r); len(got) != 3 || got[1] != DefuserPlantStop {
		t.Fatalf("want the inserted event at index 1, got %v", got)
	}
}

// an insert at the pending defuser's index pushes the pending index along with it.
func TestInsertByElapsed_InsertAtPendingIndex(t *testing.T) {
	r := &Reader{pendingDefuserPlantIdx: 1, MatchFeedback: []MatchUpdate{at(Kill, 10), at(DefuserPlantComplete, 12), at(Kill, 20)}}
	r.insertByElapsed(at(DefuserPlantStop, 11))
	if r.pendingDefuserPlantIdx != 2 || r.MatchFeedback[2].Type != DefuserPlantComplete {
		t.Fatalf("want pending index 2 on DefuserPlantComplete, got %d, %v", r.pendingDefuserPlantIdx, r.MatchFeedback)
	}
}
