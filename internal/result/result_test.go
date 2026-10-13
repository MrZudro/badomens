package result

import (
	"testing"

	"github.com/manuelserver/badomens/internal/errx"
)

func TestFromErrorNotFound(t *testing.T) {
	r := FromError("instance", errx.NotFound("no existe", "revisa el nombre"))
	if r.OK {
		t.Fatal("un error no puede tener ok=true")
	}
	if r.ExitCode != 2 {
		t.Fatalf("exit_code = %d; quiero 2", r.ExitCode)
	}
	if r.Error == nil || r.Error.Code != errx.CodeNotFound {
		t.Fatalf("code = %+v", r.Error)
	}
}

func TestWarnExitCode(t *testing.T) {
	r := Warn("doctor", "con avisos", nil)
	if r.ExitCode != 1 {
		t.Fatalf("exit_code = %d; quiero 1", r.ExitCode)
	}
	if errx.ExitCode(errx.Warn("x", "")) != 1 {
		t.Fatal("errx.ExitCode(warn) debe ser 1")
	}
}

func TestOKExitCode(t *testing.T) {
	if got := OK("version", "ok", nil).ExitCode; got != 0 {
		t.Fatalf("exit_code = %d; quiero 0", got)
	}
	if errx.ExitCode(nil) != 0 {
		t.Fatal("errx.ExitCode(nil) debe ser 0")
	}
}
