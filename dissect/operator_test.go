package dissect

import "testing"

func TestNoorIsKnownDefender(t *testing.T) {
	if got := Noor.String(); got != "Noor" {
		t.Errorf("Noor.String() = %q", got)
	}
	if got := Noor.Role(); got != Defense {
		t.Errorf("Noor.Role() = %q, want Defense", got)
	}
}

// A replay from a newer season can contain an operator this version does not
// know. That must not panic: Role returns "" and the caller decides.
func TestUnknownOperatorRoleIsEmpty(t *testing.T) {
	unknown := Operator(999999999999)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Role() panicked on an unknown operator: %v", r)
		}
	}()
	if got := unknown.Role(); got != "" {
		t.Errorf("unknown.Role() = %q, want \"\"", got)
	}
}
