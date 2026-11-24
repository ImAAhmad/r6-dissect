package dissect

import "testing"

// healthReplay builds the bytes of a health link record and a health state record for one player.
func healthReplay(slot, health []byte, state byte) (b []byte, linkOffset, stateOffset int) {
	// link: 1B <slot> 00000000 41 54 DC C4 <health> 00000000
	b = append(b, 0x1B)
	b = append(b, slot...)
	b = append(b, 0, 0, 0, 0)
	b = append(b, healthLinkIndicator...)
	linkOffset = len(b)
	b = append(b, health...)
	b = append(b, 0, 0, 0, 0)
	// 23 <health> 00000000 25 26 76 C9 04 <0> 22 E7 88 F6 A5 04 <state>
	b = append(b, 0x23)
	b = append(b, health...)
	b = append(b, 0, 0, 0, 0, 0x25, 0x26, 0x76, 0xC9, 0x04, 0, 0, 0, 0, 0x22)
	b = append(b, healthStateIndicator...)
	stateOffset = len(b)
	b = append(b, state, 0, 0, 0)
	b = append(b, make([]byte, 16)...)
	return b, linkOffset, stateOffset
}

func newHealthReader(b []byte, slot []byte) *Reader {
	return &Reader{
		b:                      b,
		pendingDefuserPlantIdx: -1,
		healthToSlot:           make(map[[4]byte][4]byte),
		healthState:            make(map[int]uint32),
		Header:                 Header{Players: []Player{{Username: "downed", slotID: slot}}},
	}
}

func TestHealthStateDown(t *testing.T) {
	slot := []byte{0x30, 0xbb, 0x2b, 0xf0}
	b, linkOffset, stateOffset := healthReplay(slot, []byte{0x38, 0xbb, 0x2b, 0xf0}, healthStateDowned)
	r := newHealthReader(b, slot)
	r.clock.started = true
	r.clock.phase = ActionPhase
	r.offset = linkOffset
	if err := readHealthLink(r); err != nil {
		t.Fatal(err)
	}
	r.offset = stateOffset
	if err := readHealthState(r); err != nil {
		t.Fatal(err)
	}
	if len(r.MatchFeedback) != 1 || r.MatchFeedback[0].Type != DBNO || r.MatchFeedback[0].Target != "downed" ||
		r.MatchFeedback[0].healthTransition == 0 || r.MatchFeedback[0].Phase != ActionPhase {
		t.Fatalf("want one stamped DBNO of 'downed', got %+v", r.MatchFeedback)
	}
	// the same state again is not a new down
	if err := readHealthState(r); err != nil {
		t.Fatal(err)
	}
	if len(r.MatchFeedback) != 1 {
		t.Fatalf("repeated state must be ignored, got %d events", len(r.MatchFeedback))
	}
}

func TestHealthStateIgnoredBeforeClock(t *testing.T) {
	slot := []byte{0x30, 0xbb, 0x2b, 0xf0}
	b, linkOffset, stateOffset := healthReplay(slot, []byte{0x38, 0xbb, 0x2b, 0xf0}, healthStateDowned)
	r := newHealthReader(b, slot)
	r.offset = linkOffset
	if err := readHealthLink(r); err != nil {
		t.Fatal(err)
	}
	r.offset = stateOffset
	if err := readHealthState(r); err != nil {
		t.Fatal(err)
	}
	if len(r.MatchFeedback) != 0 {
		t.Fatalf("states before the clock started must be ignored, got %+v", r.MatchFeedback)
	}
}

// resolveReader has a1, a2 on team 0 and d1, d2 on team 1.
func resolveReader(transitions []healthTransition, feed ...MatchUpdate) *Reader {
	return &Reader{
		pendingDefuserPlantIdx: -1,
		healthTransitions:      transitions,
		MatchFeedback:          feed,
		Header: Header{Players: []Player{
			{Username: "a1", TeamIndex: 0}, {Username: "a2", TeamIndex: 0},
			{Username: "d1", TeamIndex: 1}, {Username: "d2", TeamIndex: 1},
		}},
	}
}

