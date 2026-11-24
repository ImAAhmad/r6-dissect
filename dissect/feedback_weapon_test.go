package dissect

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

// killHeaders are the bytes readMatchFeedback reads between the match feedback listener pattern
// (59 34 E5 8B 04) and the size byte, for each of its three code version ranges.
var killHeaders = []struct {
	name        string
	codeVersion int
	header      []byte
}{
	{"Y8S2", Y8S2, slices.Concat([]byte{0x00}, activity2)},                      // Skip(1), Seek(activity2)
	{"Y9S1", Y9S1, slices.Concat(make([]byte, 9), []byte{4}, make([]byte, 24))}, // Skip(9), valid 4, Skip(24)
	{"Y11S2", Y11S2, make([]byte, 38)},                                          // Skip(38)
}

// killPacket builds a kill packet as readMatchFeedback reads it: header, size 0 (kill), the kill
// indicator, killer, 15 bytes, target, 56 bytes, the headshot byte, then tail. The result has no
// spare capacity, so reading past its end panics instead of reading zeros.
func killPacket(header []byte, killer, target string, headshot byte, tail []byte) []byte {
	p := slices.Concat(header, []byte{0x00}, killIndicator)
	p = append(p, byte(len(killer)))
	p = append(p, killer...)
	p = append(p, make([]byte, 15)...)
	p = append(p, byte(len(target)))
	p = append(p, target...)
	p = append(p, make([]byte, 56)...)
	p = append(p, headshot)
	return slices.Clip(append(p, tail...))
}

func weaponTail(id uint64) []byte {
	return binary.LittleEndian.AppendUint64(slices.Clone(killWeaponField), id)
}

func killReader(codeVersion int, b []byte) *Reader {
	return &Reader{b: b, Header: Header{CodeVersion: codeVersion, Players: []Player{
		{Username: "atk", TeamIndex: 0}, {Username: "def", TeamIndex: 1},
	}}}
}

// readOneKill runs readMatchFeedback on b and returns the one Kill it adds and the offset it
// leaves, which must be just past the headshot byte (the weapon read does not move it).
func readOneKill(t *testing.T, codeVersion int, b []byte, tailLen int) MatchUpdate {
	t.Helper()
	r := killReader(codeVersion, b)
	if err := readMatchFeedback(r); err != nil {
		t.Fatalf("readMatchFeedback: %v", err)
	}
	if len(r.MatchFeedback) != 1 || r.MatchFeedback[0].Type != Kill {
		t.Fatalf("want one Kill, got %+v", r.MatchFeedback)
	}
	if want := len(b) - tailLen; r.offset != want {
		t.Errorf("offset %d, want %d (just past the headshot byte)", r.offset, want)
	}
	return r.MatchFeedback[0]
}

// The weapon field directly follows the headshot byte on every season, and the packet may end
// with it at the very end of the buffer.
func TestReadMatchFeedback_KillWeaponID(t *testing.T) {
	for _, h := range killHeaders {
		t.Run(h.name, func(t *testing.T) {
			tail := weaponTail(395972905309) // above 2^32, so all 8 bytes are read
			u := readOneKill(t, h.codeVersion, killPacket(h.header, "atk", "def", 1, tail), len(tail))
			if u.WeaponID != 395972905309 {
				t.Errorf("WeaponID %d, want 395972905309", u.WeaponID)
			}
			if u.feedKiller != "atk" {
				t.Errorf("feedKiller %q, want atk", u.feedKiller)
			}
		})
	}
}

// A tail that does not hold the weapon field, or holds only part of it, gives weaponID 0 and the
// kill is still read.
func TestReadMatchFeedback_KillWithoutWeaponField(t *testing.T) {
	full := weaponTail(1366019496)
	for _, tc := range []struct {
		name string
		tail []byte
	}{
		{"other_field", []byte{0x22, 0x41, 0x9D, 0x9D, 0xD7, 0x08, 1, 2, 3, 4, 5, 6, 7, 8, 0}},
		// the weapon tag with another size byte: a resized field is not the weapon field
		{"resized_field", []byte{0x22, 0xF8, 0x6C, 0xDD, 0x65, 0x04, 1, 2, 3, 4, 0x22, 0x41, 0x9D, 0x9D, 0xD7}},
		{"truncated", full[:len(full)-1]},
		{"tag_cut", full[:3]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := readOneKill(t, Y11S2, killPacket(make([]byte, 38), "atk", "def", 0, tc.tail), len(tc.tail))
			if u.WeaponID != 0 {
				t.Errorf("WeaponID %d, want 0", u.WeaponID)
			}
			if u.feedKiller != "atk" {
				t.Errorf("feedKiller %q, want atk", u.feedKiller)
			}
		})
	}
}

