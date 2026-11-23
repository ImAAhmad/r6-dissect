package dissect

import (
	"encoding/json"
	"testing"
)

// TestMapNames pins worldid -> name for the modernized and new maps.
func TestMapNames(t *testing.T) {
	cases := []struct {
		id   int
		name string
	}{
		{398899676157, "FortressY10"},
		{418126004176, "ConsulateY10"},
		{409325881472, "VillaY10"},
		{423767322185, "TowerY10"},
		{436375283234, "CoastlineY10"},
		{419965653950, "CalypsoCasino"},
	}
	for _, c := range cases {
		m := Map(c.id)
		if got := m.String(); got != c.name {
			t.Errorf("Map(%d).String() = %q, want %q", c.id, got, c.name)
		}
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		var x struct {
			Name string `json:"name"`
			ID   int    `json:"id"`
		}
		if err := json.Unmarshal(b, &x); err != nil {
			t.Fatal(err)
		}
		if x.Name != c.name || x.ID != c.id {
			t.Errorf("Map(%d) JSON = %s, want name %q id %d", c.id, b, c.name, c.id)
		}
		var back Map
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatal(err)
		}
		if back != m {
			t.Errorf("Map(%d) JSON round trip = %d", c.id, back)
		}
	}
}

// TestMapUnknown pins the unknown-id fallback: it still parses, keeps a
// non-empty unique name (R6Stats rejects an empty one), and is not Known.
func TestMapUnknown(t *testing.T) {
	if Map(1).Known() {
		t.Error("Map(1).Known() = true, want false")
	}
	if !Map(398899676157).Known() {
		t.Error("FortressY10 Known() = false, want true")
	}
	if !VillaY10.Known() || !CalypsoCasino.Known() {
		t.Error("new constants must be Known")
	}
	b, err := json.Marshal(Map(1))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"name":"Map(1)","id":1}`; string(b) != want {
		t.Errorf("Map(1) JSON = %s, want %s", b, want)
	}
}
