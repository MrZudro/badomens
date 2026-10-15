//go:build !windows

package infra

import "os"

// isElevated informa si el proceso corre con privilegios (root).
func isElevated() bool { return os.Geteuid() == 0 }
