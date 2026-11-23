package dissect

import (
	"bytes"
	"encoding/binary"
)

// Loadout is the equipment a player had at the end of the preparation
// phase (after any operator swaps or gadget changes).
//
// Items are identified by the game's asset IDs, which are stable across
// seasons (e.g. the P12 is 1366019496 in both Y8S1 and Y11S2). The same
// weapon has the same ID on every operator who can equip it. Names are
// not resolved, as there is no reliable lookup table.
type Loadout struct {
	Primary   uint64 `json:"primary,omitempty"`   // primary weapon (or shield)
	Secondary uint64 `json:"secondary,omitempty"` // secondary weapon
	Gadget    uint64 `json:"gadget,omitempty"`    // selectable secondary gadget
	Ability   uint64 `json:"ability,omitempty"`   // operator's unique gadget/ability (extra gadget for Sentry/Striker)
}

// Inventory layout (verified on Y8S1, Y8S2, Y9S1 and Y11S2):
//
// Every player object owns an inventory object:
//
//	1B <player obj> E8D1E539 <inventory> 579FB57B
//
// The inventory references one item per slot, either as an edge
// (1B <inventory> <slot field> <item> <class>) or, when the inventory is
// still the current context (after a chain of empty slots), as a
// reference (1A <slot field> <item> <class>). Empty slots have a zero item
// handle and class. A newly created item is followed by its body, which
// holds the item's asset ID:
//
//	23 <item> 3BD73FC1 01 xx
//	1A D19C5716 <sub> 7882DE67
//	1B <sub> A57AAA29 <asset obj> <class>
//	23 <asset obj> 0E9EBE88 08 <asset id>
//
// Inventories are re-sent when an attacker swaps operators or a player
// changes their secondary gadget, so the last item per slot set during the
// preparation phase wins.
// The player object also precedes each player's username packet:
//
//	1B <player obj> 27C08DCA ... 22 07949BDC <username>
var (
	inventoryOwnerField = []byte{0xE8, 0xD1, 0xE5, 0x39}
	inventoryClass      = []byte{0x57, 0x9F, 0xB5, 0x7B}
	playerObjectField   = []byte{0x27, 0xC0, 0x8D, 0xCA}
	inventorySlotFields = [inventorySlots][]byte{
		{0xED, 0xE5, 0xAC, 0xBC}, // 1: drone (attack)
		{0x93, 0x27, 0xA1, 0x2B}, // 2: primary
		{0x03, 0x99, 0x40, 0x83}, // 3: secondary
		{0x4C, 0xD6, 0xA0, 0xC7}, // 4: unique ability
		{0xD8, 0x90, 0xB5, 0xF7}, // 5: secondary gadget
	}
	itemBodyMarkers = []struct {
		offset  int
		pattern []byte
	}{
		{0, []byte{0x23}},
		{9, []byte{0x3B, 0xD7, 0x3F, 0xC1, 0x01}},
		{15, []byte{0x1A, 0xD1, 0x9C, 0x57, 0x16}},
		{28, []byte{0x78, 0x82, 0xDE, 0x67, 0x1B}},
		{41, []byte{0xA5, 0x7A, 0xAA, 0x29}},
		{57, []byte{0x23}},
		{66, []byte{0x0E, 0x9E, 0xBE, 0x88, 0x08}},
	}
)

const (
	inventorySlots    = 5
	handleSize        = 8
	slotTokenSize     = 4 + handleSize + 4 // field, item handle, class
	itemBodySize      = 79
	playerObjectRange = 96 // max distance from the player object field to the username packet
)

type inventory struct {
	slots      [inventorySlots]uint64 // item handles
	lastUpdate int
}

type inventoryPlayer struct {
	dissectID []byte
	username  string
}

type inventoryState struct {
	owners      map[uint64]uint64          // inventory handle -> player object handle
	inventories map[uint64]*inventory      // inventory handle -> slots
	order       []uint64                   // inventory handles in first-seen order
	assets      map[uint64]uint64          // item handle -> asset ID
	players     map[uint64]inventoryPlayer // player object handle -> player
	refOwner    uint64                     // inventory a reference-form slot token may belong to
	refNext     int                        // offset at which that reference-form token must start
}

