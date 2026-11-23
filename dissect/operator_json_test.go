package dissect

import (
	"bytes"
	"encoding/json"
	"testing"
)

// An operator that is not renamed from the header roleName marshals byte for byte like the plain
// struct, so known operators keep their key order (rev-47 F1).
func TestJSON_UnrenamedOperatorKeepsDefaultBytes(t *testing.T) {
	type plainPlayer Player
	type plainUpdate MatchUpdate
	for _, p := range []Player{{Operator: Ace, RoleName: "NOOR"}, {Operator: 123}} {
		got, _ := json.Marshal(p)
		want, _ := json.Marshal(plainPlayer(p))
		if !bytes.Equal(got, want) {
			t.Errorf("player %v: got %s, want %s", p.Operator, got, want)
		}
	}
	for _, u := range []MatchUpdate{{Type: OperatorSwap, Operator: Ace}, {Type: OperatorSwap, Operator: 123}, {Type: Kill}} {
		got, _ := json.Marshal(u)
		want, _ := json.Marshal(plainUpdate(u))
		if !bytes.Equal(got, want) {
			t.Errorf("update %v/%v: got %s, want %s", u.Type, u.Operator, got, want)
		}
	}
}
