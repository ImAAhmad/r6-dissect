package dissect

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"testing"
)

// scoreboardLink writes op <player obj> EB219B38 <entity> <class>, the reference from a player
// object to its scoreboard entity. A 0x1A op writes the reference form, which has no owner handle.
func (b *inventoryBuilder) scoreboardLink(op byte, playerObj, entity uint64, class []byte) {
	b.WriteByte(op)
	if op != 0x1A {
		b.handle(playerObj)
	}
	b.Write(scoreboardEntityField)
	b.handle(entity)
	b.Write(class)
}

// sbID returns the scoreboard entry ID of an entity handle: its low 4 bytes, little-endian.
func sbID(entity uint64) [4]byte {
	var id [4]byte
	binary.LittleEndian.PutUint32(id[:], uint32(entity))
	return id
}

// readLinks runs readScoreboardEntity at every scoreboardEntityField in r.b.
func readLinks(r *Reader) {
	for i := 0; i+len(scoreboardEntityField) <= len(r.b); i++ {
		if string(r.b[i:i+len(scoreboardEntityField)]) == string(scoreboardEntityField) {
			r.offset = i + len(scoreboardEntityField)
			if err := readScoreboardEntity(r); err != nil {
				panic(err)
			}
		}
	}
}

func TestReadScoreboardEntity(t *testing.T) {
	const obj, ent = 0xF02BBB30, 0xF02BBB39
	for _, tc := range []struct {
		name  string
		build func(b *inventoryBuilder)
		want  bool
	}{
		{"named ref", func(b *inventoryBuilder) { b.scoreboardLink(0x1B, obj, ent, scoreboardEntityClass) }, true},
		{"named ref after other data", func(b *inventoryBuilder) {
			b.Write([]byte{0x22, 0x01, 0x02, 0x03, 0x04, 0x01, 0x00})
			b.scoreboardLink(0x1B, obj, ent, scoreboardEntityClass)
			b.Write([]byte{0x23})
		}, true},
		// the departed player's entity in a Y11S1 round: the reference form, after an empty inventory
		{"unnamed ref after an empty inventory", func(b *inventoryBuilder) {
			b.WriteByte(0x1B)
			b.handle(obj)
			b.Write(inventoryOwnerField)
			b.Write(make([]byte, 12))
			b.scoreboardLink(0x1A, 0, ent, scoreboardEntityClass)
		}, false},
		{"0x1A before an owner handle", func(b *inventoryBuilder) {
			b.WriteByte(0x1A)
			b.handle(obj)
			b.Write(scoreboardEntityField)
			b.handle(ent)
			b.Write(scoreboardEntityClass)
		}, false},
		{"unnamed ref at the start", func(b *inventoryBuilder) { b.scoreboardLink(0x1A, 0, ent, scoreboardEntityClass) }, false},
		{"other op", func(b *inventoryBuilder) {
			b.WriteByte(0x23)
			b.handle(obj)
			b.Write(scoreboardEntityField)
			b.handle(ent)
			b.Write(scoreboardEntityClass)
		}, false},
		{"other class", func(b *inventoryBuilder) { b.scoreboardLink(0x1B, obj, ent, []byte{1, 2, 3, 4}) }, false},
		{"non-zero high half", func(b *inventoryBuilder) { b.scoreboardLink(0x1B, obj, 0x00000001F02BBB39, scoreboardEntityClass) }, false},
		{"zero owner", func(b *inventoryBuilder) { b.scoreboardLink(0x1B, 0, ent, scoreboardEntityClass) }, false},
		{"zero entity", func(b *inventoryBuilder) { b.scoreboardLink(0x1B, obj, 0, scoreboardEntityClass) }, false},
		{"truncated class", func(b *inventoryBuilder) {
			b.scoreboardLink(0x1B, obj, ent, scoreboardEntityClass)
			b.Truncate(b.Len() - 1)
		}, false},
		{"truncated entity", func(b *inventoryBuilder) {
			b.scoreboardLink(0x1B, obj, ent, scoreboardEntityClass)
			b.Truncate(b.Len() - 9)
		}, false},
		{"field at the start", func(b *inventoryBuilder) {
			b.Write(scoreboardEntityField)
			b.handle(ent)
			b.Write(scoreboardEntityClass)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b inventoryBuilder
			tc.build(&b)
			r := &Reader{b: b.Bytes(), scoreboardEntityOwner: map[[4]byte]uint64{}}
			readLinks(r)
			owner, ok := r.scoreboardEntityOwner[sbID(ent)]
			if tc.want && (!ok || owner != obj) {
				t.Fatalf("got %v (owner %#x), want a link to %#x", r.scoreboardEntityOwner, owner, uint64(obj))
			}
			if !tc.want && len(r.scoreboardEntityOwner) != 0 {
				t.Fatalf("got %v, want no link", r.scoreboardEntityOwner)
			}
		})
	}
}

