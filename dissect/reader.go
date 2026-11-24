package dissect

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"runtime"
	"slices"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/rs/zerolog/log"
)

var strSep = []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

type Reader struct {
	b                      []byte
	offset                 int
	queries                [][]byte
	listeners              [][]func(r *Reader) error
	time                   float64 // in seconds
	timeRaw                string  // raw dissect format
	clock                  roundClock
	lastDefuserPlayerIndex int
	planted                bool
	defuserDisabling       bool
	lastDefuserTimer       float64
	defuserPawnToSlot      map[[4]byte][4]byte // Y10S4+: maps a pawn entity ID to its player slot entity ID
	defuserRun             defuserRun          // Y10S4+: the plant/disable action the defuser timer currently belongs to
	readPartial            bool                // reads up to the player info packets
	playersRead            int
	markers                *playerMarkers      // player record tags of this round, see playerMarkers
	lastScoreboardKiller   string              // Y10S4+: player whose scoreboard kill counter increased last, until the next kill packet
	pendingSBIDs           [][4]byte           // Y10S4+: sbIDs in header-entry order for deferred mapping
	readPlayerOrder        []int               // Y10S4+: player indices in readPlayer order
	scoreboardIDToPlayer   map[[4]byte]int     // Y10S4+: maps scoreboard entry ID to player index
	scoreboardInitialKills map[[4]byte]uint32  // Y10S4+: cumulative kills at round start per sbID
	scoreboardFinalKills   map[int]uint32      // Y10S4+: latest cumulative kills seen per player index
	scoreboardEntityOwner  map[[4]byte]uint64  // Y10S4+: scoreboard entity ID -> owning player object handle
	pendingDefuserPlantIdx int                 // Y10S4+: index into MatchFeedback for DefuserPlantComplete/DisableComplete with unknown player (-1 = none)
	pendingDefuserIsPlant  bool                // Y10S4+: true if pending event is plant (attacker), false if disable (defender)
	lastPlayerScores       map[int]uint32      // Y10S4+: last known score per player index (for detecting +100 plant/disable bonus)
	inventory              *inventoryState     // Y8S1+: player inventories for loadouts
	healthToSlot           map[[4]byte][4]byte // Y10S4+: maps a health component entity ID to its player slot entity ID
	healthState            map[int]uint32      // Y10S4+: last health state per player index
	healthTransitions      []healthTransition  // Y10S4+: health state changes of all players
	healthSamples          []healthSample      // Y10S4+: current health values of all health components, in read order
	scoreRevives           []scoreRevive       // Y10S4+: +50 score updates, matched with revives
	Header                 Header              `json:"header"`
	MatchFeedback          []MatchUpdate       `json:"matchFeedback"`
	Warnings               []ReadWarning       `json:"warnings,omitempty"` // listeners that failed, see ReadWarning
	Scoreboard             Scoreboard
}

// NewReader decompresses in using zstd and
// validates the dissect header.
func NewReader(in io.Reader) (r *Reader, err error) {
	br := bufio.NewReader(in)
	chunkedCompression, err := testFileCompression(br)
	if err != nil {
		return r, err
	}
	log.Debug().Bool("chunkedCompression (>=Y8S4)", chunkedCompression).Send()
	r = &Reader{
		readPartial:            false,
		lastDefuserPlayerIndex: -1,
		pendingDefuserPlantIdx: -1,
		scoreboardIDToPlayer:   make(map[[4]byte]int),
		scoreboardInitialKills: make(map[[4]byte]uint32),
		scoreboardFinalKills:   make(map[int]uint32),
		scoreboardEntityOwner:  make(map[[4]byte]uint64),
		lastPlayerScores:       make(map[int]uint32),
		defuserPawnToSlot:      make(map[[4]byte][4]byte),
		healthToSlot:           make(map[[4]byte][4]byte),
		healthState:            make(map[int]uint32),
		MatchFeedback:          make([]MatchUpdate, 0),
	}
	if chunkedCompression {
		if err = r.readChunkedData(br); err != nil {
			return r, err
		}
	} else {
		if err = r.readNonChunkedData(br); err != nil {
			return r, err
		}
	}
	log.Debug().Int("size", len(r.b)).Send()
	log.Debug().Str("season", r.Header.GameVersion).Int("code", r.Header.CodeVersion).Send()
	r.Listen([]byte{0x22, 0x07, 0x94, 0x9B, 0xDC}, readPlayer)
	r.Listen([]byte{0x22, 0xA9, 0x26, 0x0B, 0xE4}, readAtkOpSwap)
	r.Listen([]byte{0xAF, 0x98, 0x99, 0xCA}, readSpawn)
	if r.Header.CodeVersion >= Y8S1 {
		r.Listen([]byte{0x1F, 0x07, 0xEF, 0xC9}, readTime)
	} else {
		r.Listen([]byte{0x1E, 0xF1, 0x11, 0xAB}, readY7Time)
	}
	r.Listen([]byte{0x59, 0x34, 0xE5, 0x8B, 0x04}, readMatchFeedback)
	r.Listen([]byte{0x22, 0xA9, 0xC8, 0x58, 0xD9}, readDefuserTimer)
	if r.Header.CodeVersion >= Y10S4 {
		r.Listen(defuserPawnIndicator, readDefuserPawn)
		r.Listen(scoreboardEntityField, readScoreboardEntity)
		r.Listen(healthLinkIndicator, readHealthLink)
		r.Listen(healthStateIndicator, readHealthState)
		r.Listen(healthValueIndicator, readHealthValue)
	}
	r.Listen([]byte{0xEC, 0xDA, 0x4F, 0x80}, readScoreboardScore)
	r.Listen([]byte{0x4D, 0x73, 0x7F, 0x9E}, readScoreboardAssists)
	r.Listen([]byte{0x1C, 0xD2, 0xB1, 0x9D}, readScoreboardKills)
	r.listenLoadouts()
	return r, err
}