func tr(player int, state uint32, elapsed float64) healthTransition {
	return healthTransition{player: player, state: state, elapsed: elapsed, phase: ActionPhase}
}

// down is a DBNO read from healthTransitions[transition].
func down(target string, elapsed float64, transition int) MatchUpdate {
	return MatchUpdate{Type: DBNO, Target: target, Phase: ActionPhase, ElapsedSeconds: elapsed, healthTransition: transition + 1}
}

func findType(r *Reader, typ MatchUpdateType) []MatchUpdate {
	var found []MatchUpdate
	for _, u := range r.MatchFeedback {
		if u.Type == typ {
			found = append(found, u)
		}
	}
	return found
}

func TestResolveHealth_Finished(t *testing.T) {
	r := resolveReader([]healthTransition{tr(0, healthStateDowned, 10), tr(0, healthStateDead, 15)},
		down("a1", 10, 0), kill("d1", "a1", 15))
	r.resolveHealthEvents()
	if d := findType(r, DBNO); len(d) != 1 || d[0].Username != "d1" || d[0].TeamKill {
		t.Fatalf("want the down credited to d1, got %+v", d)
	}
	if rv := findType(r, Revive); len(rv) != 0 {
		t.Fatalf("no revive expected, got %+v", rv)
	}
}

func TestResolveHealth_FinishedByTeamkill(t *testing.T) {
	r := resolveReader([]healthTransition{tr(0, healthStateDowned, 10), tr(0, healthStateDead, 15)},
		down("a1", 10, 0), teamKill("a2", "a1", 15))
	r.resolveHealthEvents()
	if d := findType(r, DBNO); len(d) != 1 || d[0].Username != "a2" || !d[0].TeamKill {
		t.Fatalf("want the down credited to a2 as a teamkill, got %+v", d)
	}
}

func TestResolveHealth_Revived(t *testing.T) {
	r := resolveReader([]healthTransition{tr(0, healthStateDowned, 10), tr(0, healthStateWounded, 20)},
		down("a1", 10, 0), kill("a2", "d1", 25))
	r.scoreRevives = []scoreRevive{{player: 1, elapsed: 21}}
	r.resolveHealthEvents()
	if d := findType(r, DBNO); len(d) != 1 || d[0].Username != "" {
		t.Fatalf("a revived down has no known downer, got %+v", d)
	}
	want := []MatchUpdateType{DBNO, Revive, Kill}
	got := feedTypes(r)
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("want %v, got %v", want, got)
	}
	rv := r.MatchFeedback[1]
	if rv.Username != "a2" || rv.Target != "a1" || rv.ElapsedSeconds != 20 || rv.Phase != ActionPhase {
		t.Fatalf("want a2 reviving a1 at 20, got %+v", rv)
	}
}

func TestResolveHealth_RevivedNoScore(t *testing.T) {
	r := resolveReader([]healthTransition{tr(0, healthStateDowned, 10), tr(0, healthStateWounded, 20)},
		down("a1", 10, 0))
	r.resolveHealthEvents()
	if rv := findType(r, Revive); len(rv) != 1 || rv[0].Username != "" {
		t.Fatalf("want one revive without a reviver, got %+v", rv)
	}
}

func TestResolveHealth_RevivedScoreFromOpponent(t *testing.T) {
	r := resolveReader([]healthTransition{tr(0, healthStateDowned, 10), tr(0, healthStateWounded, 20)},
		down("a1", 10, 0))
	r.scoreRevives = []scoreRevive{{player: 2, elapsed: 20}}
	r.resolveHealthEvents()
	if rv := findType(r, Revive); len(rv) != 1 || rv[0].Username != "" {
		t.Fatalf("an opponent's +50 is not a revive, got %+v", rv)
	}
}

