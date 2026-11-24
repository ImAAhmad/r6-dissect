package dissect

import (
	"bytes"
	"github.com/rs/zerolog/log"
	"github.com/xuri/excelize/v2"
	"io"
)

func (r *Reader) PlayerIndexByID(id []byte) int {
	for i, p := range r.Header.Players {
		if bytes.Equal(id, p.DissectID) {
			return i
		}
	}
	if !bytes.Equal(id, []byte{0x00, 0x00, 0x00, 0x00}) {
		log.Debug().Hex("id", id).Msg("warn: could not index player by id")
	}
	return -1
}

func (r *Reader) PlayerIndexByUsername(username string) int {
	for i, p := range r.Header.Players {
		if p.Username == username {
			return i
		}
	}
	log.Debug().Str("username", username).Msg("warn: could not index player by username")
	return -1
}

type countedReader struct {
	io.Reader
	n int
}

func (r *countedReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.n += n
	return n, err
}

var headerFont = &excelize.Font{
	Family: "Arial",
	Size:   18,
}

type excelCompass struct {
	f        *excelize.File
	s        string
	row, col int
}

func newExcelCompass(f *excelize.File, sheet string) *excelCompass {
	return &excelCompass{
		f: f,
		s: sheet,
	}
}

func (c *excelCompass) Sheet(sheet string) *excelCompass {
	c.s = sheet
	c.Reset()
	return c
}

func (c *excelCompass) Reset() *excelCompass {
	c.row = 0
	c.col = 0
	return c
}

func (c *excelCompass) Up(n int) *excelCompass {
	c.row -= n
	if c.row < 0 {
		c.row = 0
	}
	return c
}

func (c *excelCompass) Down(n int) *excelCompass {
	c.row += n
	return c
}

func (c *excelCompass) Left(n int) *excelCompass {
	c.col -= n
	if c.col < 0 {
		c.col = 0
	}
	return c
}

func (c *excelCompass) Right(n int) *excelCompass {
	c.col += n
	return c
}

func (c *excelCompass) Cell() string {
	cell, _ := excelize.CoordinatesToCellName(c.col+1, c.row+1, false)
	return cell
}

func (c *excelCompass) Heading(text string) *excelCompass {
	c.f.SetCellRichText(c.s, c.Cell(), []excelize.RichTextRun{
		{
			Text: text,
			Font: headerFont,
		},
	})
	return c
}

func (c *excelCompass) Str(text string) *excelCompass {
	c.f.SetCellStr(c.s, c.Cell(), text)
	return c
}

func (c *excelCompass) Bool(b bool) *excelCompass {
	c.f.SetCellBool(c.s, c.Cell(), b)
	return c
}

func (c *excelCompass) Int(n int) *excelCompass {
	c.f.SetCellInt(c.s, c.Cell(), int64(n))
	return c
}

func (c *excelCompass) Float(n float64, precision int) *excelCompass {
	c.f.SetCellFloat(c.s, c.Cell(), n, precision, 64)
	return c
}
