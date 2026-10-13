package setup

import "testing"

func TestRecommendMaxPlayers(t *testing.T) {
	cases := []struct {
		gb     float64
		modded bool
		want   int
	}{
		{1, false, 4},
		{2, false, 8},
		{4, false, 15},
		{6, false, 25},
		{8, false, 40},
		{16, false, 40},
		{4, true, 8}, // modded: 4G → 2G efectivos → 8
	}
	for _, c := range cases {
		if got := RecommendMaxPlayers(c.gb, c.modded); got != c.want {
			t.Errorf("RecommendMaxPlayers(%.0f, %v) = %d; quiero %d", c.gb, c.modded, got, c.want)
		}
	}
}

func TestParseCommaList(t *testing.T) {
	got := ParseCommaList(" alicia, bob ,  carlos ,,")
	want := []string{"alicia", "bob", "carlos"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, quiero %v", got, want)
		}
	}
	if len(ParseCommaList(" , , ")) != 0 {
		t.Fatal("lista de comas vacía debe devolver nada")
	}
}
