# r6-dissect
[![](https://discordapp.com/api/guilds/936737628756271114/widget.png?style=shield)](https://discord.gg/XdEXWQZZAa)
[![Go Reference](https://pkg.go.dev/badge/github.com/redraskal/r6-dissect.svg)](https://pkg.go.dev/github.com/redraskal/r6-dissect)

Match Replay API/CLI for Rainbow Six: Siege's Dissect (.rec) format.

Supports replays from **Y7S1 through Y11S2** (and counting).

Download the latest version here: https://github.com/redraskal/r6-dissect/releases

## Features
- **Match Info** — Game version, map, gamemode, match type, teams, players, operators
- **Match Feedback** — Kills, teamkills, headshots, DBNOs (downs) and revives (Y10S4+), objective locates, defuser plants/disables, BattlEye bans, DCs
- **Round Timeline** — Every event has a round phase and a monotonic time since the action phase started
- **Scoreboard** — Per-player kills, assists, deaths, and scores with kill/death reconciliation
- **Output Formats** — JSON or Excel (.xlsx)

### See roadmap at https://github.com/users/redraskal/projects/1.

## CLI Usage
Print a match overview by specifying a match folder or .rec file:
```bash
r6-dissect --info Match-2026-03-26_21-19-17-21932
# or
r6-dissect --info Match-2026-03-26_21-19-17-21932-R01.rec
```
Export round stats to a JSON file:
```bash
r6-dissect Match-2026-03-26_21-19-17-21932-R01.rec -o round.json
```
Example output (abbreviated):
```json
{
  "gameVersion": "Y11S1_Alpha03",
  "codeVersion": 9578674,
  "timestamp": "2026-03-25T23:16:52Z",
  "matchType": { "name": "Ranked", "id": 2 },
  "map": { "name": "KafeDostoyevskyY10", "id": 413845419788 },
  "gamemode": { "name": "Bomb", "id": 327933806 },
  "teams": [
    { "name": "YOUR TEAM", "score": 1, "won": true, "winCondition": "KilledOpponents", "role": "Attack" },
    { "name": "OPPONENTS", "score": 0, "won": false, "role": "Defense" }
  ],
  "players": [
    {
      "id": 1830934665040226621,
      "username": "IanFiftyForty",
      "teamIndex": 0,
      "operator": { "name": "Oryx", "id": 104189664155 },
      "loadout": { "primary": 53995319912, "secondary": 127174503972, "gadget": 127363389074, "ability": 201523838476 }
    }
  ],
  "matchFeedback": [
    {
      "type": { "name": "Kill", "id": 0 },
      "username": "ReithYT",
      "target": "Ambatakum.",
      "headshot": false,
      "time": "1:51",
      "timeInSeconds": 111,
      "phase": "Action",
      "elapsedSeconds": 68
    }
  ]
}
```
`matchFeedback` is always an array: `[]` for a round with no events.

Loadouts (Y8S1+) are the game's asset IDs for the primary/secondary weapon, secondary gadget and operator ability at the end of the preparation phase. IDs are stable across seasons and shared by every operator who can equip the item; names are not resolved.

On Y9S4+ replays, a round whose replay holds no result (neither header score moved: the round was abandoned or remade, or the recording ended first) has `"won": false` on both teams and no `winCondition`, and credits no 1vX.

Export the entire match:
```bash
r6-dissect Match-2026-03-26_21-19-17-21932 -o match.json
```
A round file that cannot be read (empty, unreadable, or cut inside a zstd frame) is skipped: the match JSON lists it under `skippedRounds` (`file`, `index` = its 0-based position in the folder, `error`), stderr gets a `round skipped` line, and the CLI still exits 0. If no round file in the folder can be read, it exits 1. A file cut exactly at a zstd frame boundary is not detected: it reads as a shorter round, with no warning.

Export an Excel spreadsheet:
```bash
r6-dissect Match-2026-03-26_21-19-17-21932 -o match.xlsx
```
Output JSON to stdout:
```bash
# entire match
r6-dissect Match-2026-03-26_21-19-17-21932
# single round
r6-dissect Match-2026-03-26_21-19-17-21932-R01.rec
```

See example outputs in [/examples](https://github.com/redraskal/r6-dissect/tree/main/examples).

### Player fields
| Field | Description |
|---|---|
| `spawn` | The attacker spawn location. For defenders, this is the defended site. |
| `operator` | `name` and `id`. An operator ID newer than this build's operator table gets its `name` from the header `roleName` (for example `SOLID SNAKE` becomes `SolidSnake`); the `id` is unchanged. An `OperatorSwap` event to such an ID is named the same way when it is that player's final operator. |
| `startingRoom` | The room a defender was in when the round started (Y9S1–Y11S2; Y11S3 replays no longer contain per-player room names). |

### Stats fields
| Field | Description |
|---|---|
| `damageTaken` | Y10S4+, on round and match `stats[]`. HP the player lost that round from any source (enemies, self, falls, teammates, Rook armor HP), read from the player's health component. Boost-health decay (Finka) is not counted, revives and armor are not subtracted, and overkill is not visible (clamped at the player's remaining HP). A match row sums its rounds. Absent before Y10S4 and in rounds without health data. |

### Match feedback fields
| Field | Description |
|---|---|
| `time` / `timeInSeconds` | The in-game timer when the event happened. The timer restarts its countdown in every phase, so after a plant it shows the defuser timer, and after the action timer runs out without a plant it shows the end-of-round countdown. When a round ends early it reads 0:00 for the events that end the round. |
| `phase` | `Prep`, `Action` or `PostPlant` (after the defuser was planted). Rounds without a plant have no `PostPlant` events. |
| `elapsedSeconds` | Seconds since the action phase started. Always increases during a round; negative during the preparation phase. Use this to order or compare events. |
| `teamKill` | The kill was a teamkill. Teamkills count as a death for the victim but not as a kill. On a `DBNO`, the down ended in a teamkill. |
| `weaponID` | Kill events. The asset ID of the item that made the kill, read from the kill packet: the same IDs as a player's `loadout` fields. It can be another player's deployed gadget. Omitted when the kill packet has no weapon field. |
| `weaponSlot` | Kill events. `primary`, `secondary`, `gadget` or `ability`: the slot `weaponID` has in the loadout of the player the kill packet names. On Y10S4+ that player can differ from `username`, which can be corrected from the scoreboard to the player who downed the target. Omitted when the ID is in none of that player's slots. |
| `DBNO` events | Y10S4+. `target` is the downed player. `username` is the player credited with the kill if the downed player died, otherwise empty (the replay does not say who downed a player who was revived or bled out). |
| `Revive` events | Y10S4+. `target` is the revived player. `username` is the reviver, if one could be found. |

On Y10S4+ replays, the defuser planter/disabler is read from the defuser packets; if that player cannot be resolved, they are inferred from the +100 score bonus or the last player alive. A `DefuserDisableComplete` is added when defense wins a round after a plant without one being recorded. On Y10S4+ replays, the killer of a kill can be corrected from the scoreboard when the kill feed and the scoreboard disagree. Downs and revives are read from each player's health state; the reviver is inferred from the +50 score bonus of a teammate within 3 seconds of the revive.

### Round warnings
A round can carry `warnings` when a part of the replay could not be read, for example because a newer game build moved a marker the parser looks for. The packet that failed is skipped and the rest of the round is still read; each entry says what may be missing: `listener` (the parser function), `pattern` (the bytes it looks for), `error`, `count` (how many packets failed that way) and `firstOffset` (where the first failing match starts in the decompressed round). The key is absent when nothing failed.

## Importing a .rec file
```go
package main

import (
	"log"
	"os"

	"github.com/redraskal/r6-dissect/dissect"
)

func main() {
	f, err := os.Open("Match-2026-03-26_21-19-17-21932-R01.rec")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	r, err := dissect.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}
	// Use r.ReadPartial() for faster reads with less data
	// dissect.Ok(err) returns true if the error only pertains to EOF (read was successful)
	if err := r.Read(); !dissect.Ok(err) {
		log.Fatal(err)
	}
	// r.Warnings lists listeners that failed; the rest of the round was read
	print(r.Header.GameVersion) // Y11S1_Alpha03
}
```

## Shared Library (FFI)
Each release includes `libr6dissect` for Linux and Windows, for use from other languages via FFI.
```c
char* dissect_read(char* path); // returns {"data": ...} or {"error": "..."} as JSON
void dissect_free(char* ptr);   // release the string returned by dissect_read
```
Example with Python's `ctypes`:
```python
import ctypes, json
lib = ctypes.CDLL("./libr6dissect-linux-amd64.so")
lib.dissect_read.restype = ctypes.c_void_p
lib.dissect_read.argtypes = [ctypes.c_char_p]
lib.dissect_free.argtypes = [ctypes.c_void_p]
ptr = lib.dissect_read(b"Match-2026-03-26_21-19-17-21932")
result = json.loads(ctypes.string_at(ptr))
lib.dissect_free(ptr)
```

## Updating Operator Data
When new operators are added to the game, regenerate the operator roles mapping:
```bash
go generate ./dissect/...
```
This scrapes the current operator list from Ubisoft's website and updates `dissect/operator_roles.go`.

The tests comparing our operators with Ubisoft's website only run when `R6_DISSECT_NETWORK_TESTS=1` is set, and weekly in the Operators workflow.

## Tests
Each `.rec` file in `dissect/test/data/replays/valid` is parsed and compared with the `.rec.json` file next to it. After an intended change in output, regenerate them with:
```bash
go test ./dissect/test -run TestReader_Read -update
```
Whole matches that are too large to commit can be checked with `TestAcceptance_Replays` (event order, downs and revives, phases, gmSettings):
```bash
R6_DISSECT_REPLAY_DIRS=/path/to/Match-A:/path/to/Match-B go test ./dissect/test -run TestAcceptance -v
```

### Acceptance on real replays
Two opt-in checks run on replays that are not committed and are skipped when their variable is unset, which is how CI runs them. `R6_DISSECT_CORPUS` is a directory of match folders for the match-level `TestAcceptance_Matches`. `R6_DISSECT_REPLAY_DIRS` is for the round-level `TestAcceptance_Replays`. See [scripts/corpus/README.md](scripts/corpus/README.md) for building the Y11S1 fork corpus.

## Contributing
Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for how pull requests are reviewed and merged.

#
I would like to thank [stnokott](https://github.com/stnokott) for their work on r6-dissect, along with [draguve](https://github.com/draguve) & other contributors at [draguve/R6-Replays](https://github.com/draguve/R6-Replays) for their additional reverse engineering work.
