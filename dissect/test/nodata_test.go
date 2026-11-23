package test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/redraskal/r6-dissect/dissect"
)

func TestReader_Read_HeaderOnlyReplay(t *testing.T) {
	b := readFixture(t, solidSnake)
	end := bytes.Index(b, zstdMagic)
	if end <= 0 {
		t.Fatal("no zstd frame in fixture")
	}
	r, err := dissect.NewReader(bytes.NewReader(b[:end]))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Read(); !errors.Is(err, dissect.ErrNoReplayData) {
		t.Fatalf("Read() = %v, want ErrNoReplayData", err)
	}
}
