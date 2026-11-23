package main

// #include <stdlib.h>
import "C"
import (
	"encoding/json"
	"errors"
	"os"
	"unsafe"

	"github.com/redraskal/r6-dissect/dissect"
	"github.com/rs/zerolog"
)

func marshalToString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{\"error\":\"something went wrong during json Marshal\"}"
	}
	return string(b)
}

func convertForExport(v any) string {
	type export struct {
		Data any `json:"data"`
	}
	return marshalToString(export{
		Data: v,
	})
}

func convertErrorForExport(err error) string {
	type export struct {
		Error string `json:"error"`
	}
	return marshalToString(export{
		Error: err.Error(),
	})
}

// dissect_read parses a .rec file or match folder and returns the result as a JSON string.
// The returned string is allocated with malloc and must be released with dissect_free.
//
//export dissect_read
func dissect_read(input *C.char) *C.char {
	zerolog.SetGlobalLevel(zerolog.Disabled)
	return C.CString(readJSON(C.GoString(input)))
}

// readJSON parses the .rec file or match folder at path and returns
// {"data": ...} or {"error": "..."} as JSON.
func readJSON(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return convertErrorForExport(err)
	}
	defer f.Close()
	s, err := f.Stat()
	if err != nil {
		return convertErrorForExport(err)
	}
	if s.IsDir() {
		m, err := dissect.NewMatchReader(f)
		if err != nil {
			return convertErrorForExport(err)
		}
		if err := m.Read(); err != nil {
			var incomplete *dissect.IncompleteMatchError
			if !errors.As(err, &incomplete) || len(incomplete.Skipped) == incomplete.Total {
				return convertErrorForExport(err)
			}
		}
		j := m.Data()
		return convertForExport(j)
	} else {
		r, err := dissect.NewReader(f)
		if err != nil {
			return convertErrorForExport(err)
		}
		if err := r.Read(); !dissect.Ok(err) {
			return convertErrorForExport(err)
		}
		type round struct {
			dissect.Header
			MatchFeedback []dissect.MatchUpdate      `json:"matchFeedback"`
			PlayerStats   []dissect.PlayerRoundStats `json:"stats"`
			Warnings      []dissect.ReadWarning      `json:"warnings,omitempty"`
		}
		return convertForExport(round{
			r.Header,
			r.MatchFeedback,
			r.PlayerStats(),
			r.Warnings,
		})
	}
}

// dissect_free releases a string returned by dissect_read.
//
//export dissect_free
func dissect_free(ptr *C.char) {
	C.free(unsafe.Pointer(ptr))
}

func main() {}
