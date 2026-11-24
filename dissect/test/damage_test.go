package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/redraskal/r6-dissect/dissect"
)

func damageByUser(t *testing.T, file string) map[string]*int {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := dissect.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Read(); !dissect.Ok(err) {
		t.Fatal(err)
	}
	out := make(map[string]*int)
	for _, s := range r.PlayerStats() {
		out[s.Username] = s.DamageTaken
	}
	return out
}

// the plan's player numbers are the header order of the fixtures' players.
func TestDamageTaken_Y11S2Fixtures(t *testing.T) {
	cases := map[string]map[string]int{
		"Y11S2/custom_early_plant.rec": {"NickB2A": 150, "Ark6444.HKM": 150, "KoolAidAF1": 110, "MyzCNCBunny": 135, "HokiesHi": 150,
			"O.ni.": 100, "hattttttttt": 110, "WifiIsDown": 0, "flux_": 100, "RYCEEEEEEEEEEEE": 40},
		"Y11S2/custom_solid_snake.rec": {"NickB2A": 110, "Ark6444.HKM": 125, "KoolAidAF1": 100, "MyzCNCBunny": 110, "HokiesHi": 110,
			"O.ni.": 125, "hattttttttt": 110, "WifiIsDown": 0, "flux_": 100, "RYCEEEEEEEEEEEE": 110},
	}
	for file, want := range cases {
		got := damageByUser(t, filepath.Join("data", "replays", "valid", file))
		for user, w := range want {
			if got[user] == nil || *got[user] != w {
				t.Errorf("%s %s: want %d, got %v", file, user, w, got[user])
			}
		}
	}
}

func TestDamageTaken_OlderSeasonsOmitted(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("data", "replays", "valid", "Y[89]S[12]", "*.rec"))
	if len(files) == 0 {
		t.Fatal("no older-season fixtures found")
	}
	for _, file := range files {
		for user, v := range damageByUser(t, file) {
			if v != nil {
				t.Errorf("%s %s: damageTaken %d before Y10S4", file, user, *v)
			}
		}
	}
}
