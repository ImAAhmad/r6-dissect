package dissect

import (
	"encoding/binary"
	"math"
	"testing"
)

type defuserTestReplay struct {
	r *Reader
}

func newDefuserTestReplay() *defuserTestReplay {
	r := &Reader{
		Header: Header{
			CodeVersion: Y11S1,
			Teams:       [2]Team{{Role: Attack}, {Role: Defense}},
			Players: []Player{
				{Username: "attacker", TeamIndex: 0, slotID: []byte{0xC2, 0x68, 0x06, 0xF0}},
				{Username: "defender", TeamIndex: 1, slotID: []byte{0x72, 0x6F, 0x06, 0xF0}},
				{Username: "defender2", TeamIndex: 1, slotID: []byte{0x7C, 0x6F, 0x06, 0xF0}},
			},
		},
		lastDefuserPlayerIndex: -1,
		pendingDefuserPlantIdx: -1,
		defuserPawnToSlot:      make(map[[4]byte][4]byte),
	}
	return &defuserTestReplay{r: r}
}

// feed appends a packet ending with pattern and calls listener right after the pattern.
func (d *defuserTestReplay) feed(t *testing.T, packet []byte, tail []byte, listener func(*Reader) error) {
	t.Helper()
	d.r.b = append(d.r.b, packet...)
	d.r.offset = len(d.r.b)
	d.r.b = append(d.r.b, tail...)
	d.r.b = append(d.r.b, make([]byte, 64)...) // padding, like the rest of a replay
	if err := listener(d.r); err != nil {
		t.Fatal(err)
	}
}

func (d *defuserTestReplay) spawn(t *testing.T, slot, pawn []byte) {
	p := append([]byte{0x1B}, slot...)
	p = append(p, 0, 0, 0, 0)
	p = append(p, defuserPawnIndicator...)
	tail := append(append([]byte{}, pawn...), 0, 0, 0, 0)
	d.feed(t, p, tail, readDefuserPawn)
}

func (d *defuserTestReplay) timer(t *testing.T, pawn []byte, state int, remaining float32, timer string) {
	var p []byte
	if state >= 0 {
		p = append(p, 0x23)
		p = append(p, pawn...)
		p = append(p, 0, 0, 0, 0)
		p = append(p, defuserStateProperty...)
		p = append(p, 0x04)
		p = binary.LittleEndian.AppendUint32(p, uint32(state))
		p = append(p, 0x22)
		p = append(p, defuserRemainingProperty...)
	} else {
		p = append(p, 0x23)
		p = append(p, pawn...)
		p = append(p, 0, 0, 0, 0)
		p = append(p, defuserRemainingProperty...)
	}
	p = append(p, 0x04)
	p = binary.LittleEndian.AppendUint32(p, math.Float32bits(remaining))
	p = append(p, 0x22, 0xA9, 0xC8, 0x58, 0xD9)
	tail := append([]byte{byte(len(timer))}, timer...)
	d.feed(t, p, tail, readDefuserTimer)
}

func TestDefuserY10S4AttributesFromPawn(t *testing.T) {
	d := newDefuserTestReplay()
	atkPawn := []byte{0x03, 0xB4, 0x0C, 0xF0}
	defPawn := []byte{0x07, 0xB4, 0x0C, 0xF0}
	unknownPawn := []byte{0x09, 0xB4, 0x0C, 0xF0}
	d.spawn(t, d.r.Header.Players[0].slotID, atkPawn)
	d.spawn(t, d.r.Header.Players[1].slotID, defPawn)

	// interrupted plant
	d.timer(t, atkPawn, defuserStatePlanting, 1, "7.000")
	d.timer(t, atkPawn, -1, 0.9, "6.300")
	d.timer(t, atkPawn, defuserStateIdle, 0.8, "5.600")
	// completed plant, with two packets under 0.01
	d.timer(t, atkPawn, defuserStatePlanting, 1, "7.000")
	d.timer(t, atkPawn, -1, 0.5, "3.500")
	d.timer(t, atkPawn, -1, 0.001, "0.009")
	d.timer(t, atkPawn, defuserStateIdle, 0.0001, "0.001")
	// disable interrupted by the end of the round
	d.timer(t, defPawn, defuserStateDisabling, 1, "6.983")
	d.timer(t, defPawn, -1, 0.9, "6.300")
	d.r.stopDefuserRun()
	// unresolvable pawn and no fallback (both teams alive): no start event
	d.timer(t, unknownPawn, defuserStateDisabling, 1, "6.983")

	want := []struct {
		typ  MatchUpdateType
		user string
	}{
		{DefuserPlantStart, "attacker"},
		{DefuserPlantStop, "attacker"},
		{DefuserPlantStart, "attacker"},
		{DefuserPlantComplete, "attacker"},
		{DefuserDisableStart, "defender"},
		{DefuserDisableStop, "defender"},
	}
	got := d.r.MatchFeedback
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Type != w.typ || got[i].Username != w.user {
			t.Errorf("event %d: got %s/%q, want %s/%q", i, got[i].Type, got[i].Username, w.typ, w.user)
		}
	}
	if !d.r.planted {
		t.Error("expected the defuser to be planted")
	}
}

func TestPeekPlayerSlotID(t *testing.T) {
	// bytes following the DissectID in a Y11S2 player packet
	b := []byte{
		0x00, 0x00, 0x00, 0x00, 0x4A, 0xCD, 0x2D, 0x46, 0x01, 0x00, 0x22, 0x15, 0x44, 0x67, 0x1F, 0x01,
		0x00, 0x22, 0x85, 0x4C, 0x3B, 0x1E, 0x01, 0x00, 0x23, 0xC2, 0x68, 0x06, 0xF0, 0x00, 0x00, 0x00,
		0x00, 0x63, 0xCC, 0x18, 0x8F, 0x08,
	}
	r := &Reader{b: b}
	got := r.peekPlayerSlotID()
	if string(got) != string([]byte{0xC2, 0x68, 0x06, 0xF0}) {
		t.Fatalf("got %x", got)
	}
	if r.offset != 0 {
		t.Fatal("peek moved the reader")
	}
}
