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
