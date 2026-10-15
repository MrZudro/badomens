//go:build windows

package infra

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// Banderas de CreateProcess para un proceso desacoplado.
const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// spawnDetached arranca un proceso desacoplado de este (daemon de sesión) sin
// usar shell.
func spawnDetached(_ context.Context, exe string, args ...string) error {
	cmd := exec.Command(exe, args...) // #nosec G204 -- proceso propio, argumentos separados, sin shell
	if devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, devnull, devnull
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | detachedProcess,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
