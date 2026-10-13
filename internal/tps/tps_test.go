package tps

import "testing"

func TestParsePercent(t *testing.T) {
	if got := parsePercent("12.5%"); got != 12.5 {
		t.Fatalf("parsePercent = %v", got)
	}
	if got := parsePercent("0%"); got != 0 {
		t.Fatalf("parsePercent(0) = %v", got)
	}
}

func TestParseMemUsage(t *testing.T) {
	used, limit := parseMemUsage("1.5GiB / 4GiB")
	if used < 1530 || used > 1540 {
		t.Fatalf("used = %v", used)
	}
	if limit < 4090 || limit > 4100 {
		t.Fatalf("limit = %v", limit)
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]float64{
		"512MiB": 512,
		"1GiB":   1024,
		"128KiB": 0.125,
		"2GiB":   2048,
	}
	for in, want := range cases {
		if got := parseSize(in); got != want {
			t.Errorf("parseSize(%q) = %v; quiero %v", in, got, want)
		}
	}
}

func TestScoreGood(t *testing.T) {
	r := &Report{AvgTPS: 20.0, Samples: []Sample{{TPS: 20.0, MSPT: 5}}}
	scoreAndSuggest(r)
	if r.Score != "good" {
		t.Fatalf("score = %s", r.Score)
	}
}

func TestScoreDegradedAndCritical(t *testing.T) {
	r := &Report{AvgTPS: 17.0}
	scoreAndSuggest(r)
	if r.Score != "degraded" {
		t.Fatalf("17 tps → %s; quiero degraded", r.Score)
	}
	r = &Report{AvgTPS: 10.0}
	scoreAndSuggest(r)
	if r.Score != "critical" {
		t.Fatalf("10 tps → %s; quiero critical", r.Score)
	}
	r = &Report{LagEvents: 12}
	scoreAndSuggest(r)
	if r.Score != "critical" {
		t.Fatalf("lag severo → %s; quiero critical", r.Score)
	}
}

func TestScoreCPUHigh(t *testing.T) {
	r := &Report{AvgTPS: -1, CPUAvg: 95}
	scoreAndSuggest(r)
	if r.Score != "degraded" {
		t.Fatalf("cpu 95 → %s; quiero degraded", r.Score)
	}
	if len(r.Suggestions) == 0 {
		t.Fatal("falta sugerencia por CPU alta")
	}
}
