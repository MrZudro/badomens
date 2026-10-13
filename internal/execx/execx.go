// Package execx ejecuta procesos externos sin shell: los argumentos van
// separados, nunca interpolados en una cadena. Devuelve stdout/stderr como
// texto y un error tipado si el proceso falla o no existe.
package execx

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
)

// Result es el resultado de una ejecución.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Run ejecuta name con args y devuelve el resultado. Si el binario no existe,
// devuelve un error de dependencia ausente.
func Run(ctx context.Context, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- ejecutar procesos externos es el propósito de execx (sin shell, argumentos separados)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	res := Result{
		Stdout: strings.TrimSpace(out.String()),
		Stderr: strings.TrimSpace(errb.String()),
	}
	if err != nil {
		var exitErr *exec.ExitError
		if ok := asExitError(err, &exitErr); ok {
			res.ExitCode = exitErr.ExitCode()
			return res, errx.Wrap(errx.CodeGeneral, err, name+" falló: "+res.Stderr, "")
		}
		return res, errx.Dependency(
			"no se pudo ejecutar "+name,
			"instala "+name+" y asegúrate de que está en el PATH",
		)
	}
	return res, nil
}

// LookPath indica si un binario existe en el PATH.
func LookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func asExitError(err error, target **exec.ExitError) bool {
	// errorlint/govet: errors.As en vez de un type assert para no fallar con
	// errores envueltos (errx.Wrap); el puntero a *exec.ExitError es &ee.
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	*target = ee
	return true
}