func readFixture(t *testing.T, path string) *Reader {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Read(); !Ok(err) {
		t.Fatal(err)
	}
	return r
}

// On Y10S4+ every player's scoreboard entity is linked to their player object; before Y10S4 the
// listener is not registered (the field is used tens of thousands of times per round there).
func TestScoreboardEntityLink_Fixtures(t *testing.T) {
	for _, path := range []string{
		"test/data/replays/valid/Y11S2/custom_solid_snake.rec",
		"test/data/replays/valid/Y11S2/custom_early_plant.rec",
	} {
		t.Run(path, func(t *testing.T) {
			r := readFixture(t, path)
			linked := map[int]int{}
			for _, id := range r.pendingSBIDs {
				owner, ok := r.scoreboardEntityOwner[id]
				if !ok {
					t.Errorf("sbID %x has no link", id)
					continue
				}
				i := r.playerIndexByObject(owner)
				if i < 0 {
					t.Errorf("sbID %x: owner %#x is not a player object", id, owner)
					continue
				}
				linked[i]++
				if got := r.scoreboardIDToPlayer[id]; got != i {
					t.Errorf("sbID %x mapped to %d, linked to %d", id, got, i)
				}
			}
			if len(linked) != len(r.Header.Players) || len(r.pendingSBIDs) != len(r.Header.Players) {
				t.Errorf("%d players linked, %d sbIDs, %d players", len(linked), len(r.pendingSBIDs), len(r.Header.Players))
			}
		})
	}
	r := readFixture(t, "test/data/replays/valid/Y8S1/quick_1.rec")
	if len(r.scoreboardEntityOwner) != 0 {
		t.Errorf("Y8S1: %d links read, want none", len(r.scoreboardEntityOwner))
	}
}

const (
	objA, objB, objC = 0xF0000010, 0xF0000020, 0xF0000030
	objGone          = 0xF0000099 // a player object without a player packet: the player left
	entA, entB, entC = 0xF00000A1, 0xF00000B1, 0xF00000C1
	entE             = 0xF00000E1 // the entity of the player who left
)

// linkReader returns the state readPlayer leaves for players a, b and c (objects objA, objB and
// objC), read in that order.
func linkReader() *Reader {
	r := &Reader{
		Header:                 Header{CodeVersion: Y10S4},
		inventory:              newInventoryState(),
		scoreboardIDToPlayer:   map[[4]byte]int{},
		scoreboardEntityOwner:  map[[4]byte]uint64{},
		scoreboardInitialKills: map[[4]byte]uint32{},
		scoreboardFinalKills:   map[int]uint32{},
		lastPlayerScores:       map[int]uint32{},
		pendingDefuserPlantIdx: -1,
		lastDefuserPlayerIndex: -1,
	}
	for i, name := range []string{"a", "b", "c"} {
		p := Player{Username: name, TeamIndex: i % 2, Operator: Recruit, DissectID: []byte{byte(0x11 * (i + 1)), 0, 0, 0}}
		r.Header.Players = append(r.Header.Players, p)
		r.readPlayerOrder = append(r.readPlayerOrder, i)
	}
	r.trackPlayerObject(objA, r.Header.Players[0])
	r.trackPlayerObject(objB, r.Header.Players[1])
	r.trackPlayerObject(objC, r.Header.Players[2])
	return r
}

func (r *Reader) link(ent, obj uint64) {
	r.scoreboardEntityOwner[sbID(ent)] = obj
}

func wantMapping(t *testing.T, r *Reader, want map[uint64]int, absent ...uint64) {
	t.Helper()
	for ent, idx := range want {
		if got, ok := r.scoreboardIDToPlayer[sbID(ent)]; !ok || got != idx {
			t.Errorf("entity %#x: got %d (mapped %v), want %d", ent, got, ok, idx)
		}
	}
	for _, ent := range absent {
		if got, ok := r.scoreboardIDToPlayer[sbID(ent)]; ok {
			t.Errorf("entity %#x: mapped to %d, want unmapped", ent, got)
		}
	}
}

// The player who left keeps a linked entity whose owner has no player packet. It stays unmapped
// and is not zipped onto the next player.
func TestEnsureScoreboardMapping_LinkBeatsZip(t *testing.T) {
	r := linkReader()
	r.pendingSBIDs = [][4]byte{sbID(entA), sbID(entB), sbID(entE), sbID(entC)}
	r.link(entA, objA)
	r.link(entB, objB)
	r.link(entE, objGone)
	r.link(entC, objC)
	r.ensureScoreboardMapping()
	wantMapping(t, r, map[uint64]int{entA: 0, entB: 1, entC: 2}, entE)
}

