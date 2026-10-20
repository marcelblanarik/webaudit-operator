package audit

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/marcelblanarik/webaudit-operator/api/v1alpha1"
)

func TestParseResult(t *testing.T) {
	r, err := ParseResult([]byte(`{"lcpMs":2400.5,"tbtMs":150,"cls":0.05}`))
	if err != nil || r != (Result{LCPMs: 2400.5, TBTMs: 150, CLS: 0.05}) {
		t.Fatalf("got %+v, %v", r, err)
	}
	// zero values are valid, only absence is an error
	if _, err := ParseResult([]byte(`{"lcpMs":0,"tbtMs":0,"cls":0}`)); err != nil {
		t.Errorf("zeros: %v", err)
	}
}

func TestParseResultInvalid(t *testing.T) {
	for _, in := range []string{
		``, `{`, `NaN`, `{"lcpMs":1}`, `{"lcpMs":1,"tbtMs":1}`,
		`{"lcpMs":-1,"tbtMs":0,"cls":0}`, `{"lcpMs":0,"tbtMs":-1,"cls":0}`, `{"lcpMs":0,"tbtMs":0,"cls":-0.1}`,
		`{"lcpMs":NaN,"tbtMs":0,"cls":0}`, `{"lcpMs":null,"tbtMs":0,"cls":0}`,
	} {
		if _, err := ParseResult([]byte(in)); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}

func TestBreaches(t *testing.T) {
	lcp := metav1.Duration{Duration: 2500 * time.Millisecond}
	tbt := metav1.Duration{Duration: 200 * time.Millisecond}
	cls := resource.MustParse("0.1")

	if got := Breaches(v1alpha1.Thresholds{LCP: &lcp}, Result{LCPMs: 2500}); len(got) != 0 {
		t.Errorf("equality should pass: %v", got)
	}
	got := Breaches(v1alpha1.Thresholds{LCP: &lcp}, Result{LCPMs: 3100})
	if len(got) != 1 || got[0] != "lcp 3100ms > 2500ms" {
		t.Errorf("got %v", got)
	}
	if got := Breaches(v1alpha1.Thresholds{LCP: &lcp}, Result{LCPMs: 2501}); len(got) != 1 {
		t.Errorf("2501: %v", got)
	}
	if got := Breaches(v1alpha1.Thresholds{TBT: &tbt}, Result{TBTMs: 250}); len(got) != 1 || got[0] != "tbt 250ms > 200ms" {
		t.Errorf("tbt: %v", got)
	}
	if got := Breaches(v1alpha1.Thresholds{CLS: &cls}, Result{CLS: 0.11}); len(got) != 1 || got[0] != "cls 0.11 > 0.1" {
		t.Errorf("cls: %v", got)
	}
	if got := Breaches(v1alpha1.Thresholds{CLS: &cls}, Result{CLS: 0.1}); len(got) != 0 {
		t.Errorf("cls equality: %v", got)
	}
	if got := Breaches(v1alpha1.Thresholds{}, Result{LCPMs: 9e9, TBTMs: 9e9, CLS: 9}); len(got) != 0 {
		t.Errorf("empty: %v", got)
	}
	all := Breaches(v1alpha1.Thresholds{LCP: &lcp, TBT: &tbt, CLS: &cls}, Result{LCPMs: 3000, TBTMs: 300, CLS: 0.5})
	if len(all) != 3 {
		t.Errorf("all: %v", all)
	}
}
