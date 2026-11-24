package test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/redraskal/r6-dissect/dissect"
)

// TestAcceptance_Replays checks invariants of the parsed output on real match folders that are too
// large to commit. Set R6_DISSECT_REPLAY_DIRS to a list of match folders separated by ':'.
func TestAcceptance_Replays(t *testing.T) {
	dirs := os.Getenv("R6_DISSECT_REPLAY_DIRS")
	if dirs == "" {
		t.Skip("R6_DISSECT_REPLAY_DIRS not set")
	}
	var rounds, downs, downsCredited, revives, revivesCredited, damage, damageRows int
	for _, dir := range strings.Split(dirs, ":") {
		files, err := filepath.Glob(filepath.Join(dir, "*.rec"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no .rec files in %s", dir)
		}
		sort.Strings(files)
		for _, file := range files {
			r := readRound(t, file)
			name := filepath.Base(file)
			rounds++
			checkRound(t, name, r)
			if len(r.Warnings) > 0 {
				t.Errorf("%s: a listener failed: %+v", name, r.Warnings)
			}
			rows, total := checkDamageTaken(t, name, r)
			damageRows += rows
			damage += total
			for _, u := range r.MatchFeedback {
				switch u.Type {
				case dissect.DBNO:
					downs++
					if u.Username != "" {
						downsCredited++
					}
				case dissect.Revive:
					revives++
					if u.Username != "" {
						revivesCredited++
					}
				}
			}
		}
	}
	t.Logf("rounds %d, DBNO %d (%d credited), Revive %d (%d credited)", rounds, downs, downsCredited, revives, revivesCredited)
	t.Logf("damage taken: %d over %d player-rounds", damage, damageRows)
}

// checkDamageTaken checks the damageTaken of a round's stats and returns the rows that have it and their sum.
func checkDamageTaken(t *testing.T, name string, r *dissect.Reader) (rows, total int) {
	t.Helper()
	for _, s := range r.PlayerStats() {
		if r.Header.CodeVersion < dissect.Y10S4 {
			if s.DamageTaken != nil {
				t.Errorf("%s: %s has damageTaken before Y10S4", name, s.Username)
			}
			continue
		}
		if s.DamageTaken == nil {
			t.Errorf("%s: %s has no damageTaken", name, s.Username)
			continue
		}
		d := *s.DamageTaken
		if d < 0 || d > 400 {
			t.Errorf("%s: %s damageTaken %d out of range", name, s.Username, d)
		}
		if s.Died && d == 0 {
			t.Errorf("%s: %s died without taking damage", name, s.Username)
		}
		rows++
		total += d
	}
	return rows, total
}

func readRound(t *testing.T, file string) *dissect.Reader {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := dissect.NewReader(f)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	if err := r.Read(); !dissect.Ok(err) {
		t.Fatalf("%s: %v", file, err)
	}
	return r
}

func checkRound(t *testing.T, name string, r *dissect.Reader) {
	t.Helper()
	team := make(map[string]int)
	for _, p := range r.Header.Players {
		team[p.Username] = p.TeamIndex
	}
	if len(r.Header.GMSettings) == 0 {
		t.Errorf("%s: gmSettings empty", name)
	}
	planted := false
	for _, u := range r.MatchFeedback {
		if u.Type == dissect.DefuserPlantComplete {
			planted = true
		}
	}
	downed := make(map[string]bool) // down and not yet revived, killed or dead
	for i, u := range r.MatchFeedback {
		if i > 0 && u.ElapsedSeconds < r.MatchFeedback[i-1].ElapsedSeconds {
			t.Errorf("%s: event %d (%s) goes back in time: %v after %v", name, i, u.Type, u.ElapsedSeconds, r.MatchFeedback[i-1].ElapsedSeconds)
		}
		if u.Phase == dissect.PostPlantPhase && !planted {
			t.Errorf("%s: event %d (%s) is PostPlant without a plant", name, i, u.Type)
		}
		switch u.Type {
		case dissect.DBNO:
			if downed[u.Target] {
				t.Errorf("%s: %s downed twice without a revive (event %d)", name, u.Target, i)
			}
			downed[u.Target] = true
			if u.Username != "" && team[u.Username] == team[u.Target] && !u.TeamKill {
				t.Errorf("%s: %s downed by teammate %s without teamKill", name, u.Target, u.Username)
			}
		case dissect.Revive:
			if !downed[u.Target] {
				t.Errorf("%s: %s revived without a down (event %d)", name, u.Target, i)
			}
			delete(downed, u.Target)
			if u.Username != "" && team[u.Username] != team[u.Target] {
				t.Errorf("%s: %s revived by opponent %s", name, u.Target, u.Username)
			}
		case dissect.Kill:
			delete(downed, u.Target)
		case dissect.Death:
			delete(downed, u.Username)
		}
	}
}
