package svc

import (
	"context"
	"errors"
	"testing"

	"github.com/MrZudro/badomens/internal/instance"
)

// accessSpec construye una instancia con whitelist y operadores iniciales.
func accessSpec(slug string) *instance.Spec {
	return &instance.Spec{
		Slug: slug, Name: "N " + slug, Engine: "paper", Version: "1.21.6",
		Memory: "4G", Port: 25565, MaxPlayers: 20,
		WhitelistEnabled: true,
		Whitelist:        []string{"Alex"},
		Ops:              []string{"Bob"},
	}
}

func hasName(list []string, name string) bool {
	for _, n := range list {
		if n == name {
			return true
		}
	}
	return false
}

// TestAccessReadLive comprueba que la lectura live devuelve las listas del spec.
func TestAccessReadLive(t *testing.T) {
	ctx := context.Background()
	s := NewFakeWith([]*instance.Spec{accessSpec("alpha")}, FakeOptions{})

	st, err := s.Access.Read(ctx, "alpha")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if st.Source != AccessLive {
		t.Fatalf("Source = %q, quiero %q", st.Source, AccessLive)
	}
	if st.VerifiedAt.IsZero() {
		t.Fatal("VerifiedAt debe estar fijado en lectura live")
	}
	if !st.WhitelistEnabled {
		t.Fatal("WhitelistEnabled debe ser true")
	}
	if !hasName(st.Whitelist, "Alex") || len(st.Whitelist) != 1 {
		t.Fatalf("Whitelist = %v", st.Whitelist)
	}
	if !hasName(st.Operators, "Bob") || len(st.Operators) != 1 {
		t.Fatalf("Operators = %v", st.Operators)
	}
}

// TestAccessReadSources verifica el origen del estado y el error de lectura.
func TestAccessReadSources(t *testing.T) {
	ctx := context.Background()
	rerr := errors.New("servidor apagado")
	cases := []struct {
		name    string
		opts    FakeOptions
		wantSrc AccessSource
		wantErr error
		live    bool
	}{
		{"live", FakeOptions{}, AccessLive, nil, true},
		{"solo guardado", FakeOptions{AccessSavedOnly: true}, AccessSavedConfig, nil, false},
		{"error de lectura", FakeOptions{AccessReadErr: rerr}, AccessUnavailable, rerr, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewFakeWith([]*instance.Spec{accessSpec("alpha")}, tc.opts)
			st, err := s.Access.Read(ctx, "alpha")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, quiero %v", err, tc.wantErr)
				}
				if st.Source != AccessUnavailable {
					t.Fatalf("Source = %q, quiero %q", st.Source, AccessUnavailable)
				}
				return
			}
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if st.Source != tc.wantSrc {
				t.Fatalf("Source = %q, quiero %q", st.Source, tc.wantSrc)
			}
			if !hasName(st.Whitelist, "Alex") {
				t.Fatalf("Whitelist = %v, quiero Alex", st.Whitelist)
			}
			if tc.live && st.VerifiedAt.IsZero() {
				t.Fatal("VerifiedAt debe estar fijado")
			}
		})
	}
}

// TestAccessAddWhitelistVerificado comprueba el read-back live tras añadir.
func TestAccessAddWhitelistVerificado(t *testing.T) {
	ctx := context.Background()
	s := NewFakeWith([]*instance.Spec{accessSpec("alpha")}, FakeOptions{})

	st, err := s.Access.AddWhitelist(ctx, "alpha", "Carol")
	if err != nil {
		t.Fatalf("AddWhitelist: %v", err)
	}
	if st.Source != AccessLive {
		t.Fatalf("Source = %q, quiero %q", st.Source, AccessLive)
	}
	if !hasName(st.Whitelist, "Carol") || len(st.Whitelist) != 2 {
		t.Fatalf("Whitelist = %v", st.Whitelist)
	}
	// La lectura posterior también refleja el cambio.
	again, _ := s.Access.Read(ctx, "alpha")
	if !hasName(again.Whitelist, "Carol") {
		t.Fatalf("Read tras add = %v", again.Whitelist)
	}
}

// TestAccessAddWhitelistVerifyFail simula write OK pero read-back no verificable.
func TestAccessAddWhitelistVerifyFail(t *testing.T) {
	ctx := context.Background()
	s := NewFakeWith([]*instance.Spec{accessSpec("alpha")}, FakeOptions{AccessVerifyFail: true})

	st, err := s.Access.AddWhitelist(ctx, "alpha", "Carol")
	if err != nil {
		t.Fatalf("AddWhitelist: %v", err)
	}
	if st.Source != AccessSavedConfig {
		t.Fatalf("Source = %q, quiero %q", st.Source, AccessSavedConfig)
	}
	if !hasName(st.Whitelist, "Carol") {
		t.Fatalf("Whitelist = %v, quiero Carol aplicado", st.Whitelist)
	}
	if !st.VerifiedAt.IsZero() {
		t.Fatal("VerifiedAt debe ser cero si no se pudo verificar")
	}
}