// The form seen in the corpus: the entity of the player who left has no named link at all, and no
// player matches its +4 candidate. It stays unmapped.
func TestEnsureScoreboardMapping_UnlinkedLeftPlayer(t *testing.T) {
	r := linkReader()
	r.pendingSBIDs = [][4]byte{sbID(entA), sbID(entB), sbID(entE), sbID(entC)}
	r.link(entA, objA)
	r.link(entB, objB)
	r.link(entC, objC)
	r.ensureScoreboardMapping()
	wantMapping(t, r, map[uint64]int{entA: 0, entB: 1, entC: 2}, entE)
}

// A late joiner's player packet comes after the first mapping: their entity is resolved on a
// later call, and nothing is zipped onto them in the meantime.
func TestEnsureScoreboardMapping_LateOwner(t *testing.T) {
	r := linkReader()
	r.inventory.players = map[uint64]inventoryPlayer{}
	r.trackPlayerObject(objA, r.Header.Players[0])
	r.trackPlayerObject(objB, r.Header.Players[1])
	r.pendingSBIDs = [][4]byte{sbID(entA), sbID(entB), sbID(entE), sbID(entC)}
	r.link(entA, objA)
	r.link(entB, objB)
	r.link(entE, objGone)
	r.link(entC, objC)
	r.ensureScoreboardMapping()
	wantMapping(t, r, map[uint64]int{entA: 0, entB: 1}, entE, entC)
	for id, idx := range r.scoreboardIDToPlayer {
		if idx == 2 {
			t.Errorf("sbID %x zipped onto player 2 before their player packet", id)
		}
	}
	r.trackPlayerObject(objC, r.Header.Players[2])
	r.ensureScoreboardMapping()
	wantMapping(t, r, map[uint64]int{entA: 0, entB: 1, entC: 2}, entE)
}

// A round without any link keeps the zip of first-seen order with readPlayer order.
func TestEnsureScoreboardMapping_NoLinksKeepsZip(t *testing.T) {
	r := linkReader()
	r.Header.Players = r.Header.Players[:2]
	r.readPlayerOrder = []int{1, 0}
	r.pendingSBIDs = [][4]byte{sbID(entA), sbID(entB)}
	r.ensureScoreboardMapping()
	wantMapping(t, r, map[uint64]int{entA: 1, entB: 0})
}

// Without links, sbIDs past the zip still use the +4 rule.
func TestEnsureScoreboardMapping_NoLinksKeepsPlus4(t *testing.T) {
	r := linkReader()
	r.Header.Players[2].DissectID = []byte{0xC5, 0x00, 0x00, 0xF0} // sbID(entC) + 4
	r.readPlayerOrder = []int{0, 1}
	r.pendingSBIDs = [][4]byte{sbID(entA), sbID(entB), sbID(entC)}
	r.ensureScoreboardMapping()
	wantMapping(t, r, map[uint64]int{entA: 0, entB: 1, entC: 2})
}

// Two entities linked to one player: the first one in pendingSBIDs wins, and no player is mapped twice.
func TestEnsureScoreboardMapping_NoDoubleMapping(t *testing.T) {
	r := linkReader()
	const entA2 = 0xF00000A2
	r.pendingSBIDs = [][4]byte{sbID(entA), sbID(entA2)}
	r.link(entA, objA)
	r.link(entA2, objA)
	for i := 0; i < 2; i++ { // a second call must not map the other one either
		r.ensureScoreboardMapping()
		wantMapping(t, r, map[uint64]int{entA: 0}, entA2)
	}
}

// In a round with links, an unlinked sbID still maps through the +4 rule.
func TestEnsureScoreboardMapping_UnlinkedUsesPlus4(t *testing.T) {
	r := linkReader()
	r.Header.Players[2].DissectID = []byte{0xC5, 0x00, 0x00, 0xF0} // sbID(entC) + 4
	r.pendingSBIDs = [][4]byte{sbID(entB), sbID(entA), sbID(entC)} // not in readPlayer order
	r.link(entA, objA)
	r.link(entB, objB)
	r.ensureScoreboardMapping()
	wantMapping(t, r, map[uint64]int{entA: 0, entB: 1, entC: 2})
}

