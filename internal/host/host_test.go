package host

import "testing"

func TestParseMemory(t *testing.T) {
	cases := map[string]uint64{
		"4G":   4 * 1024 * 1024 * 1024,
		"512M": 512 * 1024 * 1024,
		"1k":   1024,
		"2048": 2048,
		"":     0,
		"abc":  0,
		"6g":   6 * 1024 * 1024 * 1024,
	}
	for in, want := range cases {
		if got := ParseMemory(in); got != want {
			t.Errorf("ParseMemory(%q) = %d; quiero %d", in, got, want)
		}
	}
}
