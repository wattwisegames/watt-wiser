package backend

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func expectToRead(t *testing.T, reader io.Reader, expected []byte) {
	var scratch [1024]byte
	n, err := reader.Read(scratch[:])
	if err != nil {
		t.Errorf("expected read to succeed, got: %v", err)
	} else if !bytes.Equal(scratch[:n], expected) {
		t.Errorf("expected read to yield %q, got: %q", expected, scratch[:n])
	}
}

func expectReadEOF(t *testing.T, reader io.Reader) {
	var scratch [1024]byte
	n, err := reader.Read(scratch[:])
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected read to give EOF, got: %v", err)
	} else if n != 0 {
		t.Errorf("expected read to read nothing, read %q", scratch[:n])
	}
}

func TestLineReader(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	first := "hello\n"
	second := "there\n"
	buf.WriteString("hello\n")
	buf.WriteString("there\n")
	l := NewLineReader(buf)
	expectToRead(t, l, []byte(first))
	expectToRead(t, l, []byte(second))
	third := "unterminated"
	buf.WriteString(third)
	expectReadEOF(t, l)
	fourth := "line\n"
	buf.WriteString(fourth)
	fullLine := third + fourth
	expectToRead(t, l, []byte(fullLine))
	buf.WriteString("foo")
	expectReadEOF(t, l)
	buf.WriteString("bar")
	expectReadEOF(t, l)
	buf.WriteString("bin\nbaz")
	expectToRead(t, l, []byte("foobarbin\n"))
}
