package infra

import "runtime"

// goos devuelve el sistema operativo real de compilación.
func goos() string { return runtime.GOOS }
