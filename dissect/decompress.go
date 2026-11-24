package dissect

import (
	"bytes"

	"github.com/klauspost/compress/zstd"
)

// maxSizeHintRatio caps the hint relative to the compressed size (worst seen: 13x),
// so a corrupt frame header cannot request gigabytes up front.
const maxSizeHintRatio = 32

// decompressedSizeHint sums the content sizes declared by the zstd frame headers in compressed.
// It is only a capacity hint: a wrong or missing size costs a reallocation, never output.
func decompressedSizeHint(compressed []byte) int {
	magic := []byte{0x28, 0xB5, 0x2F, 0xFD}
	limit := uint64(maxSizeHintRatio) * uint64(len(compressed))
	var total uint64
	for i := 0; ; {
		k := bytes.Index(compressed[i:], magic)
		if k < 0 {
			break
		}
		i += k
		var h zstd.Header
		if h.Decode(compressed[i:]) == nil && h.HasFCS {
			total += h.FrameContentSize
			if total > limit {
				return int(limit)
			}
		}
		i += len(magic)
	}
	return int(total)
}

// appendWriter is an io.Writer that appends to b. Unlike bytes.Buffer, which doubles
// its capacity, append grows large slices by about 1.25x.
type appendWriter struct{ b []byte }

func (w *appendWriter) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}
