// Package metrics exposes per-WebAudit Prometheus metrics on the
// controller-runtime registry.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/types"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/marcelblanarik/webaudit-operator/internal/audit"
)

// Outcome is the result label of webaudit_runs_total.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailed  Outcome = "failed"
	OutcomeBreach  Outcome = "breach"
)

const (
	labelNamespace = "namespace"
	labelName      = "name"
)

var keyLabels = []string{labelNamespace, labelName}

func gaugeVec(name, help string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, keyLabels)
}

var (
	lcpSeconds   = gaugeVec("webaudit_lcp_seconds", "Largest Contentful Paint of the last audit, in seconds.")
	cls          = gaugeVec("webaudit_cls", "Cumulative Layout Shift of the last audit.")
	tbtSeconds   = gaugeVec("webaudit_tbt_seconds", "Total Blocking Time of the last audit, in seconds.")
	thresholdMet = gaugeVec("webaudit_threshold_met", "1 if the last completed audit met all thresholds, 0 if it breached one.")
	lastRun      = gaugeVec("webaudit_last_run_timestamp_seconds", "Unix time of the last audit that produced a result.")
	runsTotal    = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "webaudit_runs_total",
		Help: "Audit runs by result (success, failed, breach).",
	}, []string{labelNamespace, labelName, "result"})
)

func init() {
	crmetrics.Registry.MustRegister(lcpSeconds, cls, tbtSeconds, thresholdMet, lastRun, runsTotal)
}

// ObserveRun records one finished audit run. r may be nil when no result was produced.
func ObserveRun(key types.NamespacedName, o Outcome, r *audit.Result, at time.Time) {
	runsTotal.WithLabelValues(key.Namespace, key.Name, string(o)).Inc()
	if r != nil {
		lcpSeconds.WithLabelValues(key.Namespace, key.Name).Set(r.LCPMs / 1000)
		tbtSeconds.WithLabelValues(key.Namespace, key.Name).Set(r.TBTMs / 1000)
		cls.WithLabelValues(key.Namespace, key.Name).Set(r.CLS)
		lastRun.WithLabelValues(key.Namespace, key.Name).Set(float64(at.Unix()))
	}
	switch o {
	case OutcomeSuccess:
		thresholdMet.WithLabelValues(key.Namespace, key.Name).Set(1)
	case OutcomeBreach:
		thresholdMet.WithLabelValues(key.Namespace, key.Name).Set(0)
	}
}

// Forget removes every series belonging to key, including all result values of the counter.
func Forget(key types.NamespacedName) {
	l := prometheus.Labels{labelNamespace: key.Namespace, labelName: key.Name}
	for _, c := range []interface{ DeletePartialMatch(prometheus.Labels) int }{
		lcpSeconds, cls, tbtSeconds, thresholdMet, lastRun, runsTotal,
	} {
		c.DeletePartialMatch(l)
	}
}
