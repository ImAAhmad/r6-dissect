package dissect

import (
	"bytes"
	"encoding/binary"
	"testing"
)

type inventoryBuilder struct {
	bytes.Buffer
}

func (b *inventoryBuilder) handle(h uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], h)
	b.Write(buf[:])
}

func (b *inventoryBuilder) owner(player, inv uint64) {
	b.WriteByte(0x1B)
	b.handle(player)
	b.Write(inventoryOwnerField)
	b.handle(inv)
	b.Write(inventoryClass)
}

func (b *inventoryBuilder) emptySlot(inv uint64, slot int, ref bool) {
	if ref {
		b.WriteByte(0x1A)
	} else {
		b.WriteByte(0x1B)
		b.handle(inv)
	}
	b.Write(inventorySlotFields[slot])
	b.Write(make([]byte, 12))
}

func (b *inventoryBuilder) item(inv uint64, slot int, ref bool, item, asset uint64) {
	if ref {
		b.WriteByte(0x1A)
	} else {
		b.WriteByte(0x1B)
		b.handle(inv)
	}
	b.Write(inventorySlotFields[slot])
	b.handle(item)
	b.Write([]byte{0x0E, 0xAA, 0x2B, 0xE0})
	if asset == 0 { // item without an asset sub-object
		b.Write([]byte{0x23})
		b.handle(item)
		b.Write([]byte{0x3B, 0xD7, 0x3F, 0xC1, 0x01, 0x00, 0x1A, 0xA5, 0x7A, 0xAA, 0x29})
		return
	}
	sub, assetObj := item+0x100, item+0x200
	b.WriteByte(0x23)
	b.handle(item)
	b.Write([]byte{0x3B, 0xD7, 0x3F, 0xC1, 0x01, 0x00, 0x1A, 0xD1, 0x9C, 0x57, 0x16})
	b.handle(sub)
	b.Write([]byte{0x78, 0x82, 0xDE, 0x67, 0x1B})
	b.handle(sub)
	b.Write([]byte{0xA5, 0x7A, 0xAA, 0x29})
	b.handle(assetObj)
	b.Write([]byte{0xEC, 0x5F, 0xF8, 0xFA, 0x23})
	b.handle(assetObj)
	b.Write([]byte{0x0E, 0x9E, 0xBE, 0x88, 0x08})
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], asset)
	b.Write(buf[:])
	b.Write([]byte{0x22, 0xB2, 0x97, 0xEF, 0x0C, 0x08, 0, 0, 0, 0, 0, 0, 0, 0})
}

// runInventoryListeners calls the inventory listeners at every pattern match, in order.
func runInventoryListeners(r *Reader) {
	patterns := append([][]byte{inventoryOwnerField}, inventorySlotFields[:]...)
	for i := 0; i < len(r.b); i++ {
		for p, pattern := range patterns {
			if !bytes.HasPrefix(r.b[i:], pattern) {
				continue
			}
			r.offset = i + len(pattern)
			if p == 0 {
				_ = readInventoryOwner(r)
			} else {
				readInventorySlot(r, p-1)
			}
		}
	}
}