func newInventoryState() *inventoryState {
	return &inventoryState{
		owners:      make(map[uint64]uint64),
		inventories: make(map[uint64]*inventory),
		assets:      make(map[uint64]uint64),
		players:     make(map[uint64]inventoryPlayer),
		refNext:     -1,
	}
}

func (r *Reader) listenLoadouts() {
	if r.Header.CodeVersion < Y8S1 {
		return
	}
	r.inventory = newInventoryState()
	r.Listen(inventoryOwnerField, readInventoryOwner)
	for i := range inventorySlotFields {
		slot := i
		r.Listen(inventorySlotFields[i], func(r *Reader) error {
			readInventorySlot(r, slot)
			return nil
		})
	}
}

func readHandle(b []byte) uint64 {
	return binary.LittleEndian.Uint64(b)
}

// readInventoryOwner reads 1B <player obj> E8D1E539 <inventory> 579FB57B.
func readInventoryOwner(r *Reader) error {
	inv := r.inventory
	start := r.offset - len(inventoryOwnerField)
	if inv == nil || start < handleSize+1 || start+slotTokenSize > len(r.b) || r.b[start-handleSize-1] != 0x1B {
		return nil
	}
	if !bytes.Equal(r.b[start+4+handleSize:start+slotTokenSize], inventoryClass) {
		return nil
	}
	owner := readHandle(r.b[start-handleSize : start])
	handle := readHandle(r.b[start+4 : start+4+handleSize])
	if owner == 0 || handle == 0 {
		return nil
	}
	inv.owners[handle] = owner
	inv.refOwner = handle
	inv.refNext = -1
	return nil
}

func readInventorySlot(r *Reader, slot int) {
	inv := r.inventory
	start := r.offset - len(inventorySlotFields[slot])
	if inv == nil || start < 1 || start+slotTokenSize > len(r.b) {
		return
	}
	// Loadouts can only change during the preparation phase. Inventories
	// are emptied once the round is over, which must not erase them.
	if r.clock.started && r.clock.phase != PrepPhase {
		return
	}
	var handle uint64
	switch {
	case start >= handleSize+1 && r.b[start-handleSize-1] == 0x1B:
		handle = readHandle(r.b[start-handleSize : start])
		if _, ok := inv.owners[handle]; !ok {
			return
		}
	case r.b[start-1] == 0x1A && start-1 == inv.refNext && inv.refOwner != 0:
		handle = inv.refOwner
	default:
		return
	}
	item := readHandle(r.b[start+4 : start+4+handleSize])
	end := start + slotTokenSize
	if item == 0 {
		if !bytes.Equal(r.b[start+4+handleSize:end], []byte{0, 0, 0, 0}) {
			return
		}
		// an empty slot keeps the inventory as the current context
		inv.refOwner = handle
		inv.refNext = end
	} else {
		inv.refNext = -1
		if asset, ok := readItemBody(r.b, end, item); ok {
			inv.assets[item] = asset
		}
	}
	slots, ok := inv.inventories[handle]
	if !ok {
		slots = &inventory{}
		inv.inventories[handle] = slots
		inv.order = append(inv.order, handle)
	}
	slots.slots[slot] = item
	slots.lastUpdate = start
}

// readItemBody returns the asset ID of the item created at b[off:].
func readItemBody(b []byte, off int, item uint64) (uint64, bool) {
	if off+itemBodySize > len(b) {
		return 0, false
	}
	body := b[off : off+itemBodySize]
	for _, m := range itemBodyMarkers {
		if !bytes.Equal(body[m.offset:m.offset+len(m.pattern)], m.pattern) {
			return 0, false
		}
	}
	if readHandle(body[1:9]) != item ||
		!bytes.Equal(body[20:28], body[33:41]) ||
		!bytes.Equal(body[45:53], body[58:66]) {
		return 0, false
	}
	return readHandle(body[71:79]), true
}