func (r *Reader) readChunkedData(genericReader io.Reader) error {
	log.Debug().Msg("reading data")
	temp, err := io.ReadAll(genericReader)
	if err != nil {
		return err
	}
	r.b = temp
	log.Debug().Msg("reading header magic")
	if err := r.readHeaderMagic(); err != nil {
		return err
	}
	log.Debug().Msg("reading header")
	h, err := r.readHeader()
	r.Header = h
	if err != nil {
		return err
	}
	log.Debug().Msg("decompressing data")
	zstdMagic := []byte{0x28, 0xB5, 0x2F, 0xFD}
	// Sections are small, so the synchronous decoder is as fast and allocates far less.
	zstdReader, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return err
	}
	defer zstdReader.Close()
	memoryReader := bytes.NewReader(nil)
	patternIndex := 0
	sections := 0
	data := appendWriter{b: make([]byte, 0, decompressedSizeHint(r.b[r.offset:]))}
	for !errors.Is(err, io.EOF) {
		for patternIndex != 4 {
			b, scanErr := r.Bytes(1)
			if errors.Is(scanErr, io.EOF) {
				err = scanErr
				break
			}
			if scanErr != nil {
				return scanErr
			}
			if b[0] == zstdMagic[patternIndex] {
				patternIndex++
			} else {
				patternIndex = 0
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		sections++
		patternIndex = 0
		memoryReader.Reset(r.b[r.offset-4:])
		tempReader := countedReader{memoryReader, 0}
		if err = zstdReader.Reset(&tempReader); err != nil {
			return err
		}
		// decode straight onto the end of data instead of into a new buffer per section
		n, err := zstdReader.WriteTo(&data)
		if err != nil && !(n > 0 && errors.Is(err, zstd.ErrMagicMismatch)) {
			return err
		}
		r.offset += tempReader.n
	}
	r.b = data.b
	r.offset = 0
	log.Debug().Int("zstd_sections", sections).Send()
	return nil
}

func (r *Reader) readNonChunkedData(genericReader io.Reader) error {
	zstdReader, err := zstd.NewReader(genericReader)
	if err != nil {
		return err
	}
	defer zstdReader.Close()
	decompressed, err := io.ReadAll(zstdReader)
	if err != nil && !(len(decompressed) > 0 && errors.Is(err, zstd.ErrMagicMismatch)) {
		return err
	}
	r.b = decompressed
	if err = r.readHeaderMagic(); err != nil {
		return err
	}
	h, err := r.readHeader()
	r.Header = h
	return err
}

type match struct {
	offset        int // index of the last byte of the pattern; listeners start at offset+1
	listenerIndex int // index into r.queries / r.listeners
}

// findQueries returns every occurrence of every query in r.b[start:end],
// ordered by offset and then by listener index (registration order).
//
// Each query is searched for on its own with bytes.Index over the whole
// range, so an occurrence is never lost to a block split or to a partial
// match of the same query that started a few bytes earlier. After an
// occurrence the search resumes past its last byte, so occurrences of one
// query never overlap. An empty query matches nothing.
func (r *Reader) findQueries(start, end int) []match {
	if start < 0 || end > len(r.b) || start >= end {
		return nil
	}
	b := r.b[start:end]
	found := make([][]match, len(r.queries))
	var wg sync.WaitGroup
	for j, query := range r.queries {
		if len(query) == 0 {
			continue
		}
		wg.Add(1)
		go func(j int, query []byte) {
			defer wg.Done()
			var out []match
			for i := 0; i <= len(b)-len(query); {
				k := bytes.Index(b[i:], query)
				if k < 0 {
					break
				}
				i += k + len(query)
				out = append(out, match{offset: start + i - 1, listenerIndex: j})
			}
			found[j] = out
		}(j, query)
	}
	wg.Wait()
	matches := slices.Concat(found...)
	slices.SortFunc(matches, func(a, b match) int {
		return cmp.Or(cmp.Compare(a.offset, b.offset), cmp.Compare(a.listenerIndex, b.listenerIndex))
	})
	return matches
}

// Read continues reading the replay past the header until the EOF.
//
// It returns an error only when the replay has no data after its header
// (ErrNoReplayData). A listener that fails is skipped for that one packet and
// recorded in Warnings; every later listener and the end-of-round work still run.
func (r *Reader) Read() error {
	if r.offset >= len(r.b) {
		// nothing after the header to scan
		return ErrNoReplayData
	}
	end := len(r.b)
	if r.readPartial {
		end /= 3
	}
	matches := r.findQueries(r.offset, end)
	log.Debug().Int("matches", len(matches)).Msg("calling listeners")
	for _, entry := range matches {
		for _, listener := range r.listeners[entry.listenerIndex] {
			r.offset = entry.offset + 1
			n := len(r.MatchFeedback)
			if err := listener(r); err != nil {
				r.listenerFailed(entry.listenerIndex, listener, entry.offset, err)
			}
			r.stampNew(n)
		}
	}
	if !r.readPartial {
		n := len(r.MatchFeedback)
		// a plant/disable still in progress when the round ends did not complete
		r.stopDefuserRun()
		r.roundEnd()
		r.stampNew(n)
		// after roundEnd, so that kills corrected from the scoreboard credit the downs
		r.resolveHealthEvents()
		r.backfillLeadingUpdates()
		r.relabelPostPlantWithoutPlant()
		r.applyLoadouts()
		// after applyLoadouts, which it reads
		r.classifyKillWeapons()
	}
	r.nameUnknownSwapOperators()
	r.warnUnknownOperators()
	r.b = nil
	return nil
}

// ReadPartial continues reading the replay past the header until the full player list is read.
// This information does not include dynamic data, such as attack operator swaps.
// Use ReadPartial for faster, minimal reads.
func (r *Reader) ReadPartial() error {
	r.readPartial = true
	log.Debug().Msg("using partial read")
	err := r.Read()
	r.readPartial = false
	return err
}

// Listen registers a callback to be run during Read whenever
// the pattern is found. If the callback returns an error, Read records it in
// Warnings and goes on; what the callback changed before the error is kept.
func (r *Reader) Listen(pattern []byte, callback func(r *Reader) error) {
	var i int
	for i = 0; i < len(r.queries); i++ {
		if bytes.Equal(r.queries[i], pattern) {
			r.listeners[i] = append(r.listeners[i], callback)
			return
		}
	}
	r.queries = append(r.queries, pattern)
	r.listeners = append(r.listeners, []func(reader *Reader) error{callback})
}

// Seek skips through the replay until the pattern is found and leaves the
// offset just past it. If the pattern is not found, it returns io.EOF.
//
// Like a walk over r.Bytes(1), which reports io.EOF on reaching the last byte
// of r.b, Seek ignores a match that ends on that byte. On a miss the offset
// moves to the end of r.b, or one byte on if it was already there.
func (r *Reader) Seek(pattern []byte) error {
	start := r.offset
	if start < len(r.b) {
		if k := bytes.Index(r.b[start:len(r.b)-1], pattern); k >= 0 {
			r.offset = start + k + len(pattern)
			return nil
		}
	}
	r.offset = max(len(r.b), start+1)
	e := log.Warn().Int("bytes", r.offset-start).Str("pattern", fmt.Sprintf("% X", pattern))
	pc, _, _, ok := runtime.Caller(1)
	if details := runtime.FuncForPC(pc); ok && details != nil {
		e = e.Interface("func", details.Name())
	}
	e.Msg("large seek")
	return io.EOF
}

// Skip increases the replay offset by n bytes.
func (r *Reader) Skip(n int) error {
	r.offset += n
	if r.offset >= len(r.b) {
		return io.EOF
	}
	return nil
}

func (r *Reader) Bytes(n int) ([]byte, error) {
	if err := r.Skip(n); err != nil {
		return []byte{}, err
	}
	return r.b[r.offset-n : r.offset], nil
}

func (r *Reader) Int() (int, error) {
	b, err := r.Bytes(1)
	if err != nil {
		return -1, err
	}
	return int(b[0]), nil
}

func (r *Reader) String() (string, error) {
	size, err := r.Int()
	if err != nil {
		return "", err
	}
	b, err := r.Bytes(size)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *Reader) Uint32() (uint32, error) {
	if err := r.Skip(1); err != nil {
		return 0, err
	}
	b, err := r.Bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func (r *Reader) Uint64() (uint64, error) {
	if err := r.Skip(1); err != nil { // size- unnecessary since we already know the length
		return 0, err
	}
	b, err := r.Bytes(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b), nil
}

func (r *Reader) Write(w io.Writer) (n int, err error) {
	return w.Write(r.b)
}

func (r *Reader) Float32() (float32, error) {
	b, err := r.Bytes(4)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(b)), nil
}

// stampNew stamps match updates appended since index n that do not have a phase yet.
func (r *Reader) stampNew(n int) {
	for i := n; i < len(r.MatchFeedback); i++ {
		if r.MatchFeedback[i].Phase == "" {
			r.stamp(&r.MatchFeedback[i])
		}
	}
}