// TestAccessAddWhitelistIdempotente evita duplicados (comparación sin caja).
func TestAccessAddWhitelistIdempotente(t *testing.T) {
	ctx := context.Background()
	s := NewFakeWith([]*instance.Spec{accessSpec("alpha")}, FakeOptions{})

	if _, err := s.Access.AddWhitelist(ctx, "alpha", "alex"); err != nil {
		t.Fatalf("AddWhitelist: %v", err)
	}
	st, err := s.Access.AddWhitelist(ctx, "alpha", "Alex")
	if err != nil {
		t.Fatalf("AddWhitelist: %v", err)
	}
	if len(st.Whitelist) != 1 {
		t.Fatalf("Whitelist = %v, no debe duplicar", st.Whitelist)
	}
}

// TestAccessRemoveWhitelist quita al jugador.
func TestAccessRemoveWhitelist(t *testing.T) {
	ctx := context.Background()
	s := NewFakeWith([]*instance.Spec{accessSpec("alpha")}, FakeOptions{})

	st, err := s.Access.RemoveWhitelist(ctx, "alpha", "Alex")
	if err != nil {
		t.Fatalf("RemoveWhitelist: %v", err)
	}
	if st.Source != AccessLive {
		t.Fatalf("Source = %q", st.Source)
	}
	if len(st.Whitelist) != 0 {
		t.Fatalf("Whitelist = %v, quiero vacía", st.Whitelist)
	}
}

// TestAccessSetWhitelistEnabled table-driven para on/off.
func TestAccessSetWhitelistEnabled(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		enabled bool
	}{
		{"apagar", false},
		{"encender", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := accessSpec("alpha")
			spec.WhitelistEnabled = !tc.enabled
			s := NewFakeWith([]*instance.Spec{spec}, FakeOptions{})

			st, err := s.Access.SetWhitelistEnabled(ctx, "alpha", tc.enabled)
			if err != nil {
				t.Fatalf("SetWhitelistEnabled: %v", err)
			}
			if st.WhitelistEnabled != tc.enabled {
				t.Fatalf("WhitelistEnabled = %v, quiero %v", st.WhitelistEnabled, tc.enabled)
			}
			if st.Source != AccessLive {
				t.Fatalf("Source = %q", st.Source)
			}
		})
	}
}

// TestAccessOperators añade y quita operadores.
func TestAccessOperators(t *testing.T) {
	ctx := context.Background()
	s := NewFakeWith([]*instance.Spec{accessSpec("alpha")}, FakeOptions{})

	st, err := s.Access.AddOperator(ctx, "alpha", "Dana")
	if err != nil {
		t.Fatalf("AddOperator: %v", err)
	}
	if !hasName(st.Operators, "Dana") || len(st.Operators) != 2 {
		t.Fatalf("Operators = %v", st.Operators)
	}

	st, err = s.Access.RemoveOperator(ctx, "alpha", "Bob")
	if err != nil {
		t.Fatalf("RemoveOperator: %v", err)
	}
	if hasName(st.Operators, "Bob") || !hasName(st.Operators, "Dana") {
		t.Fatalf("Operators = %v, quiero solo Dana", st.Operators)
	}
}

// TestValidatePlayerName cubre trim y rechazo de vacío/control.
func TestValidatePlayerName(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"normal", "Steve", "Steve", false},
		{"con espacios", "  Alex  ", "Alex", false},
		{"vacío", "", "", true},
		{"solo espacios", "   ", "", true},
		{"control", "bad\x00name", "", true},
		{"tabulador", "a\tb", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidatePlayerName(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("quiero error para %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidatePlayerName(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("got %q, quiero %q", got, tc.want)
			}
		})
	}
}

// TestAccessValidacionNoMuta asegura que un nombre inválido no cambia el estado.
func TestAccessValidacionNoMuta(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		in   string
	}{
		{"vacío", ""},
		{"espacios", "   "},
		{"control", "bad\x00name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewFakeWith([]*instance.Spec{accessSpec("alpha")}, FakeOptions{})
			if _, err := s.Access.AddWhitelist(ctx, "alpha", tc.in); err == nil {
				t.Fatalf("AddWhitelist(%q) debe fallar", tc.in)
			}
			if _, err := s.Access.AddOperator(ctx, "alpha", tc.in); err == nil {
				t.Fatalf("AddOperator(%q) debe fallar", tc.in)
			}
			st, err := s.Access.Read(ctx, "alpha")
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if len(st.Whitelist) != 1 || !hasName(st.Whitelist, "Alex") {
				t.Fatalf("Whitelist mutó: %v", st.Whitelist)
			}
			if len(st.Operators) != 1 || !hasName(st.Operators, "Bob") {
				t.Fatalf("Operators mutó: %v", st.Operators)
			}
		})
	}
}
