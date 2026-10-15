package tui

// zoneFocus es el foco entre las zonas/paneles de la pantalla actual: Tab avanza
// y Shift+Tab retrocede (SPEC 2 §1.2: la etiqueta definitiva de Tab es
// «Desplazarse»), sin cambiar de pantalla. La navegación de primer nivel dejó de
// depender de una fila de pestañas (SPEC 2 §15), así que Tab ya no cicla
// pantallas: desplaza el foco entre las zonas/paneles del cuerpo. Cada pantalla
// declara cuántas zonas visibles tiene en el ancho del último View; con una sola
// zona no hay a dónde desplazarse y Tab no hace nada.
type zoneFocus struct {
	idx int // zona con foco (0-based)
	n   int // zonas declaradas por la pantalla en el último View
}

// set declara el número de zonas visibles (0 = la pantalla no tiene zonas).
func (z *zoneFocus) set(n int) {
	if n < 0 {
		n = 0
	}
	z.n = n
	if n == 0 {
		z.idx = 0
		return
	}
	if z.idx >= n {
		z.idx = n - 1
	}
}

// move desplaza el foco (d = 1 siguiente, d = -1 anterior) y devuelve si hubo
// movimiento: con menos de dos zonas no hay nada que desplazar.
func (z *zoneFocus) move(d int) bool {
	if z.n < 2 {
		return false
	}
	z.idx = ((z.idx+d)%z.n + z.n) % z.n
	return true
}

// active indica si la zona i (0-based) tiene el foco.
func (z *zoneFocus) active(i int) bool { return i == z.idx }
