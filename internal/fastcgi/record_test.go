package fastcgi

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestAppendLength(t *testing.T) {
	tests := []struct {
		n    int
		want []byte
	}{
		{0, []byte{0}},
		{127, []byte{127}},
		{128, []byte{0x80, 0, 0, 128}},
		{70000, []byte{0x80, 0x01, 0x11, 0x70}},
	}
	for _, tt := range tests {
		if got := appendLength(nil, tt.n); !bytes.Equal(got, tt.want) {
			t.Errorf("appendLength(%d) = %x, want %x", tt.n, got, tt.want)
		}
	}
}

func TestWriteRecordPadsToEightBytes(t *testing.T) {
	var buf bytes.Buffer
	if err := writeRecord(&buf, typeStdin, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 16 {
		t.Fatalf("record is %d bytes, want 16", buf.Len())
	}
	h, content, err := readRecord(&buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.Type != typeStdin || string(content) != "abc" || h.PaddingLength != 5 {
		t.Fatalf("got type %d content %q padding %d", h.Type, content, h.PaddingLength)
	}
}

func readAll(t *testing.T, buf *bytes.Buffer) (sizes []int) {
	t.Helper()
	for buf.Len() > 0 {
		h, content, err := readRecord(buf, nil)
		if err != nil {
			t.Fatal(err)
		}
		if h.Type != typeParams {
			t.Fatalf("unexpected record type %d", h.Type)
		}
		sizes = append(sizes, len(content))
	}
	return sizes
}

func TestWriteParamsKeepsPairsWhole(t *testing.T) {
	big := strings.Repeat("x", 40000)
	var buf bytes.Buffer
	params := []Param{{"A", big}, {"B", big}, {"C", "c"}}
	if err := writeParams(&buf, params); err != nil {
		t.Fatal(err)
	}
	sizes := readAll(t, &buf)
	// A and B do not fit one record together, so each starts a record; C
	// follows B, and the stream ends with an empty record.
	if len(sizes) != 3 || sizes[2] != 0 || sizes[0] == 0 || sizes[1] == 0 {
		t.Fatalf("record sizes %v", sizes)
	}
	if sizes[0] > maxContent || sizes[1] > maxContent {
		t.Fatalf("record sizes %v exceed %d", sizes, maxContent)
	}
}

func TestWriteParamsSplitsOversizedPair(t *testing.T) {
	var buf bytes.Buffer
	if err := writeParams(&buf, []Param{{"A", strings.Repeat("x", 70000)}}); err != nil {
		t.Fatal(err)
	}
	sizes := readAll(t, &buf)
	if len(sizes) != 3 || sizes[0] != maxContent || sizes[2] != 0 {
		t.Fatalf("record sizes %v", sizes)
	}
}

func TestReadRecordRejectsViolations(t *testing.T) {
	tests := map[string][]byte{
		"version":    {2, typeStdout, 0, 1, 0, 0, 0, 0},
		"request id": {1, typeStdout, 0, 2, 0, 0, 0, 0},
		"truncated":  {1, typeStdout, 0, 1, 0, 100, 0, 0, 'a', 'b'},
		"eof":        {},
	}
	for name, raw := range tests {
		_, _, err := readRecord(bytes.NewReader(raw), nil)
		if !errors.Is(err, ErrProtocol) {
			t.Errorf("%s: got %v, want ErrProtocol", name, err)
		}
	}
}
