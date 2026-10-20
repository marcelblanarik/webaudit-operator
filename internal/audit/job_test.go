package audit

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/marcelblanarik/webaudit-operator/api/v1alpha1"
)

const testName = "site"

var fire = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func testAudit(image string) *v1alpha1.WebAudit {
	return &v1alpha1.WebAudit{
		ObjectMeta: metav1.ObjectMeta{Name: testName, Namespace: "ns1"},
		Spec: v1alpha1.WebAuditSpec{
			URL:        "https://example.com",
			Schedule:   "*/5 * * * *",
			FormFactor: v1alpha1.FormFactorDesktop,
			Image:      image,
		},
	}
}

func env(c corev1.Container, name string) string {
	for _, e := range c.Env {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}

func TestJobName(t *testing.T) {
	if got, want := JobName("site", fire), "site-1792411200"; got != want {
		t.Fatalf("JobName = %q, want %q", got, want)
	}
	long := strings.Repeat("a", 51) + "-bbbb" // cut lands right after '-': it must not be left before the suffix
	got := JobName(long, fire)
	if len(got) > 63 || strings.Contains(got, "--") || !strings.HasSuffix(got, "-1792411200") {
		t.Fatalf("bad name %q (len %d)", got, len(got))
	}
	got = JobName(strings.Repeat("a", 50), fire)
	if len(got) > 63 || !strings.HasSuffix(got, "-1792411200") {
		t.Fatalf("bad name %q (len %d)", got, len(got))
	}
}

func TestBuildJobMetadata(t *testing.T) {
	j := BuildJob(testAudit(""), fire, "default:1")
	if j.Name != "site-1792411200" || j.Namespace != "ns1" {
		t.Fatalf("name/ns = %s/%s", j.Namespace, j.Name)
	}
	if j.Annotations[FireTimeAnnotation] != "2026-10-19T12:00:00Z" {
		t.Fatalf("annotation = %q", j.Annotations[FireTimeAnnotation])
	}
	if j.Labels[WebAuditLabel] != testName || j.Spec.Template.Labels[WebAuditLabel] != testName {
		t.Fatalf("labels job=%v pod=%v", j.Labels, j.Spec.Template.Labels)
	}
	if len(j.OwnerReferences) != 0 {
		t.Fatal("builder must not set ownerReferences")
	}
}

func TestBuildJobImage(t *testing.T) {
	if got := BuildJob(testAudit(""), fire, "default:1").Spec.Template.Spec.Containers[0].Image; got != "default:1" {
		t.Fatalf("image = %q", got)
	}
	if got := BuildJob(testAudit("custom:2"), fire, "default:1").Spec.Template.Spec.Containers[0].Image; got != "custom:2" {
		t.Fatalf("image = %q", got)
	}
}

func podFixture() (corev1.PodSpec, corev1.Container) {
	p := BuildJob(testAudit(""), fire, "default:1").Spec.Template.Spec
	return p, p.Containers[0]
}

func TestBuildJobPodBasics(t *testing.T) {
	j := BuildJob(testAudit(""), fire, "default:1")
	if j.Spec.BackoffLimit == nil || *j.Spec.BackoffLimit != 0 {
		t.Fatal("BackoffLimit must be 0")
	}
	if j.Spec.ActiveDeadlineSeconds == nil || *j.Spec.ActiveDeadlineSeconds != 300 {
		t.Fatal("ActiveDeadlineSeconds must be 300")
	}
	p := j.Spec.Template.Spec
	if p.RestartPolicy != corev1.RestartPolicyNever {
		t.Fatalf("RestartPolicy = %s", p.RestartPolicy)
	}
	if len(p.Containers) != 1 || p.Containers[0].Name != AuditContainer {
		t.Fatalf("containers = %+v", p.Containers)
	}
	c := p.Containers[0]
	if env(c, "AUDIT_URL") != "https://example.com" || env(c, "AUDIT_FORM_FACTOR") != "desktop" {
		t.Fatalf("env = %+v", c.Env)
	}
}

func TestBuildJobPodSecurityContext(t *testing.T) {
	p, c := podFixture()
	psc := p.SecurityContext
	if psc == nil || psc.RunAsNonRoot == nil || !*psc.RunAsNonRoot ||
		psc.RunAsUser == nil || *psc.RunAsUser != 1000 || psc.RunAsGroup == nil || *psc.RunAsGroup != 1000 ||
		psc.SeccompProfile == nil || psc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("pod security context = %+v", psc)
	}
	sc := c.SecurityContext
	if sc == nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation ||
		sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem ||
		sc.Capabilities == nil || len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != "ALL" {
		t.Fatalf("container security context = %+v", sc)
	}
	if p.AutomountServiceAccountToken == nil || *p.AutomountServiceAccountToken {
		t.Fatalf("automountServiceAccountToken = %v, want false", p.AutomountServiceAccountToken)
	}
}

func TestBuildJobPodVolumes(t *testing.T) {
	p, c := podFixture()
	if len(c.VolumeMounts) != 1 || c.VolumeMounts[0].MountPath != "/tmp" {
		t.Fatalf("mounts = %+v", c.VolumeMounts)
	}
	if len(p.Volumes) != 1 || p.Volumes[0].Name != c.VolumeMounts[0].Name || p.Volumes[0].EmptyDir == nil {
		t.Fatalf("volumes = %+v", p.Volumes)
	}
}

func TestBuildJobPodTermination(t *testing.T) {
	_, c := podFixture()
	if c.TerminationMessagePolicy != corev1.TerminationMessageReadFile || c.TerminationMessagePath != "/dev/termination-log" {
		t.Fatalf("termination = %s %s", c.TerminationMessagePolicy, c.TerminationMessagePath)
	}
}