// the state can pass through 2 right before the player dies.
func TestResolveHealth_TransientTwoThenDeath(t *testing.T) {
	r := resolveReader([]healthTransition{tr(0, healthStateDowned, 10), tr(0, healthStateWounded, 30), tr(0, healthStateDead, 30.5)},
		down("a1", 10, 0), kill("d1", "a1", 30.5))
	r.resolveHealthEvents()
	if rv := findType(r, Revive); len(rv) != 0 {
		t.Fatalf("a death right after leaving the downed state is not a revive, got %+v", rv)
	}
	if d := findType(r, DBNO); d[0].Username != "d1" {
		t.Fatalf("want the down credited to d1, got %+v", d)
	}
}

func TestResolveHealth_RoundEnd(t *testing.T) {
	r := resolveReader([]healthTransition{tr(0, healthStateDowned, 10)}, down("a1", 10, 0))
	r.resolveHealthEvents()
	if d := findType(r, DBNO); len(d) != 1 || d[0].Username != "" || len(findType(r, Revive)) != 0 {
		t.Fatalf("still down at round end: want no downer and no revive, got %+v", r.MatchFeedback)
	}
}

func TestResolveHealth_DeathNoKiller(t *testing.T) {
	r := resolveReader([]healthTransition{tr(0, healthStateDowned, 10), tr(0, healthStateDead, 40)},
		down("a1", 10, 0), MatchUpdate{Type: Death, Username: "a1", Phase: ActionPhase, ElapsedSeconds: 40})
	r.resolveHealthEvents()
	if d := findType(r, DBNO); d[0].Username != "" {
		t.Fatalf("a bleed out has no downer, got %+v", d)
	}
}

func TestResolveHealth_DownedTwice(t *testing.T) {
	r := resolveReader([]healthTransition{
		tr(0, healthStateDowned, 10), tr(0, healthStateWounded, 20),
		tr(0, healthStateDowned, 40), tr(0, healthStateDead, 45),
	}, down("a1", 10, 0), down("a1", 40, 2), kill("d2", "a1", 45))
	r.resolveHealthEvents()
	want := []MatchUpdateType{DBNO, Revive, DBNO, Kill}
	got := feedTypes(r)
	if len(got) != 4 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Fatalf("want %v, got %v", want, got)
	}
	if r.MatchFeedback[0].Username != "" || r.MatchFeedback[2].Username != "d2" {
		t.Fatalf("want the first down uncredited and the second credited to d2, got %+v", r.MatchFeedback)
	}
}

// a revived player who is downed again and finished right after the revive keeps the revive.
func TestResolveHealth_RevivedThenQuickDownAndKill(t *testing.T) {
	r := resolveReader([]healthTransition{tr(0, healthStateDowned, 10), tr(0, healthStateWounded, 20), tr(0, healthStateDowned, 20), tr(0, healthStateDead, 21)},
		down("a1", 10, 0), down("a1", 20, 2), kill("d2", "a1", 21))
	r.resolveHealthEvents()
	if got := feedTypes(r); len(got) != 4 || got[1] != Revive || r.MatchFeedback[2].Username != "d2" {
		t.Fatalf("want [DBNO Revive DBNO Kill] with the second down credited to d2, got %v", r.MatchFeedback)
	}
}

// a Revive lands before its target's next DBNO at the same second even when other events of
// that second sit between the DBNO and the time-ordered insert point.
func TestResolveHealth_RevivedThenQuickDownSameSecondEvent(t *testing.T) {
	r := resolveReader([]healthTransition{tr(0, healthStateDowned, 10), tr(0, healthStateWounded, 20), tr(0, healthStateDowned, 20), tr(0, healthStateDead, 21)},
		down("a1", 10, 0), down("a1", 20, 2), kill("a2", "d1", 20), kill("d2", "a1", 21))
	r.resolveHealthEvents()
	got := feedTypes(r)
	if len(got) != 5 {
		t.Fatalf("want 5 events, got %v", got)
	}
	var downs []int
	revive := -1
	for i, u := range r.MatchFeedback {
		if u.Type == DBNO && u.Target == "a1" {
			downs = append(downs, i)
		}
		if u.Type == Revive && u.Target == "a1" {
			revive = i
		}
	}
	if len(downs) != 2 || revive < downs[0] || revive > downs[1] {
		t.Fatalf("want a Revive of a1 between its two downs, got %v", got)
	}
	for i := 1; i < len(r.MatchFeedback); i++ {
		if r.MatchFeedback[i].ElapsedSeconds < r.MatchFeedback[i-1].ElapsedSeconds {
			t.Fatalf("elapsedSeconds decreases at %d: %+v", i, r.MatchFeedback)
		}
	}
}

