package result

// Outcome clasifica el DESENLACE de una operación que modifica estado (AC-10):
// no basta con informar de éxito o error, hay que decir si la operación terminó,
// falló o quedó parcialmente aplicada. Se publica en `data.outcome` y acompaña a
// las `phases` del documento.
type Outcome string

const (
	// OutcomeApplied: la operación terminó por completo y su efecto está aplicado
	// y verificado.
	OutcomeApplied Outcome = "applied"
	// OutcomeFailed: la operación falló sin dejar ningún efecto aplicado (estado
	// recuperable: se puede reintentar tal cual).
	OutcomeFailed Outcome = "failed"
	// OutcomePartial: la operación solo se aplicó EN PARTE (por ejemplo, la
	// configuración quedó escrita pero el runtime no se pudo reiniciar). El
	// consumidor debe reintentar SOLO la parte pendiente.
	OutcomePartial Outcome = "partial"
)

// String devuelve el identificador estable del desenlace.
func (o Outcome) String() string { return string(o) }

// PhaseFor construye una fase cuyo estado se deriva del desenlace: ok, failed o
// partial. Una operación parcial deja escrita la fase que no terminó, en lugar
// de informar de éxito antes de tiempo (T-08/AC-10).
func PhaseFor(name string, o Outcome) Phase {
	status := "ok"
	switch o {
	case OutcomeFailed:
		status = "failed"
	case OutcomePartial:
		status = "partial"
	}
	return Phase{Name: name, Status: status}
}
