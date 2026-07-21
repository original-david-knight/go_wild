package main

import (
	"io"
	"testing"
)

func TestJSONRawReaderReturnsEOF(t *testing.T) {
	data := []byte(`{"hello":"world"}`)
	r := &jsonRawReader{data: data}
	buf := make([]byte, 4)

	total := 0
	for {
		n, err := r.Read(buf)
		total += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		if n == 0 {
			t.Fatalf("unexpected zero-byte read without EOF")
		}
	}

	if total != len(data) {
		t.Fatalf("expected to read %d bytes, read %d", len(data), total)
	}

	n, err := r.Read(buf)
	if n != 0 || err != io.EOF {
		t.Fatalf("expected EOF on exhausted reader, got n=%d err=%v", n, err)
	}
}
