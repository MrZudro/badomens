package cli

import "github.com/MrZudro/badomens/internal/infra"

// infraStateData serializa el estado tipado de un servicio. Es aditivo: no
// cambia las claves existentes del contrato JSON.
func infraStateData(st infra.State) map[string]any {
	m := map[string]any{
		"domain":  string(st.Domain),
		"status":  string(st.Status),
		"reason":  st.Reason,
		"message": st.Message,
	}
	if st.Hint != "" {
		m["hint"] = st.Hint
	}
	if st.Detail != "" {
		m["detail"] = st.Detail
	}
	if st.Elevation {
		m["elevation"] = true
	}
	if st.Extra != nil {
		m["extra"] = st.Extra
	}
	return m
}

// infraReportData serializa el resultado del mecanismo (detectar → levantar →
// elevar → informar).
func infraReportData(rep infra.Report) map[string]any {
	m := map[string]any{
		"domain":  string(rep.Domain),
		"status":  string(rep.Status),
		"reason":  rep.Reason,
		"message": rep.Message,
		"ready":   rep.Ready,
		"elapsed": rep.Elapsed,
	}
	if rep.Hint != "" {
		m["hint"] = rep.Hint
	}
	if rep.NeedsElevation {
		m["needs_elevation"] = true
	}
	if rep.Degraded {
		m["degraded"] = true
	}
	if rep.Canceled {
		m["canceled"] = true
	}
	m["attempts"] = infraAttemptsData(rep.Attempts)
	m["pending"] = infraPendingData(rep.Pending)
	m["manual"] = rep.Manual
	m["phases"] = rep.Phases
	if rep.Extra != nil {
		m["extra"] = rep.Extra
	}
	return m
}

func infraAttemptsData(as []infra.Attempt) []map[string]any {
	out := make([]map[string]any, 0, len(as))
	for _, a := range as {
		out = append(out, map[string]any{
			"description": a.Step.Description,
			"command":     a.Step.Command,
			"ok":          a.OK,
			"error":       a.Error,
		})
	}
	return out
}

func infraPendingData(ss []infra.Step) []map[string]any {
	out := make([]map[string]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, map[string]any{
			"description": s.Description,
			"command":     s.Command,
			"instruction": s.Instruction,
			"elevation":   s.RequiresElevation,
		})
	}
	return out
}