func TestLoadouts(t *testing.T) {
	const (
		defenderObj, defenderInv = 0xF00668C2, 0xF0066F06
		attackerObj, attackerInv = 0xF0066F7F, 0xF0066F6B
	)
	var b inventoryBuilder
	// defender: empty drone slot, so the primary is a reference
	b.owner(defenderObj, defenderInv)
	b.emptySlot(defenderInv, 0, false)
	b.item(defenderInv, 1, true, 0xF007C21B, 372271879863)
	b.item(defenderInv, 2, false, 0xF007C2A5, 78526932169)
	b.item(defenderInv, 3, false, 0xF007C122, 371820933471)
	b.item(defenderInv, 4, false, 0xF007C119, 133651070258)
	// attacker: drone, then operator swap to an operator without an ability asset
	b.owner(attackerObj, attackerInv)
	b.item(attackerInv, 0, false, 0xF007FAD5, 249219679725)
	b.item(attackerInv, 1, false, 0xF007FBDD, 1366019220)
	b.item(attackerInv, 2, false, 0xF007FA5C, 1366019472)
	b.item(attackerInv, 3, false, 0xF007FADF, 201523838482)
	b.item(attackerInv, 4, false, 0xF007F815, 133651070288)
	b.item(attackerInv, 1, false, 0xF007B4BB, 133651069198)
	b.item(attackerInv, 2, false, 0xF007BB3A, 1366019496)
	b.item(attackerInv, 3, false, 0xF007BBB8, 0)
	// an unrelated object using a slot field must be ignored
	b.item(0xF0001234, 1, false, 0xF0005678, 1)
	// a reference that does not follow an empty slot of the inventory must be ignored
	b.item(0, 4, true, 0xF0009999, 2)

	r := &Reader{
		b: b.Bytes(),
		Header: Header{Players: []Player{
			{Username: "defender", DissectID: []byte{1, 0, 0, 0}},
			{Username: "attacker", DissectID: []byte{2, 0, 0, 0}},
		}},
		inventory: newInventoryState(),
	}
	r.trackPlayerObject(defenderObj, r.Header.Players[0])
	r.trackPlayerObject(attackerObj, r.Header.Players[1])
	runInventoryListeners(r)

	// inventories are emptied after the round, which must not erase loadouts
	r.clock = roundClock{started: true, phase: PostPlantPhase}
	var end inventoryBuilder
	end.emptySlot(defenderInv, 0, false)
	end.emptySlot(defenderInv, 1, true)
	end.emptySlot(defenderInv, 2, true)
	r.b = end.Bytes()
	runInventoryListeners(r)

	r.applyLoadouts()

	want := []Loadout{
		{Primary: 372271879863, Secondary: 78526932169, Ability: 371820933471, Gadget: 133651070258},
		{Primary: 133651069198, Secondary: 1366019496, Gadget: 133651070288},
	}
	for i, w := range want {
		got := r.Header.Players[i].Loadout
		if got == nil || *got != w {
			t.Errorf("player %d: got loadout %+v, want %+v", i, got, w)
		}
	}
}

func TestPlayerObjectBefore(t *testing.T) {
	var b inventoryBuilder
	b.WriteByte(0x1B)
	b.handle(0xF00668CE)
	b.Write(playerObjectField)
	b.Write(make([]byte, 16))
	b.Write([]byte{0x22, 0xDC, 0xC8, 0x86, 0x8D, 0x01, 0x01})
	start := b.Len()
	b.Write([]byte{0x22, 0x07, 0x94, 0x9B, 0xDC})
	r := &Reader{b: b.Bytes(), inventory: newInventoryState()}
	if got := r.playerObjectBefore(start); got != 0xF00668CE {
		t.Fatalf("got player object %x, want f00668ce", got)
	}
}

func TestAtkOpSwapByPlayerObject(t *testing.T) {
	const playerObj = 0xF0066F72
	var b inventoryBuilder
	b.WriteByte(0x23)
	b.handle(playerObj)
	b.Write([]byte{0xF9, 0x39, 0x11, 0xF2, 0x08})
	b.handle(358196448678)
	b.Write([]byte{0x22, 0xA9, 0x26, 0x0B, 0xE4, 0x08})
	b.handle(uint64(Grim))
	b.Write(make([]byte, 16))
	r := &Reader{
		b: b.Bytes(),
		Header: Header{
			CodeVersion: Y11S1,
			Players:     []Player{{Username: "attacker", DissectID: []byte{2, 0, 0, 0}, Operator: Twitch}},
		},
		inventory: newInventoryState(),
	}
	r.trackPlayerObject(playerObj, r.Header.Players[0])
	r.offset = 1 + 8 + 5 + 8 + 5 // just after the A9260BE4 pattern
	_ = readAtkOpSwap(r)
	if r.Header.Players[0].Operator != Grim {
		t.Fatalf("got operator %v, want Grim", r.Header.Players[0].Operator)
	}
	if len(r.MatchFeedback) != 1 || r.MatchFeedback[0].Type != OperatorSwap || r.MatchFeedback[0].Operator != Grim {
		t.Fatalf("unexpected match feedback: %+v", r.MatchFeedback)
	}
}
