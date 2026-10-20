package v1alpha1_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	webauditv1alpha1 "github.com/marcelblanarik/webaudit-operator/api/v1alpha1"
)

func newAudit(spec webauditv1alpha1.WebAuditSpec) *webauditv1alpha1.WebAudit {
	return &webauditv1alpha1.WebAudit{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "t-", Namespace: "default"},
		Spec:       spec,
	}
}

func TestURLValidation(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://example.com/a?b=1", false},
		{"http://svc.ns.svc:8080", false},
		{"file:///etc/passwd", true},
		{"javascript:alert(1)", true},
		{"ftp://x", true},
		{"https://", true},
		{"", true},
	}
	for _, tc := range tests {
		t.Run(tc.url, func(t *testing.T) {
			err := k8sClient.Create(context.Background(),
				newAudit(webauditv1alpha1.WebAuditSpec{URL: tc.url, Schedule: "0 * * * *"}))
			if (err != nil) != tc.wantErr {
				t.Fatalf("url %q: wantErr=%v, got err=%v", tc.url, tc.wantErr, err)
			}
		})
	}
}

func TestFormFactorDefault(t *testing.T) {
	wa := newAudit(webauditv1alpha1.WebAuditSpec{URL: "https://example.com", Schedule: "0 * * * *"})
	if err := k8sClient.Create(context.Background(), wa); err != nil {
		t.Fatal(err)
	}
	if wa.Spec.FormFactor != webauditv1alpha1.FormFactorMobile {
		t.Fatalf("formFactor = %q, want %q", wa.Spec.FormFactor, webauditv1alpha1.FormFactorMobile)
	}
}

func TestFormFactorEnum(t *testing.T) {
	for ff, wantErr := range map[webauditv1alpha1.FormFactor]bool{
		webauditv1alpha1.FormFactorDesktop: false,
		"tablet":                           true,
	} {
		err := k8sClient.Create(context.Background(), newAudit(webauditv1alpha1.WebAuditSpec{
			URL: "https://example.com", Schedule: "0 * * * *", FormFactor: ff,
		}))
		if (err != nil) != wantErr {
			t.Fatalf("formFactor %q: wantErr=%v, got err=%v", ff, wantErr, err)
		}
	}
}

func TestScheduleRequired(t *testing.T) {
	err := k8sClient.Create(context.Background(),
		newAudit(webauditv1alpha1.WebAuditSpec{URL: "https://example.com"}))
	if err == nil {
		t.Fatal("expected empty schedule to be rejected")
	}
}
