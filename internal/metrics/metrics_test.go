package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"k8s.io/apimachinery/pkg/types"

	"github.com/marcelblanarik/webaudit-operator/internal/audit"
)

var all = []prometheus.Collector{lcpSeconds, cls, tbtSeconds, thresholdMet, lastRun, runsTotal}

func total() int {
	n := 0
	for _, c := range all {
		n += testutil.CollectAndCount(c)
	}
	return n
}

func gauge(t *testing.T, g *prometheus.GaugeVec, key types.NamespacedName) float64 {
	t.Helper()
	return testutil.ToFloat64(g.WithLabelValues(key.Namespace, key.Name))
}

func runs(key types.NamespacedName, result Outcome) float64 {
	return testutil.ToFloat64(runsTotal.WithLabelValues(key.Namespace, key.Name, string(result)))
}

func TestObserveRunSuccess(t *testing.T) {
	key := types.NamespacedName{Namespace: "t-success", Name: "a"}
	t.Cleanup(func() { Forget(key) })
	at := time.Unix(1700000000, 0)

	ObserveRun(key, OutcomeSuccess, &audit.Result{LCPMs: 2300, TBTMs: 150, CLS: 0.05}, at)

	for name, tc := range map[string]struct {
		got, want float64
	}{
		"lcp":       {gauge(t, lcpSeconds, key), 2.3},
		"tbt":       {gauge(t, tbtSeconds, key), 0.15},
		"cls":       {gauge(t, cls, key), 0.05},
		"threshold": {gauge(t, thresholdMet, key), 1},
		"last_run":  {gauge(t, lastRun, key), 1700000000},
		"runs":      {runs(key, OutcomeSuccess), 1},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", name, tc.got, tc.want)
		}
	}
}

func TestObserveRunBreachSetsThresholdZero(t *testing.T) {
	key := types.NamespacedName{Namespace: "t-breach", Name: "a"}
	t.Cleanup(func() { Forget(key) })
	r := &audit.Result{LCPMs: 4000, TBTMs: 500, CLS: 0.3}

	ObserveRun(key, OutcomeSuccess, r, time.Now())
	ObserveRun(key, OutcomeBreach, r, time.Now())

	if got := gauge(t, thresholdMet, key); got != 0 {
		t.Errorf("threshold_met = %v, want 0", got)
	}
	if got := runs(key, OutcomeBreach); got != 1 {
		t.Errorf("breach runs = %v, want 1", got)
	}
}

func TestObserveRunFailedNilTouchesOnlyCounter(t *testing.T) {
	key := types.NamespacedName{Namespace: "t-failed", Name: "a"}
	t.Cleanup(func() { Forget(key) })

	ObserveRun(key, OutcomeFailed, nil, time.Now())

	if got := runs(key, OutcomeFailed); got != 1 {
		t.Errorf("failed runs = %v, want 1", got)
	}
	if got := total(); got != 1 {
		t.Errorf("series count = %d, want 1 (counter only)", got)
	}
}

func TestObserveRunFailedWithResultLeavesThreshold(t *testing.T) {
	key := types.NamespacedName{Namespace: "t-failed-r", Name: "a"}
	t.Cleanup(func() { Forget(key) })
	r := &audit.Result{LCPMs: 1000}

	ObserveRun(key, OutcomeSuccess, r, time.Now())
	ObserveRun(key, OutcomeFailed, r, time.Now())

	if got := gauge(t, thresholdMet, key); got != 1 {
		t.Errorf("threshold_met = %v, want untouched 1", got)
	}
}

func TestForgetRemovesAllSeriesForKeyOnly(t *testing.T) {
	gone := types.NamespacedName{Namespace: "t-forget", Name: "gone"}
	kept := types.NamespacedName{Namespace: "t-forget", Name: "kept"}
	t.Cleanup(func() { Forget(gone); Forget(kept) })
	r := &audit.Result{LCPMs: 1000, TBTMs: 100, CLS: 0.1}
	at := time.Now()

	for _, k := range []types.NamespacedName{gone, kept} {
		ObserveRun(k, OutcomeSuccess, r, at)
		ObserveRun(k, OutcomeBreach, r, at)
		ObserveRun(k, OutcomeFailed, nil, at)
	}
	// 5 gauges + 3 counter series per key.
	if got := total(); got != 16 {
		t.Fatalf("series before Forget = %d, want 16", got)
	}

	Forget(gone)

	if got := total(); got != 8 {
		t.Errorf("series after Forget = %d, want 8 (only %v left)", got, kept)
	}
	if got := runs(kept, OutcomeBreach); got != 1 {
		t.Errorf("kept breach runs = %v, want 1", got)
	}
}
