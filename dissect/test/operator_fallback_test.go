package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/redraskal/r6-dissect/dissect"
)

// the name derived from a header roleName is the operator constant's name, so that adding the
// constant later does not change what a consumer sees.
func TestOperatorFallbackMatchesConstants(t *testing.T) {
	files, err := filepath.Glob("data/replays/valid/*/*.rec")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		r, err := dissect.NewReader(f)
		if err == nil {
			err = r.Read()
		}
		f.Close()
		if err != nil && !dissect.Ok(err) {
			t.Fatalf("%s: %v", file, err)
		}
		for _, p := range r.Header.Players {
			// IQ is the one all-caps constant
			if p.RoleName == "" || p.Operator == dissect.IQ || p.Operator == 0 {
				continue
			}
			got := dissect.Player{Operator: 1, RoleName: p.RoleName}.OperatorName()
			if got != p.Operator.String() {
				t.Errorf("%s: roleName %q derives %q, constant is %q", file, p.RoleName, got, p.Operator.String())
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no header player with a roleName was checked")
	}
	t.Logf("checked %d players", checked)
}
