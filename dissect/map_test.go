package dissect

import "testing"

// Oregon was modernized in Y11S1 and got a new map ID; replays recorded on it
// showed up as "Map(434715462383)".
func TestOregonY11IsKnown(t *testing.T) {
	if got := Map(434715462383).String(); got != "OregonY11" {
		t.Errorf("Map(434715462383).String() = %q, want OregonY11", got)
	}
	if got := Oregon.String(); got != "Oregon" {
		t.Errorf("old Oregon renamed to %q", got)
	}
}

// Villa, Calypso Casino and Coastline replays recorded in Y11S3 carry IDs
// the parser didn't know (checked against the in-game replay list).
func TestY11S3MapsAreKnown(t *testing.T) {
	for id, want := range map[Map]string{
		409325881472: "VillaY11",
		419965653950: "CalypsoCasino",
		436375283234: "CoastlineY11",
	} {
		if got := id.String(); got != want {
			t.Errorf("Map(%d).String() = %q, want %q", int64(id), got, want)
		}
	}
	if Villa.String() != "Villa" || Coastline.String() != "Coastline" {
		t.Error("old Villa/Coastline names changed")
	}
}
