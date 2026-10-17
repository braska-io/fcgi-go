package cgi

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		status     int
		reason     string
		offset     int64
		wantHeader map[string][]string
	}{
		{"default status", "Content-Type: text/html\r\n\r\nbody", 200, "", 27,
			map[string][]string{"Content-Type": {"text/html"}}},
		{"status with reason", "Status: 404 Not Found \r\n\r\n", 404, "Not Found", 26, nil},
		{"status without reason", "Status: 299\r\n\r\n", 299, "", 15, nil},
		{"lowercase names, bare lf", "status: 201\nx-a: 1\nx-a:  2 \n\nrest", 201, "", 29,
			map[string][]string{"X-A": {"1", "2"}}},
		{"absolute location", "Location: https://x.test/\r\n\r\n", 302, "", 29,
			map[string][]string{"Location": {"https://x.test/"}}},
		{"relative location", "Location: /local\r\n\r\n", 200, "", 20, nil},
		{"location with status", "Status: 301\r\nLocation: https://x.test/\r\n\r\n", 301, "", 42, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResponse(strings.NewReader(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tt.status || got.Reason != tt.reason || got.BodyOffset != tt.offset {
				t.Errorf("got status %d reason %q offset %d", got.Status, got.Reason, got.BodyOffset)
			}
			for k, want := range tt.wantHeader {
				if g := got.Header[k]; strings.Join(g, "|") != strings.Join(want, "|") {
					t.Errorf("%s = %q, want %q", k, g, want)
				}
			}
			if _, ok := got.Header["Status"]; ok {
				t.Error("Status leaked into the header")
			}
		})
	}
}

func TestParseResponseErrors(t *testing.T) {
	tests := map[string]string{
		"no terminator": "Content-Type: text/html\r\nX: y",
		"empty":         "",
		"bad status":    "Status: abc\r\n\r\n",
		"no colon":      "garbage\r\n\r\n",
		"too large":     strings.Repeat("X-A: "+strings.Repeat("a", 1000)+"\r\n", 1100),
	}
	for name, in := range tests {
		if _, err := ParseResponse(strings.NewReader(in)); !errors.Is(err, ErrResponse) {
			t.Errorf("%s: got %v, want ErrResponse", name, err)
		}
	}
}

func TestRemoveHopByHop(t *testing.T) {
	h := http.Header{}
	h.Set("Connection", "X-Hop")
	h.Set("X-Hop", "1")
	h.Set("Keep-Alive", "5")
	h.Set("Content-Length", "3")
	RemoveHopByHop(h)
	if len(h) != 1 || h.Get("Content-Length") != "3" {
		t.Errorf("got %v", h)
	}
}
