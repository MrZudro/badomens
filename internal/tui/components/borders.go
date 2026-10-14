package components

import "github.com/charmbracelet/lipgloss"

// Border devuelve el borde de caja: ASCII (+-|) si ascii es true, redondeado si
// no. El modo ASCII se activa con BADOMENS_ASCII=1 o --ascii (§4.2).
func Border(ascii bool) lipgloss.Border {
	if ascii {
		return lipgloss.Border{
			Top: "-", Bottom: "-", Left: "|", Right: "|",
			TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+",
		}
	}
	return lipgloss.RoundedBorder()
}

// Pointer es el cursor de listas ("▶" o ">" en ASCII).
func Pointer(ascii bool) string {
	if ascii {
		return ">"
	}
	return "▶"
}

// Warn devuelve el icono de aviso (⚠ o [!!]).
func Warn(ascii bool) string {
	if ascii {
		return "[!!]"
	}
	return "⚠"
}

// Check devuelve el icono de correcto (✓ o [OK]).
func Check(ascii bool) string {
	if ascii {
		return "[OK]"
	}
	return "✓"
}

// Cross devuelve el icono de error (✖ o [XX]).
func Cross(ascii bool) string {
	if ascii {
		return "[XX]"
	}
	return "✖"
}