func TestLoadoutSlot(t *testing.T) {
	l := &Loadout{Primary: 1, Secondary: 2, Gadget: 3, Ability: 4}
	for _, tc := range []struct {
		name  string
		l     *Loadout
		asset uint64
		want  WeaponSlot
	}{
		{"primary", l, 1, PrimarySlot},
		{"secondary", l, 2, SecondarySlot},
		{"gadget", l, 3, GadgetSlot},
		{"ability", l, 4, AbilitySlot},
		{"not in the loadout", l, 5, ""},
		{"asset 0", l, 0, ""},
		{"asset 0 and an empty slot", &Loadout{Primary: 1}, 0, ""},
		{"nil loadout", nil, 1, ""},
		// one asset in two slots (Blackbeard's primary and ability in Y11S3): the first slot in
		// the order primary, secondary, gadget, ability wins
		{"primary before ability", &Loadout{Primary: 7, Ability: 7}, 7, PrimarySlot},
		{"primary before secondary", &Loadout{Primary: 7, Secondary: 7}, 7, PrimarySlot},
		{"secondary before gadget", &Loadout{Secondary: 7, Gadget: 7}, 7, SecondarySlot},
		{"gadget before ability", &Loadout{Gadget: 7, Ability: 7}, 7, GadgetSlot},
	} {
		if got := tc.l.Slot(tc.asset); got != tc.want {
			t.Errorf("%s: Slot(%d) = %q, want %q", tc.name, tc.asset, got, tc.want)
		}
	}
}

// weaponPlayers: a1 and its teammate a2 have loadouts, d1 has none.
func weaponPlayers() []Player {
	return []Player{
		{Username: "a1", TeamIndex: 0, Loadout: &Loadout{Primary: 10, Secondary: 20, Gadget: 30, Ability: 40}},
		{Username: "a2", TeamIndex: 0, Loadout: &Loadout{Primary: 30, Ability: 99}},
		{Username: "d1", TeamIndex: 1},
		{Username: "d2", TeamIndex: 1},
	}
}

func TestClassifyKillWeapons(t *testing.T) {
	withFeedKiller := func(u MatchUpdate, finisher string, weapon uint64) MatchUpdate {
		u.feedKiller = finisher
		u.WeaponID = weapon
		return u
	}
	withWeapon := func(u MatchUpdate, weapon uint64) MatchUpdate {
		u.WeaponID = weapon
		return u
	}
	cases := []struct {
		name string
		u    MatchUpdate
		want WeaponSlot
	}{
		{"own secondary", withFeedKiller(kill("a1", "d1", 1), "a1", 20), SecondarySlot},
		{"no feedKiller: classified by username", withWeapon(kill("a1", "d1", 2), 10), PrimarySlot},
		{"a teammate's gadget", withFeedKiller(kill("a1", "d2", 3), "a1", 99), ""},
		{"killer without a loadout", withFeedKiller(kill("d1", "a1", 4), "d1", 10), ""},
		{"killer not a player", withFeedKiller(kill("ghost", "a1", 5), "ghost", 10), ""},
		{"Death", withWeapon(MatchUpdate{Type: Death, Username: "a1"}, 10), ""},
		// a kill reconciliation credited to the downer a2: the weapon is the finisher a1's gadget,
		// even though 30 is a2's primary
		{"reconciled downer credit", withFeedKiller(kill("a2", "d2", 6), "a1", 30), GadgetSlot},
	}
	r := &Reader{Header: Header{Players: weaponPlayers()}}
	for _, tc := range cases {
		r.MatchFeedback = append(r.MatchFeedback, tc.u)
	}
	r.classifyKillWeapons()
	for i, tc := range cases {
		if got := r.MatchFeedback[i].WeaponSlot; got != tc.want {
			t.Errorf("%s: WeaponSlot %q, want %q", tc.name, got, tc.want)
		}
	}
}

