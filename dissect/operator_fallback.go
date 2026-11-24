package dissect

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/rs/zerolog/log"
	"golang.org/x/text/unicode/norm"
)

// Known reports whether the operator ID is in the generated operator table.
func (i Operator) Known() bool {
	_, ok := _Operator_map[i]
	return ok
}

// operatorNameFromRoleName converts a header roleName ("SOLID SNAKE", "CAPITÃO") to the style of
// the Operator constants ("SolidSnake", "Capitao"). It returns "" if roleName has no letters or digits.
func operatorNameFromRoleName(roleName string) string {
	var sb strings.Builder
	upper := true
	for _, r := range norm.NFKD.String(roleName) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case r == 'Ø' || r == 'ø':
			r = 'o'
		case r == 'ß':
			if upper {
				sb.WriteString("Ss")
			} else {
				sb.WriteString("ss")
			}
			upper = false
			continue
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			upper = true
			continue
		}
		if upper {
			sb.WriteRune(unicode.ToUpper(r))
		} else {
			sb.WriteRune(unicode.ToLower(r))
		}
		upper = false
	}
	return sb.String()
}

// OperatorName is the name of the player's operator. For an operator ID that is not in the operator
// table (a season newer than this build), it is derived from the header roleName; otherwise it is
// Operator.String(). The ID is never changed.
func (p Player) OperatorName() string {
	if !p.Operator.Known() {
		if name := operatorNameFromRoleName(p.RoleName); name != "" {
			return name
		}
	}
	return p.Operator.String()
}

// MarshalJSON overrides only the operator key, and only for a renamed operator; any other player
// marshals exactly as the plain struct does.
func (p Player) MarshalJSON() ([]byte, error) {
	type plain Player
	name := p.OperatorName()
	if name == p.Operator.String() {
		return json.Marshal(plain(p))
	}
	return json.Marshal(struct {
		plain
		Operator stringerIntMarshal `json:"operator"`
	}{plain(p), stringerIntMarshal{Name: name, ID: int(p.Operator)}})
}

// MarshalJSON prints a swap to an operator newer than the operator table with the name derived from the header.
func (u MatchUpdate) MarshalJSON() ([]byte, error) {
	type plain MatchUpdate
	if u.Operator == 0 || u.operatorName == "" {
		return json.Marshal(plain(u))
	}
	return json.Marshal(struct {
		plain
		Operator stringerIntMarshal `json:"operator"`
	}{plain(u), stringerIntMarshal{Name: u.operatorName, ID: int(u.Operator)}})
}

// nameUnknownSwapOperators names an OperatorSwap to an unknown operator ID when it is that player's
// final operator, because the header roleName names the operator the player ended the round on.
func (r *Reader) nameUnknownSwapOperators() {
	for i, u := range r.MatchFeedback {
		if u.Type != OperatorSwap || u.Operator == 0 || u.Operator.Known() {
			continue
		}
		for _, p := range r.Header.Players {
			if p.Username == u.Username && p.Operator == u.Operator {
				r.MatchFeedback[i].operatorName = operatorNameFromRoleName(p.RoleName)
				break
			}
		}
	}
}

// warnUnknownOperators tells the maintainer which operator IDs to add to the Operator constants.
func (r *Reader) warnUnknownOperators() {
	for _, p := range r.Header.Players {
		if p.Operator == 0 || p.Operator.Known() {
			continue
		}
		log.Warn().
			Uint64("op", uint64(p.Operator)).
			Str("roleName", p.RoleName).
			Str("name", p.OperatorName()).
			Str("username", p.Username).
			Msg("operator id not in the operator table; add it to dissect/header.go")
	}
}
