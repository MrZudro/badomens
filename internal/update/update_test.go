package update

import "testing"

func TestNewer(t *testing.T) {
	cases := []struct {
		candidate string
		current   string
		want      bool
	}{
		{"1.2.3", "1.2.2", true},
		{"1.2.2", "1.2.3", false},
		{"2.0.0", "1.9.9", true},
		{"1.2.3", "1.2.3", false},
		{"v0.2.0", "0.1.0", true},
		{"1.2.3", "1.2.3-dev", true},
		{"1.0.0-rc1", "1.0.0", false},
	}
	for _, c := range cases {
		if got := newer(c.candidate, c.current); got != c.want {
			t.Errorf("newer(%q, %q) = %v; quiero %v", c.candidate, c.current, got, c.want)
		}
	}
}

func TestParseSemver(t *testing.T) {
	s := parseSemver("v1.4.2-beta.1")
	if s.nums != [3]int{1, 4, 2} {
		t.Fatalf("nums = %v", s.nums)
	}
	if s.pre != "beta.1" {
		t.Fatalf("pre = %q", s.pre)
	}
}
