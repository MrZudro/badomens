//go:build windows

package infra

import "syscall"

// procIsUserAnAdmin llama a shell32.IsUserAnAdmin, que informa si el proceso
// actual corre elevado (UAC). Evita depender de golang.org/x/sys.
var procIsUserAnAdmin = syscall.NewLazyDLL("shell32.dll").NewProc("IsUserAnAdmin")

// isElevated informa si el proceso corre con privilegios de administrador.
func isElevated() bool {
	r, _, _ := procIsUserAnAdmin.Call()
	return r != 0
}
