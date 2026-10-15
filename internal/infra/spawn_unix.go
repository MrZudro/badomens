//go:build !windows

package infra

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// spawnDetached arranca un proceso desacoplado de este (daemon de sesión) sin
// usar shell. Setsid lo separa del grupo de procesos y del terminal actual.
func spawnDetached(_ context.Context, exe string, args ...string) error {
	cmd := exec.Command(exe, args...) // #nosec G204 -- proceso propio, argumentos separados, sin shell
	if devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, devnull, devnull
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
