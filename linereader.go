package main

import (
	"bufio"
	"io"
)

// lineReader is a specialized reader that ensures only entire newline-delimited lines are
// read at a time. This is useful when attempting to parse a file that is being actively
// written to as a CSV, as you don't actually attempt to parse any partial lines.
type lineReader struct {
	r       *bufio.Reader
	partial []byte
}

var _ io.Reader = (*lineReader)(nil)

func NewLineReader(r io.Reader) *lineReader {
	return &lineReader{
		r: bufio.NewReader(r),
	}
}

func (l *lineReader) Read(b []byte) (int, error) {
	data, err := l.r.ReadBytes(byte('\n'))
	if err != nil {
		l.partial = append(l.partial, data...)
		return 0, io.EOF
	}
	var n int
	if len(l.partial) > 0 {
		n = copy(b, l.partial)
		l.partial = l.partial[:copy(l.partial, l.partial[n:])]
		b = b[n:]
	}
	return n + copy(b, data), nil
}
