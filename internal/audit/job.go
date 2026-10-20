package audit

import (
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/marcelblanarik/webaudit-operator/api/v1alpha1"
)

const (
	// FireTimeAnnotation records the scheduled fire time (RFC3339, UTC) on the Job.
	FireTimeAnnotation = "webaudit.simpleit.sk/fire-time"
	// WebAuditLabel carries the owning WebAudit name on Jobs and their Pods.
	WebAuditLabel = "webaudit.simpleit.sk/name"
	// AuditContainer is the name of the Lighthouse container.
	AuditContainer = "audit"

	// auditUID matches the user baked into the audit image.
	auditUID int64 = 1000
	// jobNameMax is the DNS-label limit: Job names end up in Pod labels.
	jobNameMax = 63
)

// JobName returns "<name>-<unix>", truncating the audit name so the result fits a DNS label.
func JobName(auditName string, fire time.Time) string {
	suffix := "-" + strconv.FormatInt(fire.Unix(), 10)
	if max := jobNameMax - len(suffix); len(auditName) > max {
		auditName = strings.TrimRight(auditName[:max], "-.")
	}
	return auditName + suffix
}

// BuildJob builds the Job for one scheduled fire. It sets no ownerReference; the controller does.
func BuildJob(wa *v1alpha1.WebAudit, fire time.Time, defaultImage string) *batchv1.Job {
	image := defaultImage
	if wa.Spec.Image != "" {
		image = wa.Spec.Image
	}
	// The name is capped to 52 chars by CRD validation, so it is a valid label value.
	labels := map[string]string{WebAuditLabel: wa.Name}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        JobName(wa.Name, fire),
			Namespace:   wa.Namespace,
			Labels:      labels,
			Annotations: map[string]string{FireTimeAnnotation: fire.UTC().Format(time.RFC3339)},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          new(int32(0)),
			ActiveDeadlineSeconds: new(int64(300)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   new(true),
						RunAsUser:      new(auditUID),
						RunAsGroup:     new(auditUID),
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Volumes: []corev1.Volume{{
						Name:         "tmp",
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
					}},
					Containers: []corev1.Container{{
						Name:  AuditContainer,
						Image: image,
						Env: []corev1.EnvVar{
							{Name: "AUDIT_URL", Value: wa.Spec.URL},
							{Name: "AUDIT_FORM_FACTOR", Value: string(wa.Spec.FormFactor)},
						},
						VolumeMounts: []corev1.VolumeMount{{Name: "tmp", MountPath: "/tmp"}},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: new(false),
							ReadOnlyRootFilesystem:   new(true),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
						TerminationMessagePath:   "/dev/termination-log",
						TerminationMessagePolicy: corev1.TerminationMessageReadFile,
					}},
				},
			},
		},
	}
}