// a revive through a healthy state before the second down: the down is not a bleed-out.
func TestResolveHealth_RevivedThenReDownViaHealthy(t *testing.T) {
	r := resolveReader([]healthTransition{
		tr(0, healthStateDowned, 10), tr(0, healthStateWounded, 20), tr(0, 0, 20),
		tr(0, healthStateDowned, 21), tr(0, healthStateDead, 21),
	}, down("a1", 10, 0), down("a1", 21, 3), kill("d2", "a1", 21))
	r.resolveHealthEvents()
	want := []MatchUpdateType{DBNO, Revive, DBNO, Kill}
	got := feedTypes(r)
	if len(got) != 4 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Fatalf("want %v, got %v", want, got)
	}
}

// the next transition after the exit from the downed state is another down: the first down was
// revived even when a Kill of the target sits inside the death window before the second DBNO in
// the feed (that Kill belongs to the second down).
func TestResolveHealth_RevivedKillBeforeSecondDownInFeed(t *testing.T) {
	r := resolveReader([]healthTransition{
		tr(0, healthStateDowned, 10), tr(0, healthStateWounded, 20), tr(0, healthStateDowned, 21), tr(0, healthStateDead, 25),
	}, down("a1", 10, 0), kill("d2", "a1", 21), down("a1", 21, 2))
	r.resolveHealthEvents()
	if got := feedTypes(r); len(got) != 4 || got[1] != Revive {
		t.Fatalf("want the first down revived, got %v", got)
	}
}

// a revive whose state change lands in the same second as the down it ends goes after that
// down, even when another event of that second sits between them (the floor in insertByElapsed).
func TestResolveHealth_RevivedSameSecondAsItsDown(t *testing.T) {
	r := resolveReader([]healthTransition{tr(0, healthStateDowned, 20), tr(0, healthStateWounded, 20)},
		down("a1", 20, 0), kill("a2", "d1", 20))
	r.resolveHealthEvents()
	want := []MatchUpdateType{DBNO, Kill, Revive}
	got := feedTypes(r)
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("want %v, got %v", want, got)
	}
}

// two revives of one player in one second: each lands after the down it ends.
func TestResolveHealth_TwoRevivesSameSecond(t *testing.T) {
	r := resolveReader([]healthTransition{
		tr(0, healthStateDowned, 10), tr(0, healthStateWounded, 20), tr(0, healthStateDowned, 20), tr(0, healthStateWounded, 20),
	}, down("a1", 10, 0), down("a1", 20, 2), kill("a2", "d1", 20))
	r.resolveHealthEvents()
	want := []MatchUpdateType{DBNO, Revive, DBNO, Kill, Revive}
	got := feedTypes(r)
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}

// healthValueReplay builds a health link record followed by a health value record
// (23 <health> 00000000 25 26 76 C9 04 <hp>) and returns the offsets of the link and the value.
func healthValueReplay(slot, health []byte, hp uint32) (b []byte, linkOffset, valueOffset int) {
	b = append(b, 0x1B)
	b = append(b, slot...)
	b = append(b, 0, 0, 0, 0)
	b = append(b, healthLinkIndicator...)
	linkOffset = len(b)
	b = append(b, health...)
	b = append(b, 0, 0, 0, 0)
	b = append(b, 0x23)
	b = append(b, health...)
	b = append(b, 0, 0, 0, 0)
	b = append(b, healthValueIndicator...)
	valueOffset = len(b)
	b = append(b, byte(hp), byte(hp>>8), byte(hp>>16), byte(hp>>24))
	b = append(b, make([]byte, 16)...)
	return b, linkOffset, valueOffset
}

