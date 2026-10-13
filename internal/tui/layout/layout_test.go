package layout

import (
	"strings"
	"testing"
)

func TestClassifyBreakpoints(t *testing.T) {
	cases := []struct {
		w, h int
		want Class
	}{
		{39, 30, TooSmall},  // ancho < 40
		{100, 11, TooSmall}, // alto < 12
		{40, 12, Compact},
		{69, 12, Compact},
		{70, 18, Normal},
		{109, 18, Normal},
		{110, 24, Wide},
		{200, 50, Wide},
	}
	for _, tc := range cases {
		if got := Classify(tc.w, tc.h); got != tc.want {
			t.Fatalf("Classify(%d, %d): quiero %s, tengo %s", tc.w, tc.h, tc.want, got)
		}
	}
	if s := (Size{W: 30, H: 10, Class: TooSmall}); !s.TooSmall() {
		t.Fatal("TooSmall debe ser detectable")
	}
}

func TestTruncateUsaAnchoReal(t *testing.T) {
	// Las letras anchas (CJK) ocupan 2 celdas.
	wide := "塞éé塞" // ancho 2 + 1 + 1 + 2 = 6
	if got := Truncate(wide, 3); got == "" || got == wide {
		t.Fatalf("truncado de ancho 6 a 3 debe cambiar: %q", got)
	}
	if got := Truncate(wide, 6); got != wide {
		t.Fatalf("si cabe, no hay que truncar: %q", got)
	}
	if !strings.HasSuffix(Truncate("abcdefgh", 4), "…") {
		t.Fatalf("quiero elipsis final: %q", Truncate("abcdefgh", 4))
	}
	if got := Truncate("abc", 1); got != "" {
		t.Fatalf("ancho 1 no admite texto: %q", got)
	}
}
