package dissect

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func TestOperatorNameFromRoleName(t *testing.T) {
	for in, want := range map[string]string{
		"NOOR": "Noor", "SOLID SNAKE": "SolidSnake", "CAPITÃO": "Capitao", "JÄGER": "Jager",
		"NØKK": "Nokk", "SKOPÓS": "Skopos", "TUBARÃO": "Tubarao", "": "", "  ": "",
		"STRASSE": "Strasse", "ß": "Ss",
	} {
		if got := operatorNameFromRoleName(in); got != want {
			t.Errorf("operatorNameFromRoleName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlayerOperatorName(t *testing.T) {
	cases := []struct {
		name string
		p    Player
		want string
	}{
		{"known id ignores roleName", Player{Operator: Ace, RoleName: "NOOR"}, "Ace"},
		{"unknown id uses roleName", Player{Operator: 123, RoleName: "NOOR"}, "Noor"},
		{"unknown id, no roleName", Player{Operator: 123}, "Operator(123)"},
		{"unknown id, blank roleName", Player{Operator: 123, RoleName: " "}, "Operator(123)"},
		{"unknown id, accents and spaces", Player{Operator: 123, RoleName: "SOLID SNAKE"}, "SolidSnake"},
		{"recruit is known", Player{Operator: Recruit, RoleName: "RECRUIT"}, "Recruit"},
		{"recruit ignores roleName", Player{Operator: Recruit, RoleName: "NOOR"}, "Recruit"},
	}
	for _, c := range cases {
		if got := c.p.OperatorName(); got != c.want {
			t.Errorf("%s: OperatorName() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPlayerJSON_UnknownOperatorNamedFromRoleName(t *testing.T) {
	b, err := json.Marshal(Player{Username: "u", Operator: 123, RoleName: "NOOR", TeamIndex: 1})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"operator":{"name":"Noor","id":123}`, `"roleName":"NOOR"`, `"username":"u"`, `"teamIndex":1`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
	if n := strings.Count(s, `"operator"`); n != 1 {
		t.Errorf("want one operator key, got %d in %s", n, s)
	}
	var back Player
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Operator != 123 || back.RoleName != "NOOR" {
		t.Errorf("round trip lost fields: %+v", back)
	}
}

func TestPlayerJSON_KnownOperatorUnchanged(t *testing.T) {
	b, _ := json.Marshal(Player{Username: "u", Operator: Ace, RoleName: "NOOR"})
	if !strings.Contains(string(b), `"operator":{"name":"Ace","id":104189664390}`) {
		t.Errorf("got %s", b)
	}
	b, _ = json.Marshal(Player{Username: "u", Operator: 123})
	if !strings.Contains(string(b), `"operator":{"name":"Operator(123)","id":123}`) {
		t.Errorf("got %s", b)
	}
}

func TestNameUnknownSwapOperators(t *testing.T) {
	r := &Reader{
		Header: Header{Players: []Player{{Username: "a", Operator: 123, RoleName: "NOOR"}}},
		MatchFeedback: []MatchUpdate{
			{Type: OperatorSwap, Username: "a", Operator: 456}, // not the final pick
			{Type: OperatorSwap, Username: "a", Operator: 123},
			{Type: OperatorSwap, Username: "a", Operator: Ace},
			{Type: Kill, Username: "a", Target: "b"},
		},
	}
	r.nameUnknownSwapOperators()
	b, err := json.Marshal(r.MatchFeedback)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"operator":{"name":"Operator(456)","id":456}`,
		`"operator":{"name":"Noor","id":123}`,
		`"operator":{"name":"Ace","id":104189664390}`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
	if n := strings.Count(s, `"operator"`); n != 3 {
		t.Errorf("want 3 operator keys (the kill has none), got %d in %s", n, s)
	}
}

// a swap to a known id keeps the table name even when the player's roleName names something else,
// and only swap events are renamed.
func TestNameUnknownSwapOperators_KnownAndNonSwap(t *testing.T) {
	r := &Reader{
		Header: Header{Players: []Player{{Username: "a", Operator: Ace, RoleName: "NOOR"}, {Username: "b", Operator: 123, RoleName: "NOOR"}}},
		MatchFeedback: []MatchUpdate{
			{Type: OperatorSwap, Username: "a", Operator: Ace},
			{Type: Other, Username: "b", Operator: 123},
		},
	}
	r.nameUnknownSwapOperators()
	b, _ := json.Marshal(r.MatchFeedback)
	s := string(b)
	for _, want := range []string{`"operator":{"name":"Ace","id":104189664390}`, `"operator":{"name":"Operator(123)","id":123}`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
}

func TestNameUnknownSwapOperators_NoRoleName(t *testing.T) {
	r := &Reader{
		Header:        Header{Players: []Player{{Username: "a", Operator: 123}}},
		MatchFeedback: []MatchUpdate{{Type: OperatorSwap, Username: "a", Operator: 123}},
	}
	r.nameUnknownSwapOperators()
	b, _ := json.Marshal(r.MatchFeedback)
	if !strings.Contains(string(b), `"operator":{"name":"Operator(123)","id":123}`) {
		t.Errorf("got %s", b)
	}
}

func TestPlayerStats_UnknownOperatorName(t *testing.T) {
	r := &Reader{Header: Header{Players: []Player{{Username: "a", Operator: 123, RoleName: "NOOR"}}}}
	if got := r.PlayerStats()[0].Operator; got != "Noor" {
		t.Errorf("PlayerStats operator = %q, want Noor", got)
	}
}

// forgetOperator removes an operator from the generated tables for the test, as if it were newer than the table.
func forgetOperator(t *testing.T, ops ...Operator) {
	t.Helper()
	for _, o := range ops {
		name, ok := _Operator_map[o]
		role, hasRole := _operatorRoles[o]
		delete(_Operator_map, o)
		delete(_operatorRoles, o)
		t.Cleanup(func() {
			if ok {
				_Operator_map[o] = name
			}
			if hasRole {
				_operatorRoles[o] = role
			}
		})
	}
}

// the simulation: an operator the table does not know, in a real replay.
func TestReplay_UnknownOperatorsNamedFromHeader(t *testing.T) {
	const skopos, twitch = Operator(386098331713), Operator(92270644111)
	forgetOperator(t, skopos, twitch)
	var logs bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(&logs)
	t.Cleanup(func() { log.Logger = prev })
	f, err := os.Open("test/data/replays/valid/Y11S2/custom_solid_snake.rec")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Read(); !Ok(err) {
		t.Fatal(err)
	}
	b, err := json.Marshal(struct {
		Header        Header        `json:"header"`
		MatchFeedback []MatchUpdate `json:"matchFeedback"`
	}{r.Header, r.MatchFeedback})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"operator":{"name":"Skopos","id":386098331713}`,
		`"operator":{"name":"Twitch","id":92270644111}`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(s, "Operator(") {
		t.Errorf("an unknown operator is still printed by id")
	}
	for _, want := range []string{`"op":386098331713`, `"roleName":"SKOPÓS"`, `"name":"Skopos"`, `"op":92270644111`, `"roleName":"TWITCH"`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("Read did not warn with %s", want)
		}
	}
	// the swap event to Twitch is the player's final pick
	swaps := 0
	for _, u := range r.MatchFeedback {
		if u.Type == OperatorSwap && u.Operator == twitch {
			swaps++
			if u.operatorName != "Twitch" {
				t.Errorf("swap to the final unknown operator is named %q", u.operatorName)
			}
		}
	}
	if swaps == 0 {
		t.Error("fixture has no swap to the forgotten operator")
	}
	// team roles still come from the known operators
	if r.Header.Teams[0].Role == "" || r.Header.Teams[1].Role == "" || r.Header.Teams[0].Role == r.Header.Teams[1].Role {
		t.Errorf("team roles not derived: %q %q", r.Header.Teams[0].Role, r.Header.Teams[1].Role)
	}
	stats := r.PlayerStats()
	found := false
	for _, ps := range stats {
		if ps.Operator == "Skopos" || ps.Operator == "Twitch" {
			found = true
		}
		if strings.HasPrefix(ps.Operator, "Operator(") {
			t.Errorf("round stats print %q", ps.Operator)
		}
	}
	if !found {
		t.Error("round stats never name the forgotten operators")
	}
}

func TestWarnUnknownOperators(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = prev })
	r := &Reader{Header: Header{Players: []Player{
		{Username: "new", Operator: 999, RoleName: "SOLID SNAKE"},
		{Username: "known", Operator: Ace, RoleName: "ACE"},
		{Username: "recruit", Operator: Recruit},
		{Username: "gone"},
	}}}
	r.warnUnknownOperators()
	out := buf.String()
	if n := strings.Count(out, "operator id not in the operator table"); n != 1 {
		t.Fatalf("want one warning, got %d: %s", n, out)
	}
	for _, want := range []string{`"op":999`, `"roleName":"SOLID SNAKE"`, `"name":"SolidSnake"`, `"username":"new"`} {
		if !strings.Contains(out, want) {
			t.Errorf("warning lacks %s: %s", want, out)
		}
	}
}