// feedKiller survives the passes that rewrite or copy a Kill's username after it is read:
// reconciliation (inside roundEnd) credits the downer, resolveHealthEvents copies the credited
// killer to the DBNO, and a revive inserted before the kill shifts it in the slice.
func TestFeedKiller_SurvivesReconcileAndHealth(t *testing.T) {
	finish := kill("a1", "d1", 20)
	finish.feedKiller = "a1"
	finish.scoreboardKiller = "a2"
	finish.WeaponID = 30
	r := resolveReader(
		[]healthTransition{tr(3, healthStateDowned, 5), tr(3, healthStateWounded, 8), tr(2, healthStateDowned, 10), tr(2, healthStateDead, 20)},
		down("d2", 5, 0), down("d1", 10, 2), finish)
	r.Header.CodeVersion = Y10S4
	r.Header.Players = weaponPlayers()
	r.Header.Teams = [2]Team{{Role: Attack, Score: 1}, {Role: Defense}}
	r.scoreboardIDToPlayer = map[[4]byte]int{{1}: 0, {2}: 1, {3}: 2, {4}: 3}
	r.scoreboardInitialKills = map[[4]byte]uint32{{1}: 0, {2}: 0, {3}: 0, {4}: 0}
	r.scoreboardFinalKills = map[int]uint32{0: 0, 1: 1, 2: 0, 3: 0} // the scoreboard credits a2

	r.roundEnd()
	r.resolveHealthEvents()
	r.classifyKillWeapons()

	if got, want := fmt.Sprint(feedTypes(r)), fmt.Sprint([]MatchUpdateType{DBNO, Revive, DBNO, Kill}); got != want {
		t.Fatalf("feed %s, want %s", got, want)
	}
	k := r.MatchFeedback[3]
	if k.Username != "a2" {
		t.Fatalf("reconciliation did not credit the downer: username %q", k.Username)
	}
	if d := r.MatchFeedback[2]; d.Username != "a2" {
		t.Errorf("DBNO credited to %q, want a2", d.Username)
	}
	if k.feedKiller != "a1" || k.WeaponID != 30 || k.WeaponSlot != GadgetSlot {
		t.Errorf("feedKiller %q, weaponID %d, weaponSlot %q; want a1, 30, gadget", k.feedKiller, k.WeaponID, k.WeaponSlot)
	}
}

// The two keys are on a Kill with a weapon, also when MarshalJSON overrides the operator key
// (a renamed operator), and absent without a weapon.
func TestJSON_KillWeaponKeys(t *testing.T) {
	for _, tc := range []struct {
		name     string
		u        MatchUpdate
		id       float64
		slot     string
		operator any
	}{
		{"kill", MatchUpdate{Type: Kill, Username: "a1", WeaponID: 1366019496, WeaponSlot: PrimarySlot}, 1366019496, "primary", nil},
		{"renamed operator", MatchUpdate{Type: Kill, Username: "a1", WeaponID: 395972905309, WeaponSlot: AbilitySlot,
			Operator: 123, operatorName: "SolidSnake"}, 395972905309, "ability", map[string]any{"name": "SolidSnake", "id": float64(123)}},
		{"unclassified", MatchUpdate{Type: Kill, Username: "a1", WeaponID: 70077334926}, 70077334926, "", nil},
		{"death", MatchUpdate{Type: Death, Username: "a1"}, 0, "", nil},
	} {
		b, err := json.Marshal(tc.u)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err = json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		id, hasID := m["weaponID"]
		if (tc.id != 0) != hasID || (hasID && id != tc.id) {
			t.Errorf("%s: weaponID %v (present %v), want %v in %s", tc.name, id, hasID, tc.id, b)
		}
		slot, hasSlot := m["weaponSlot"]
		if (tc.slot != "") != hasSlot || (hasSlot && slot != tc.slot) {
			t.Errorf("%s: weaponSlot %v (present %v), want %q in %s", tc.name, slot, hasSlot, tc.slot, b)
		}
		if tc.operator != nil && fmt.Sprint(m["operator"]) != fmt.Sprint(tc.operator) {
			t.Errorf("%s: operator %v, want %v", tc.name, m["operator"], tc.operator)
		}
	}
}
