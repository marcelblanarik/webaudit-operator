package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/marcelblanarik/webaudit-operator/api/v1alpha1"
)

// Result is the metrics document written by the audit container.
type Result struct {
	LCPMs float64
	TBTMs float64
	CLS   float64
}

// ParseResult decodes and validates the audit container's JSON output.
func ParseResult(b []byte) (Result, error) {
	if len(b) == 0 {
		return Result{}, errors.New("empty result")
	}
	// Pointers distinguish a missing key from a zero value.
	var raw struct {
		LCPMs *float64 `json:"lcpMs"`
		TBTMs *float64 `json:"tbtMs"`
		CLS   *float64 `json:"cls"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return Result{}, fmt.Errorf("invalid result JSON: %w", err)
	}
	for name, v := range map[string]*float64{"lcpMs": raw.LCPMs, "tbtMs": raw.TBTMs, "cls": raw.CLS} {
		if v == nil {
			return Result{}, fmt.Errorf("result is missing %q", name)
		}
		if math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 {
			return Result{}, fmt.Errorf("result %q must be a finite non-negative number, got %v", name, *v)
		}
	}
	return Result{LCPMs: *raw.LCPMs, TBTMs: *raw.TBTMs, CLS: *raw.CLS}, nil
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// Breaches returns one message per metric exceeding its threshold. Unset thresholds are skipped; equality passes.
func Breaches(t v1alpha1.Thresholds, r Result) []string {
	var out []string
	if t.LCP != nil {
		if max := float64(t.LCP.Milliseconds()); r.LCPMs > max {
			out = append(out, fmt.Sprintf("lcp %sms > %sms", num(r.LCPMs), num(max)))
		}
	}
	if t.TBT != nil {
		if max := float64(t.TBT.Milliseconds()); r.TBTMs > max {
			out = append(out, fmt.Sprintf("tbt %sms > %sms", num(r.TBTMs), num(max)))
		}
	}
	if t.CLS != nil {
		if max := t.CLS.AsApproximateFloat64(); r.CLS > max {
			out = append(out, fmt.Sprintf("cls %s > %s", num(r.CLS), num(max)))
		}
	}
	return out
}
