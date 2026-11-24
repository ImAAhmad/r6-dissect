package dissect

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
)

var ErrInvalidFile = errors.New("dissect: not a dissect file")
var ErrInvalidFolder = errors.New("dissect: not a match folder")
var ErrInvalidStringSep = errors.New("dissect: invalid string separator")

// ErrNoReplayData is returned by Reader.Read when the replay has a header but no data after it.
var ErrNoReplayData = errors.New("dissect: replay has no data after its header")

// RoundError reports a round file of a match folder that could not be read.
type RoundError struct {
	Index int    // position of the file in file-name order
	Path  string // path of the round file
	Err   error
}

func (e *RoundError) Error() string {
	return fmt.Sprintf("dissect: round file %s: %v", filepath.Base(e.Path), e.Err)
}

func (e *RoundError) Unwrap() error { return e.Err }

// IncompleteMatchError is returned by MatchReader.Read when some round files could not be read.
// The readable rounds are kept. It deliberately does not unwrap, so Ok(err) is false.
type IncompleteMatchError struct {
	Skipped []*RoundError
	Total   int
}

func (e *IncompleteMatchError) Error() string {
	if len(e.Skipped) == 0 {
		return "dissect: incomplete match"
	}
	return fmt.Sprintf("dissect: %d of %d round files could not be read (first: %v)", len(e.Skipped), e.Total, e.Skipped[0])
}

// Ok returns true if err only pertains to EOF (read was successful).
func Ok(err error) bool {
	// zstd.ErrMagicMismatch is expected at EOF because .rec files have extra non-compressed data.
	return err == nil || errors.Is(err, io.EOF) || errors.Is(err, zstd.ErrMagicMismatch)
}