func TestHealthValue_Recorded(t *testing.T) {
	slot, health := []byte{1, 2, 3, 4}, []byte{0x38, 0xbb, 0x2b, 0xf0}
	b, link, value := healthValueReplay(slot, health, 87)
	r := newHealthReader(b, slot)
	r.offset = link
	if err := readHealthLink(r); err != nil {
		t.Fatal(err)
	}
	r.clock.started = true
	r.clock.phase = ActionPhase
	r.offset = value
	if err := readHealthValue(r); err != nil {
		t.Fatal(err)
	}
	if len(r.healthSamples) != 1 {
		t.Fatalf("want 1 sample, got %d", len(r.healthSamples))
	}
	s := r.healthSamples[0]
	if s.hp != 87 || s.health != [4]byte{0x38, 0xbb, 0x2b, 0xf0} || !s.started || s.phase != ActionPhase {
		t.Fatalf("unexpected sample %+v", s)
	}
}

// pre-clock values matter: the defender spawn snapshot is written before the clock starts.
func TestHealthValue_RecordedBeforeClock(t *testing.T) {
	slot, health := []byte{1, 2, 3, 4}, []byte{0x38, 0xbb, 0x2b, 0xf0}
	b, link, value := healthValueReplay(slot, health, 110)
	r := newHealthReader(b, slot)
	r.offset = link
	_ = readHealthLink(r)
	r.offset = value
	if err := readHealthValue(r); err != nil {
		t.Fatal(err)
	}
	if len(r.healthSamples) != 1 || r.healthSamples[0].hp != 110 || r.healthSamples[0].started {
		t.Fatalf("want one unstarted sample of 110, got %+v", r.healthSamples)
	}
}

// the player slot is set after the first samples are read; the sample is keyed by health id.
func TestHealthValue_ResolvedAfterPlayersRead(t *testing.T) {
	slot, health := []byte{1, 2, 3, 4}, []byte{0x38, 0xbb, 0x2b, 0xf0}
	b, link, value := healthValueReplay(slot, health, 100)
	r := newHealthReader(b, slot)
	r.Header.Players[0].slotID = nil
	r.offset = link
	_ = readHealthLink(r)
	r.offset = value
	if err := readHealthValue(r); err != nil {
		t.Fatal(err)
	}
	if len(r.healthSamples) != 1 {
		t.Fatalf("sample dropped while the slot was unknown: %+v", r.healthSamples)
	}
	if got := r.playerIndexByHealth(r.healthSamples[0].health); got != -1 {
		t.Fatalf("player resolved before the slot is known: %d", got)
	}
	r.Header.Players[0].slotID = slot
	if got := r.playerIndexByHealth(r.healthSamples[0].health); got != 0 {
		t.Fatalf("want player 0 after slot is set, got %d", got)
	}
}

func TestHealthValue_UnlinkedIgnored(t *testing.T) {
	slot, health := []byte{1, 2, 3, 4}, []byte{0x38, 0xbb, 0x2b, 0xf0}
	b, _, value := healthValueReplay(slot, health, 100)
	r := newHealthReader(b, slot)
	r.offset = value
	if err := readHealthValue(r); err != nil {
		t.Fatal(err)
	}
	if len(r.healthSamples) != 0 {
		t.Fatalf("unlinked value recorded: %+v", r.healthSamples)
	}
}

// a value record cut off by the end of the buffer is ignored.
func TestHealthValue_Truncated(t *testing.T) {
	slot, health := []byte{1, 2, 3, 4}, []byte{0x38, 0xbb, 0x2b, 0xf0}
	b, link, value := healthValueReplay(slot, health, 100)
	r := newHealthReader(b[:value+2], slot)
	r.offset = link
	_ = readHealthLink(r)
	r.offset = value
	if err := readHealthValue(r); err != nil || len(r.healthSamples) != 0 {
		t.Fatalf("err %v samples %+v", err, r.healthSamples)
	}
}

