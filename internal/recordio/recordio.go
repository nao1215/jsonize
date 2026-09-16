// Package recordio cuts an input stream into records the one way jz
// reads them, so that the lines a format is identified by and the lines
// it is read with are ended and bounded alike.
package recordio

import (
	"bufio"
	"bytes"
	"errors"

	"github.com/nao1215/jsonize/pkg/convert"
)

// ErrTooLong reports a record longer than the limit it was read under.
var ErrTooLong = errors.New("line too long")

// Read reads one record up to sep, refusing one longer than maxLen
// rather than letting a producer with no separators in its output grow
// the buffer without bound. The record comes back without the
// separator; a nil error means sep ended it, and io.EOF comes alongside
// a last record the input ended without one.
//
// The limit counts the bytes between two separators as they were read,
// escape sequences and a carriage return included, whether or not a
// separator follows the last of them. A carriage return of a CRLF line
// ending is left on, because the whole-document reader removes the
// escape sequences first and the two have to agree.
func Read(br *bufio.Reader, sep byte, maxLen int) ([]byte, error) {
	var out []byte
	for {
		chunk, err := br.ReadSlice(sep)
		if len(out)+len(chunk) > maxLen+1 {
			return nil, ErrTooLong
		}
		out = append(out, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == sep {
			out = out[:len(out)-1]
		}
		if len(out) > maxLen {
			return nil, ErrTooLong
		}
		return out, err
	}
}

// Blank reports a line before the text of an input: nothing on it once
// the escape sequences and, on the first line, the byte order mark are
// off, which is how the selector reads it. The lines a format is
// identified by and the lines a stream holds back have to agree about
// which of them say nothing, so both ask here.
func Blank(line []byte, first bool) bool {
	line = convert.StripANSI(line)
	if first {
		line = bytes.TrimPrefix(line, []byte{0xEF, 0xBB, 0xBF})
	}
	return len(bytes.TrimSpace(line)) == 0
}
