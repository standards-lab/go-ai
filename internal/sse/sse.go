package sse

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"unicode/utf8"
)

// Event is one event a stream dispatched.
type Event struct {
	// Type is the event's type, from its last event field. Empty means the stream named none,
	// which the standard reads as "message".
	Type string
	// Data is the event's data: its data fields' values joined with "\n". It may be empty, for
	// an event whose only data field had an empty value.
	Data string
	// ID is the stream's last event ID when the event was dispatched: the last id field's
	// value, in this event or an earlier one, that held no NUL. Empty means none was set, or
	// the stream reset it.
	ID string
}

// Decoder reads the events of one stream. It isn't safe for concurrent use. It decodes the
// stream as UTF-8 with replacement, as the standard does.
//
// A line has no length limit: the Decoder holds a line until its ending arrives, so a stream's
// longest line is the most it buffers. The caller bounds a stream it doesn't trust with its
// context or transport, as it bounds the rest of a reply.
type Decoder struct {
	r          *bufio.Reader
	bomPending bool
	afterCR    bool
	line       []byte

	typ    string
	data   strings.Builder
	lastID string
}

// NewDecoder returns a Decoder reading the stream r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReader(r), bomPending: true}
}

// Next reads the stream until it dispatches an event, at a blank line ending a block that holds
// a data field, and returns that event. Comments, unknown fields, and blocks without data
// dispatch nothing, and Next reads on past them. At the stream's end it returns io.EOF, and the
// standard discards an event that no blank line ended. A read error is returned as it is.
func (d *Decoder) Next() (Event, error) {
	for {
		line, err := d.readLine()
		if err != nil {
			return Event{}, err
		}
		if len(line) == 0 {
			if e, ok := d.dispatch(); ok {
				return e, nil
			}
			continue
		}
		d.field(decodeUTF8(line))
	}
}

func (d *Decoder) readLine() ([]byte, error) {
	d.line = d.line[:0]
	for {
		b, err := d.r.ReadByte()
		if err != nil {
			return nil, err
		}
		if d.afterCR {
			d.afterCR = false
			if b == '\n' {
				continue
			}
		}
		switch b {
		case '\r':
			d.afterCR = true
			return d.stripBOM(d.line), nil
		case '\n':
			return d.stripBOM(d.line), nil
		}
		d.line = append(d.line, b)
	}
}

var bom = []byte("\uFEFF")

func (d *Decoder) stripBOM(line []byte) []byte {
	if d.bomPending {
		d.bomPending = false
		return bytes.TrimPrefix(line, bom)
	}
	return line
}

func decodeUTF8(line []byte) []byte {
	if utf8.Valid(line) {
		return line
	}
	out := make([]byte, 0, len(line)+2*utf8.UTFMax)
	var (
		cp           rune
		needed, seen int
		lower, upper byte = 0x80, 0xBF
	)
	for i := 0; i < len(line); {
		b := line[i]
		if needed == 0 {
			i++
			switch {
			case b <= 0x7F:
				out = append(out, b)
			case 0xC2 <= b && b <= 0xDF:
				needed, cp = 1, rune(b&0x1F)
			case 0xE0 <= b && b <= 0xEF:
				switch b {
				case 0xE0:
					lower = 0xA0
				case 0xED:
					upper = 0x9F
				}
				needed, cp = 2, rune(b&0x0F)
			case 0xF0 <= b && b <= 0xF4:
				switch b {
				case 0xF0:
					lower = 0x90
				case 0xF4:
					upper = 0x8F
				}
				needed, cp = 3, rune(b&0x07)
			default:
				out = utf8.AppendRune(out, utf8.RuneError)
			}
			continue
		}
		if b < lower || b > upper {
			out = utf8.AppendRune(out, utf8.RuneError)
			cp, needed, seen, lower, upper = 0, 0, 0, 0x80, 0xBF
			continue
		}
		i++
		lower, upper = 0x80, 0xBF
		cp = cp<<6 | rune(b&0x3F)
		if seen++; seen == needed {
			out = utf8.AppendRune(out, cp)
			cp, needed, seen = 0, 0, 0
		}
	}
	if needed != 0 {
		out = utf8.AppendRune(out, utf8.RuneError)
	}
	return out
}

func (d *Decoder) field(line []byte) {
	if line[0] == ':' {
		return
	}
	name, value, _ := bytes.Cut(line, []byte(":"))
	value = bytes.TrimPrefix(value, []byte(" "))
	switch string(name) {
	case "event":
		d.typ = string(value)
	case "data":
		d.data.Write(value)
		d.data.WriteByte('\n')
	case "id":
		if bytes.IndexByte(value, 0) < 0 {
			d.lastID = string(value)
		}
	}
}

func (d *Decoder) dispatch() (Event, bool) {
	data, typ := d.data.String(), d.typ
	d.data.Reset()
	d.typ = ""
	if data == "" {
		return Event{}, false
	}
	return Event{Type: typ, Data: strings.TrimSuffix(data, "\n"), ID: d.lastID}, true
}