// smp builds samples that share a clock state.
func smp(started bool, phase RoundPhase, hps ...uint32) []healthSample {
	var out []healthSample
	for _, hp := range hps {
		out = append(out, healthSample{hp: hp, started: started, phase: phase})
	}
	return out
}

func cat(parts ...[]healthSample) []healthSample {
	var out []healthSample
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestDamageTaken_Survivor(t *testing.T) {
	s := cat(pre(110), smp(true, ActionPhase, 70))
	if got := damageTakenFromSeries(s, false, false); got != 40 {
		t.Fatalf("want 40, got %d", got)
	}
}

func TestDamageTaken_Untouched(t *testing.T) {
	s := cat(pre(0), smp(true, PrepPhase, 125))
	if got := damageTakenFromSeries(s, true, false); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
}

func TestDamageTaken_AttackerStaleSnapshot(t *testing.T) {
	cases := []struct {
		name string
		s    []healthSample
		want int
	}{
		{"66 then 125", cat(pre(66), smp(true, PrepPhase, 125), smp(true, ActionPhase, 101, 77, 53, 29, 0)), 125},
		{"110 p0 p100", cat(pre(110), smp(true, PrepPhase, 0, 100), smp(true, ActionPhase, 53, 6, 0)), 100},
		{"stale above spawn", cat(pre(125), smp(true, PrepPhase, 110), smp(true, ActionPhase, 0)), 110},
	}
	for _, c := range cases {
		if got := damageTakenFromSeries(c.s, true, true); got != c.want {
			t.Errorf("%s: want %d, got %d", c.name, c.want, got)
		}
	}
}

func TestDamageTaken_DeathWithoutZero(t *testing.T) {
	if got := damageTakenFromSeries(cat(pre(110), smp(true, ActionPhase, 87)), false, true); got != 110 {
		t.Fatalf("want 110, got %d", got)
	}
	if got := damageTakenFromSeries(pre(110), false, true); got != 110 {
		t.Fatalf("one-shot: want 110, got %d", got)
	}
	if got := damageTakenFromSeries(nil, false, true); got != 0 {
		t.Fatalf("empty series: want 0, got %d", got)
	}
}

func TestDamageTaken_ReviveAndRook(t *testing.T) {
	s := cat(pre(110), smp(true, ActionPhase, 82, 54, 0, 20, 45, 22, 0))
	if got := damageTakenFromSeries(s, false, true); got != 155 {
		t.Fatalf("want 155, got %d", got)
	}
	s = cat(pre(125), smp(true, PrepPhase, 150), smp(true, ActionPhase, 90, 30, 0, 20, 0))
	if got := damageTakenFromSeries(s, false, true); got != 170 {
		t.Fatalf("prep Rook: want 170, got %d", got)
	}
}

func TestDamageTaken_BoostDecay(t *testing.T) {
	var act []uint32
	for hp := uint32(130); hp >= 110; hp-- {
		act = append(act, hp)
	}
	for hp := uint32(130); hp >= 121; hp-- {
		act = append(act, hp)
	}
	act = append(act, 94, 67, 40, 13, 0)
	s := cat(pre(0), smp(true, PrepPhase, 110), smp(true, ActionPhase, act...))
	if got := damageTakenFromSeries(s, true, true); got != 121 {
		t.Fatalf("want 121, got %d", got)
	}
}

func TestDamageTaken_DefenderPrepDamage(t *testing.T) {
	s := cat(pre(110), smp(true, PrepPhase, 78), smp(true, ActionPhase, 18, 0))
	if got := damageTakenFromSeries(s, false, true); got != 110 {
		t.Fatalf("want 110, got %d", got)
	}
}

func TestDamageTaken_RealOneHPHitBelowSpawn(t *testing.T) {
	s := cat(pre(110), smp(true, ActionPhase, 50, 49))
	if got := damageTakenFromSeries(s, false, false); got != 61 {
		t.Fatalf("want 61, got %d", got)
	}
}

// an attacker whose last pre-action sample is a stale unstarted one (no prep reset written).
func TestDamageTaken_AttackerNoPrepReset(t *testing.T) {
	s := cat(pre(100), smp(true, ActionPhase, 60))
	if got := damageTakenFromSeries(s, true, false); got != 40 {
		t.Fatalf("want 40, got %d", got)
	}
}

func TestReaderDamageTaken_RoleAndDeath(t *testing.T) {
	sa, sd, sx := [4]byte{1, 0, 0, 0}, [4]byte{2, 0, 0, 0}, [4]byte{3, 0, 0, 0}
	ha, hd := [4]byte{0xA1}, [4]byte{0xD1}
	r := &Reader{
		healthToSlot: map[[4]byte][4]byte{ha: sa, hd: sd},
		Header: Header{
			Teams: [2]Team{{Role: Attack}, {Role: Defense}},
			Players: []Player{
				{Username: "atk", TeamIndex: 0, slotID: sa[:]},
				{Username: "def", TeamIndex: 1, slotID: sd[:]},
				{Username: "none", TeamIndex: 1, slotID: sx[:]},
			},
		},
		MatchFeedback: []MatchUpdate{{Type: Kill, Username: "atk", Target: "def"}},
	}
	for _, s := range cat(pre(66), smp(true, PrepPhase, 100), smp(true, ActionPhase, 60)) {
		s.health = ha
		r.healthSamples = append(r.healthSamples, s)
	}
	for _, s := range cat(pre(110), smp(true, ActionPhase, 87)) {
		s.health = hd
		r.healthSamples = append(r.healthSamples, s)
	}
	got := r.damageTaken()
	if len(got) != 2 || got[0] != 40 || got[1] != 110 {
		t.Fatalf("want {0:40 1:110}, got %v", got)
	}
}

// pre builds samples read before the round clock started.
func pre(hps ...uint32) []healthSample { return smp(false, ActionPhase, hps...) }

// a player whose team index is out of range is treated as a defender, not a panic.
func TestReaderDamageTaken_BadTeamIndex(t *testing.T) {
	slot, health := [4]byte{1}, [4]byte{0xA1}
	r := &Reader{
		healthToSlot: map[[4]byte][4]byte{health: slot},
		Header:       Header{Players: []Player{{Username: "x", TeamIndex: 5, slotID: slot[:]}}},
	}
	for _, s := range cat(pre(100), smp(true, ActionPhase, 80)) {
		s.health = health
		r.healthSamples = append(r.healthSamples, s)
	}
	if got := r.damageTaken(); got[0] != 20 {
		t.Fatalf("want 20, got %v", got)
	}
}

// stale snapshot values before the clock starts are not damage, even without a prep sample.
func TestDamageTaken_AttackerStaleThenZero(t *testing.T) {
	s := cat(pre(100, 0), smp(true, ActionPhase, 60))
	if got := damageTakenFromSeries(s, true, false); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
}

// a death with no killer is a Death update for the player, not a Kill.
func TestReaderDamageTaken_DeathWithoutKiller(t *testing.T) {
	slot, health := [4]byte{1}, [4]byte{0xA1}
	r := &Reader{
		healthToSlot:  map[[4]byte][4]byte{health: slot},
		Header:        Header{Players: []Player{{Username: "x", TeamIndex: 1, slotID: slot[:]}}},
		MatchFeedback: []MatchUpdate{{Type: Death, Username: "x"}},
	}
	for _, s := range cat(pre(110), smp(true, ActionPhase, 87)) {
		s.health = health
		r.healthSamples = append(r.healthSamples, s)
	}
	if got := r.damageTaken(); got[0] != 110 {
		t.Fatalf("want 110, got %v", got)
	}
}
