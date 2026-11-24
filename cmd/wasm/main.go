//go:build js && wasm

// Command wasm is r6-dissect built for WebAssembly, for Ligo (https://ligoleague.eu): it reads
// one round's .rec file in a browser Web Worker or in Node, with the same module in both, so a
// server can parse a file again and compare with what a browser sent.
//
// It sets one global, r6dissect, with:
//
//	parseRound(bytes Uint8Array) -> { json: string } | { error: string }
//
// Build: GOOS=js GOARCH=wasm go build -trimpath -o r6.wasm ./cmd/wasm
// and load it with Go's wasm_exec.js.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"syscall/js"

	"github.com/redraskal/r6-dissect/dissect"
	"github.com/rs/zerolog"
)

// round is what Ligo keeps of a round: the CLI's round output (main.go, writeRound) without the
// positions, utility and destruction events, which Ligo does not use and which would make the
// JSON a browser sends hundreds of times larger.
type round struct {
	dissect.Header
	MatchFeedback []dissect.MatchUpdate      `json:"matchFeedback"`
	PlayerStats   []dissect.PlayerRoundStats `json:"stats"`
}

func parseRound(in []byte) (out string, err error) {
	// A malformed file must not take the whole module down: the next file still parses.
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("parser panic: %v", recovered)
		}
	}()
	r, err := dissect.NewReader(bytes.NewReader(in))
	if err != nil {
		return "", err
	}
	if err := r.Read(); !dissect.Ok(err) {
		return "", err
	}
	encoded, err := json.Marshal(round{
		Header:        r.Header,
		MatchFeedback: r.MatchFeedback,
		PlayerStats:   r.PlayerStats(),
	})
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func main() {
	// Logs would go to the console of every player who uploads a replay.
	zerolog.SetGlobalLevel(zerolog.Disabled)
	parse := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].Type() != js.TypeObject {
			return map[string]any{"error": "parseRound takes one Uint8Array"}
		}
		in := make([]byte, args[0].Get("length").Int())
		js.CopyBytesToGo(in, args[0])
		out, err := parseRound(in)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		return map[string]any{"json": out}
	})
	js.Global().Set("r6dissect", map[string]any{"parseRound": parse})
	// The functions above live as long as the program does.
	select {}
}
