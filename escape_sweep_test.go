package json_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	goccy "github.com/goccy/go-json"
)

// TestEscapeSweepByteIdentity compares goccy v1 Marshal with encoding/json for strings of every
// length 0..96 with a special byte planted at every position: the one-pass word chain of
// AppendQuoted must cover every byte of the string at every length.
func TestEscapeSweepByteIdentity(t *testing.T) {
	type holder struct {
		S string `json:"s"`
	}
	specials := []struct {
		name string
		make func(i, n int) string
	}{
		{"clean", func(i, n int) string {
			b := make([]byte, n)
			for j := range b {
				b[j] = byte('a' + j%26)
			}
			return string(b)
		}},
		{"lt", func(i, n int) string {
			b := []byte(strings.Repeat("x", n))
			if i < n {
				b[i] = '<'
			}
			return string(b)
		}},
		{"gt", func(i, n int) string {
			b := []byte(strings.Repeat("x", n))
			if i < n {
				b[i] = '>'
			}
			return string(b)
		}},
		{"amp", func(i, n int) string {
			b := []byte(strings.Repeat("x", n))
			if i < n {
				b[i] = '&'
			}
			return string(b)
		}},
		{"quote", func(i, n int) string {
			b := []byte(strings.Repeat("x", n))
			if i < n {
				b[i] = '"'
			}
			return string(b)
		}},
		{"backslash", func(i, n int) string {
			b := []byte(strings.Repeat("x", n))
			if i < n {
				b[i] = '\\'
			}
			return string(b)
		}},
		{"control", func(i, n int) string {
			b := []byte(strings.Repeat("x", n))
			if i < n {
				b[i] = 0x01
			}
			return string(b)
		}},
		{"del", func(i, n int) string {
			b := []byte(strings.Repeat("x", n))
			if i < n {
				b[i] = 0x7f
			}
			return string(b)
		}},
		{"nbsp", func(i, n int) string { // U+00A0 as UTF-8, 2 bytes: plant at every byte offset
			b := []byte(strings.Repeat("x", n))
			if i+1 < n {
				b[i], b[i+1] = 0xc2, 0xa0
			}
			return string(b)
		}},
		{"u2028", func(i, n int) string { // line separator, 3 bytes, must be normalized
			b := []byte(strings.Repeat("x", n))
			if i+2 < n {
				b[i], b[i+1], b[i+2] = 0xe2, 0x80, 0xa8
			}
			return string(b)
		}},
		{"emoji", func(i, n int) string { // 4-byte UTF-8
			b := []byte(strings.Repeat("x", n))
			if i+3 < n {
				b[i], b[i+1], b[i+2], b[i+3] = 0xf0, 0x9f, 0x98, 0x80
			}
			return string(b)
		}},
		{"invalid", func(i, n int) string { // lone continuation byte: invalid UTF-8
			b := []byte(strings.Repeat("x", n))
			if i < n {
				b[i] = 0x80
			}
			return string(b)
		}},
		{"two", func(i, n int) string { // two specials, second at the end
			b := []byte(strings.Repeat("x", n))
			if i < n {
				b[i] = '<'
			}
			if n >= 2 {
				b[n-1] = '&'
			}
			return string(b)
		}},
	}
	for _, sp := range specials {
		for n := 0; n <= 96; n++ {
			for i := 0; i <= n; i++ {
				s := sp.make(i, n)
				want, err := json.Marshal(holder{S: s})
				if err != nil {
					t.Fatalf("%s n=%d i=%d: encoding/json: %v", sp.name, n, i, err)
				}
				got, err := goccy.Marshal(holder{S: s})
				if err != nil {
					t.Fatalf("%s n=%d i=%d: go-json: %v", sp.name, n, i, err)
				}
				if string(got) != string(want) {
					t.Fatalf("%s n=%d i=%d:\n got: %s\nwant: %s\n string: %q",
						sp.name, n, i, fmt.Sprintf("%q", got), fmt.Sprintf("%q", want), s)
				}
			}
		}
	}
}
