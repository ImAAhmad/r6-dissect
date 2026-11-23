package dissect

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"

	"github.com/rs/zerolog/log"
)

// ReadWarning records a listener that returned an error while a round was read.
// Read skips that one packet and keeps going, so the rest of the round is still read,
// but whatever that packet carried (a player, a kill, a scoreboard value) may be missing.
// Failures of the same listener on the same pattern with the same error are merged into one warning.
type ReadWarning struct {
	Listener    string `json:"listener"`    // the listener function, e.g. "readPlayer"
	Pattern     string `json:"pattern"`     // the byte pattern it listens for, e.g. "22 07 94 9B DC"
	Error       string `json:"error"`       // e.g. "EOF"
	Count       int    `json:"count"`       // how many packets failed this way
	FirstOffset int    `json:"firstOffset"` // where the first failing pattern match starts in the decompressed round
}

// listenerFailed logs err from listener for the pattern match whose last byte is at offset,
// and records it in r.Warnings.
func (r *Reader) listenerFailed(listenerIndex int, listener func(*Reader) error, offset int, err error) {
	name := listenerName(listener)
	query := r.queries[listenerIndex]
	pattern := fmt.Sprintf("% X", query)
	start := offset - len(query) + 1
	msg := err.Error()
	log.Warn().Str("listener", name).Str("pattern", pattern).Int("offset", start).Err(err).
		Msg("listener failed; skipping this packet, the rest of the round is still read")
	for i := range r.Warnings {
		w := &r.Warnings[i]
		if w.Listener == name && w.Pattern == pattern && w.Error == msg {
			w.Count++
			return
		}
	}
	r.Warnings = append(r.Warnings, ReadWarning{Listener: name, Pattern: pattern, Error: msg, Count: 1, FirstOffset: start})
}

// listenerName is the listener's function name without its package path:
// "readPlayer", or "(*Reader).listenLoadouts.func1" for a closure.
func listenerName(listener func(*Reader) error) string {
	f := runtime.FuncForPC(reflect.ValueOf(listener).Pointer())
	if f == nil {
		return "unknown"
	}
	name := f.Name()
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.Index(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}