// playerObjectBefore returns the player object handle written shortly before
// the username packet starting at offset, or 0 if it cannot be found.
func (r *Reader) playerObjectBefore(offset int) uint64 {
	if r.inventory == nil || offset > len(r.b) {
		return 0
	}
	from := offset - playerObjectRange
	if from < 0 {
		from = 0
	}
	i := bytes.LastIndex(r.b[from:offset], playerObjectField)
	if i < 0 {
		return 0
	}
	i += from
	if i < handleSize+1 || r.b[i-handleSize-1] != 0x1B {
		return 0
	}
	return readHandle(r.b[i-handleSize : i])
}

func (r *Reader) trackPlayerObject(handle uint64, p Player) {
	if r.inventory == nil || handle == 0 {
		return
	}
	r.inventory.players[handle] = inventoryPlayer{
		dissectID: append([]byte(nil), p.DissectID...),
		username:  p.Username,
	}
}

// playerIndexByObject returns the index of the player owning the player object, or -1.
func (r *Reader) playerIndexByObject(handle uint64) int {
	if r.inventory == nil {
		return -1
	}
	p, ok := r.inventory.players[handle]
	if !ok {
		return -1
	}
	if len(p.dissectID) == 4 {
		if i := r.PlayerIndexByID(p.dissectID); i > -1 {
			return i
		}
	}
	return r.PlayerIndexByUsername(p.username)
}

// applyLoadouts assigns each player the final contents of their inventory.
func (r *Reader) applyLoadouts() {
	inv := r.inventory
	if inv == nil {
		return
	}
	latest := make(map[int]*inventory)
	for _, handle := range inv.order {
		i := r.playerIndexByObject(inv.owners[handle])
		if i < 0 {
			continue
		}
		slots := inv.inventories[handle]
		if prev, ok := latest[i]; ok && prev.lastUpdate > slots.lastUpdate {
			continue
		}
		latest[i] = slots
	}
	for i, slots := range latest {
		asset := func(slot int) uint64 {
			return inv.assets[slots.slots[slot]]
		}
		l := Loadout{
			Primary:   asset(1),
			Secondary: asset(2),
			Ability:   asset(3),
			Gadget:    asset(4),
		}
		if l != (Loadout{}) {
			r.Header.Players[i].Loadout = &l
		}
	}
}

// WeaponSlot is the Loadout slot that holds an item.
type WeaponSlot string

const (
	PrimarySlot   WeaponSlot = "primary"
	SecondarySlot WeaponSlot = "secondary"
	GadgetSlot    WeaponSlot = "gadget"
	AbilitySlot   WeaponSlot = "ability"
)

// Slot returns the slot that holds the asset. It checks primary, secondary, gadget and ability in
// that order, so the first one wins for an asset in two slots. It returns "" for asset 0, an asset
// in no slot and a nil Loadout.
func (l *Loadout) Slot(asset uint64) WeaponSlot {
	if l == nil || asset == 0 {
		return ""
	}
	switch asset {
	case l.Primary:
		return PrimarySlot
	case l.Secondary:
		return SecondarySlot
	case l.Gadget:
		return GadgetSlot
	case l.Ability:
		return AbilitySlot
	}
	return ""
}

// classifyKillWeapons sets each Kill's WeaponSlot from the loadout of the player the kill packet
// names (the finisher). When reconciliation credited the kill to the downer, that is not the
// Username. It runs after applyLoadouts.
func (r *Reader) classifyKillWeapons() {
	for i := range r.MatchFeedback {
		u := &r.MatchFeedback[i]
		if u.Type != Kill {
			continue
		}
		killer := u.feedKiller
		if killer == "" {
			killer = u.Username
		}
		if idx := r.PlayerIndexByUsername(killer); idx >= 0 {
			u.WeaponSlot = r.Header.Players[idx].Loadout.Slot(u.WeaponID)
		}
	}
}
