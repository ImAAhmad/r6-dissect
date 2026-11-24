package dissect

import "testing"

func headerPlayer(id, name, hero string) []headerEntry {
	return []headerEntry{{"playerid", id}, {"playername", name}, {"team", "0"}, {"heroname", hero}}
}

func concatEntries(parts ...[]headerEntry) []headerEntry {
	var all []headerEntry
	for _, p := range parts {
		all = append(all, p...)
	}
	return all
}

func checkPlayers(t *testing.T, players []Player, want ...string) {
	t.Helper()
	if len(players) != len(want) {
		t.Fatalf("want %d players, got %d: %+v", len(want), len(players), players)
	}
	for i, p := range players {
		if p.Username != want[i] || p.ID == 0 || p.HeroName == 0 {
			t.Fatalf("player %d: want %s with id and heroname, got %+v", i, want[i], p)
		}
	}
}

// Y11S2+: half the players come before the match properties.
func TestSplitHeaderEntries_Y11S2Order(t *testing.T) {
	entries := concatEntries(
		headerPlayer("1", "p1", "11"), headerPlayer("2", "p2", "12"),
		[]headerEntry{{"version", "Y11S2"}, {"gmsetting", "1"}, {"gmsetting", "2"}, {"maxnbplayersperteam", "5"}, {"id", "match"}},
		headerPlayer("3", "p3", "13"), headerPlayer("4", "p4", "14"),
		[]headerEntry{{"teamscore0", "0"}, {"teamscore1", "1"}},
	)
	props, gm, players, err := splitHeaderEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	checkPlayers(t, players, "p1", "p2", "p3", "p4")
	if len(gm) != 2 || gm[0] != 1 || gm[1] != 2 {
		t.Fatalf("want gmSettings [1 2], got %v", gm)
	}
	if props["version"] != "Y11S2" || props["id"] != "match" || props["teamscore1"] != "1" {
		t.Fatalf("props: %v", props)
	}
}

// Y11S3 adds starttime after the match id.
func TestSplitHeaderEntries_Y11S3StartTime(t *testing.T) {
	entries := concatEntries(
		headerPlayer("1", "p1", "11"), headerPlayer("2", "p2", "12"),
		[]headerEntry{{"version", "Y11S3"}, {"gmsetting", "1"}, {"id", "match"}, {"starttime", "123"}},
		headerPlayer("3", "p3", "13"), headerPlayer("4", "p4", "14"),
		[]headerEntry{{"teamscore0", "0"}, {"teamscore1", "1"}},
	)
	props, _, players, err := splitHeaderEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	checkPlayers(t, players, "p1", "p2", "p3", "p4")
	if props["starttime"] != "123" {
		t.Fatalf("want starttime as a match property, got %v", props)
	}
}

func TestSplitHeaderEntries_Y9Order(t *testing.T) {
	entries := concatEntries(
		[]headerEntry{{"version", "Y9S1"}, {"gmsetting", "7"}, {"gmsetting", "8"}},
		headerPlayer("1", "p1", "11"), headerPlayer("2", "p2", "12"),
		[]headerEntry{{"id", "match"}, {"teamscore0", "0"}, {"teamscore1", "1"}},
	)
	props, gm, players, err := splitHeaderEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	checkPlayers(t, players, "p1", "p2")
	if len(gm) != 2 || props["id"] != "match" {
		t.Fatalf("gmSettings %v, props %v", gm, props)
	}
}

// a player key added by a future season must not split the player.
func TestSplitHeaderEntries_UnknownPlayerKey(t *testing.T) {
	entries := []headerEntry{
		{"version", "Y12S1"},
		{"playerid", "1"}, {"playername", "p1"}, {"newplayerkey", "x"}, {"team", "0"}, {"heroname", "11"},
		{"id", "match"}, {"teamscore0", "0"}, {"teamscore1", "1"},
	}
	_, _, players, err := splitHeaderEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	checkPlayers(t, players, "p1")
}