// A linked sbID takes its player before an unlinked one whose +4 candidate is the same player,
// even when the unlinked one was seen first.
func TestEnsureScoreboardMapping_LinkBeatsPlus4(t *testing.T) {
	r := linkReader()
	r.Header.Players[0].DissectID = []byte{0xE5, 0x00, 0x00, 0xF0} // sbID(entE) + 4
	r.trackPlayerObject(objA, r.Header.Players[0])
	r.pendingSBIDs = [][4]byte{sbID(entE), sbID(entA), sbID(entB)}
	r.link(entA, objA)
	r.link(entB, objB)
	r.ensureScoreboardMapping()
	wantMapping(t, r, map[uint64]int{entA: 0, entB: 1}, entE)
}

// A reconnect gives the player a new dissect ID and a new player object; the entity linked to the
// old object still resolves to them (by username).
func TestEnsureScoreboardMapping_Reconnect(t *testing.T) {
	r := linkReader()
	r.Header.Players[0].DissectID = []byte{0x44, 0, 0, 0} // what readPlayer does when the username matches
	r.trackPlayerObject(0xF0000011, r.Header.Players[0])
	r.readPlayerOrder = append(r.readPlayerOrder, 0)
	r.pendingSBIDs = [][4]byte{sbID(entB), sbID(entA), sbID(entC)} // not in readPlayer order
	r.link(entA, objA)
	r.link(entB, objB)
	r.link(entC, objC)
	r.ensureScoreboardMapping()
	wantMapping(t, r, map[uint64]int{entA: 0, entB: 1, entC: 2})
}

// readFixtureWithout reads a fixture round as if the player had left before it: their player
// packet is hidden, so they keep a scoreboard entity (linked to a player object no player packet
// names) and a header entry, but readPlayer never reads them. This is the layout of a round after
// a player left the match, rewritten from one of our own fixtures.
func readFixtureWithout(t *testing.T, path, username string) *Reader {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	packet := append([]byte{0x22, 0x07, 0x94, 0x9B, 0xDC, byte(len(username))}, username...)
	if n := bytes.Count(r.b, packet); n != 1 {
		t.Fatalf("%s: %d player packets for %s, want 1", path, n, username)
	}
	hidden := bytes.Clone(packet)
	hidden[4] = 0x00
	r.b = bytes.Replace(r.b, packet, hidden, 1)
	if err := r.Read(); !Ok(err) {
		t.Fatal(err)
	}
	return r
}

type playerRound struct {
	score, assists uint32
}

func scoreboardByPlayer(r *Reader) map[string]playerRound {
	out := map[string]playerRound{}
	for i, p := range r.Header.Players {
		var row ScoreboardPlayer
		if i < len(r.Scoreboard.Players) {
			row = r.Scoreboard.Players[i]
		}
		out[p.Username] = playerRound{row.Score, row.AssistsFromRound}
	}
	return out
}

func killList(r *Reader) string {
	var b bytes.Buffer
	for _, u := range r.MatchFeedback {
		if u.Type == Kill {
			fmt.Fprintf(&b, "%s>%s ", u.Username, u.Target)
		}
	}
	return b.String()
}

// A round in which a player who left keeps a scoreboard entity: the other players keep their own
// score, assists and kills. Zipping the sbIDs with readPlayer order gave each player read after
// the one who left the next player's row and moved kills to match.
func TestScoreboardLink_LeftPlayerFixture(t *testing.T) {
	for _, tc := range []struct{ path, left string }{
		{"test/data/replays/valid/Y11S2/custom_solid_snake.rec", "WifiIsDown"}, // read second
		{"test/data/replays/valid/Y11S2/custom_solid_snake.rec", "flux_"},      // read first
		{"test/data/replays/valid/Y11S2/custom_early_plant.rec", "WifiIsDown"},
	} {
		t.Run(tc.path+"/"+tc.left, func(t *testing.T) {
			want := readFixture(t, tc.path)
			wantRows, wantKills := scoreboardByPlayer(want), killList(want)
			for name, w := range wantRows {
				if w.score == 0 {
					t.Fatalf("%s has no score in the unmodified round", name)
				}
			}
			for run := 0; run < 3; run++ {
				r := readFixtureWithout(t, tc.path, tc.left)
				got := scoreboardByPlayer(r)
				for name, w := range wantRows {
					if name == tc.left {
						continue
					}
					if got[name] != w {
						t.Errorf("run %d, %s: got %+v, want %+v", run, name, got[name], w)
					}
				}
				if g := got[tc.left]; g != (playerRound{}) {
					t.Errorf("run %d: the player who left got %+v", run, g)
				}
				if g := killList(r); g != wantKills {
					t.Errorf("run %d: kills\n got %s\nwant %s", run, g, wantKills)
				}
			}
		})
	}
}
