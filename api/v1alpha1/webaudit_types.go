package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// FormFactor selects the Lighthouse emulation profile.
// +kubebuilder:validation:Enum=mobile;desktop
type FormFactor string

const (
	FormFactorMobile  FormFactor = "mobile"
	FormFactorDesktop FormFactor = "desktop"
)

// Condition types and reasons set on WebAudit.status.conditions.
const (
	ConditionReady         = "Ready"
	ConditionThresholdsMet = "ThresholdsMet"

	ReasonInvalidSchedule    = "InvalidSchedule"
	ReasonAuditFailed        = "AuditFailed"
	ReasonThresholdsBreached = "ThresholdsBreached"
	ReasonThresholdsMet      = "ThresholdsMet"
	ReasonScheduled          = "Scheduled"
)

// Thresholds are upper bounds for the audited metrics. An omitted field is not enforced.
type Thresholds struct {
	// lcp is the maximum Largest Contentful Paint, e.g. 2500ms.
	// +optional
	LCP *metav1.Duration `json:"lcp,omitempty"`

	// tbt is the maximum Total Blocking Time, e.g. 200ms.
	// +optional
	TBT *metav1.Duration `json:"tbt,omitempty"`

	// cls is the maximum Cumulative Layout Shift, e.g. "0.1".
	// +optional
	CLS *resource.Quantity `json:"cls,omitempty"`
}

// WebAuditSpec defines the desired state of WebAudit
type WebAuditSpec struct {
	// url is the page to audit. Only http and https URLs are accepted.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self.matches('^https?://[^/\\\\s]+')",message="url must be an http(s) URL with a host"
	URL string `json:"url"`

	// schedule is a 5-field cron expression, evaluated in UTC.
	// +required
	// +kubebuilder:validation:MinLength=1
	Schedule string `json:"schedule"`

	// formFactor selects mobile or desktop emulation.
	// +optional
	// +kubebuilder:default=mobile
	FormFactor FormFactor `json:"formFactor,omitempty"`

	// thresholds are the pass/fail limits for the audit.
	// +optional
	Thresholds Thresholds `json:"thresholds,omitzero"`

	// image overrides the default audit image.
	// +optional
	Image string `json:"image,omitempty"`
}

// LastRun summarises the most recent finished audit Job.
type LastRun struct {
	// jobName is the name of the Job that produced this result.
	JobName string `json:"jobName"`

	// startTime is when the Job started.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// completionTime is when the Job finished.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// lcp is the measured Largest Contentful Paint.
	// +optional
	LCP *metav1.Duration `json:"lcp,omitempty"`

	// tbt is the measured Total Blocking Time.
	// +optional
	TBT *metav1.Duration `json:"tbt,omitempty"`

	// cls is the measured Cumulative Layout Shift.
	// +optional
	CLS *resource.Quantity `json:"cls,omitempty"`

	// passed reports whether all configured thresholds were met. Unset if the audit itself failed.
	// +optional
	Passed *bool `json:"passed,omitempty"`

	// message carries a human-readable result or failure detail.
	// +optional
	Message string `json:"message,omitempty"`
}

// WebAuditStatus defines the observed state of WebAudit.
type WebAuditStatus struct {
	// observedGeneration is the spec generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the WebAudit resource.
	// Types are Ready and ThresholdsMet.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// lastRun is the most recent finished audit.
	// +optional
	LastRun *LastRun `json:"lastRun,omitempty"`

	// nextRunTime is when the next audit is scheduled.
	// +optional
	NextRunTime *metav1.Time `json:"nextRunTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.spec.url`
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="ThresholdsMet",type=string,JSONPath=`.status.conditions[?(@.type=="ThresholdsMet")].status`
// +kubebuilder:printcolumn:name="LastRun",type=date,JSONPath=`.status.lastRun.startTime`

// WebAudit is the Schema for the webaudits API
type WebAudit struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of WebAudit
	// +required
	Spec WebAuditSpec `json:"spec"`

	// status defines the observed state of WebAudit
	// +optional
	Status WebAuditStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// WebAuditList contains a list of WebAudit
type WebAuditList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []WebAudit `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &WebAudit{}, &WebAuditList{})
		return nil
	})
}
