package controller

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	webauditv1alpha1 "github.com/marcelblanarik/webaudit-operator/api/v1alpha1"
	"github.com/marcelblanarik/webaudit-operator/internal/audit"
)

const (
	testNamespace = "default"
	testImage     = "audit:test"
)

var waCounter int

var _ = Describe("WebAudit Controller", func() {
	var (
		key      types.NamespacedName
		now      time.Time
		recorder *record.FakeRecorder
		r        *WebAuditReconciler
	)

	BeforeEach(func() {
		waCounter++
		key = types.NamespacedName{Namespace: testNamespace, Name: fmt.Sprintf("wa-%d", waCounter)}
		recorder = record.NewFakeRecorder(100)
		r = &WebAuditReconciler{
			Client:       k8sClient,
			Scheme:       k8sClient.Scheme(),
			Recorder:     recorder,
			DefaultImage: testImage,
			Now:          func() time.Time { return now },
		}
	})

	AfterEach(func() {
		wa := &webauditv1alpha1.WebAudit{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, wa))).To(Succeed())
		sel := client.MatchingLabels{audit.WebAuditLabel: key.Name}
		bg := client.PropagationPolicy(metav1.DeletePropagationBackground)
		Expect(k8sClient.DeleteAllOf(ctx, &batchv1.Job{}, client.InNamespace(testNamespace), sel, bg)).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace(testNamespace), sel)).To(Succeed())
	})

	createWA := func(schedule string, th webauditv1alpha1.Thresholds) *webauditv1alpha1.WebAudit {
		wa := &webauditv1alpha1.WebAudit{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
			Spec:       webauditv1alpha1.WebAuditSpec{URL: "https://example.com", Schedule: schedule, Thresholds: th},
		}
		Expect(k8sClient.Create(ctx, wa)).To(Succeed())
		// Past the first fire of "* * * * *": the latest minute boundary is at least 30s after creation.
		now = wa.CreationTimestamp.Add(90 * time.Second)
		return wa
	}

	reconcileOnce := func() ctrl.Result {
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		return res
	}

	listJobs := func() []batchv1.Job {
		var jobs batchv1.JobList
		Expect(k8sClient.List(ctx, &jobs, client.InNamespace(testNamespace),
			client.MatchingLabels{audit.WebAuditLabel: key.Name})).To(Succeed())
		return jobs.Items
	}

	getWA := func() *webauditv1alpha1.WebAudit {
		wa := &webauditv1alpha1.WebAudit{}
		Expect(k8sClient.Get(ctx, key, wa)).To(Succeed())
		return wa
	}

	condition := func(t string) *metav1.Condition {
		return meta.FindStatusCondition(getWA().Status.Conditions, t)
	}

	// The API server only accepts a terminal Job status with these fields set.
	completeJob := func(job *batchv1.Job) {
		t := metav1.NewTime(now)
		job.Status.StartTime = &t
		job.Status.CompletionTime = &t
		job.Status.Succeeded = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue, LastTransitionTime: t},
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: t},
		}
		Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())
	}

	failJob := func(job *batchv1.Job) {
		t := metav1.NewTime(now)
		job.Status.StartTime = &t
		job.Status.Failed = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", LastTransitionTime: t},
			{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", LastTransitionTime: t},
		}
		Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())
	}

	terminatedPod := func(job *batchv1.Job, message string) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      job.Name + "-pod",
				Namespace: testNamespace,
				Labels:    map[string]string{"job-name": job.Name, audit.WebAuditLabel: key.Name},
			},
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers:    []corev1.Container{{Name: audit.AuditContainer, Image: testImage}},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		pod.Status.Phase = corev1.PodSucceeded
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name:  audit.AuditContainer,
			Image: testImage,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Message: message}},
		}}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
	}

	// runOnce creates the WebAudit and reconciles it into exactly one Job.
	runOnce := func(th webauditv1alpha1.Thresholds) *batchv1.Job {
		createWA("* * * * *", th)
		reconcileOnce()
		jobs := listJobs()
		Expect(jobs).To(HaveLen(1))
		return &jobs[0]
	}

	expectAuditFailed := func() {
		c := condition(webauditv1alpha1.ConditionThresholdsMet)
		Expect(c).NotTo(BeNil())
		Expect(c.Status).To(Equal(metav1.ConditionUnknown))
		Expect(c.Reason).To(Equal(webauditv1alpha1.ReasonAuditFailed))
		Expect(getWA().Status.LastRun.Passed).To(BeNil())
		Expect(recorder.Events).To(Receive(HavePrefix("Warning AuditFailed")))
	}

	It("(a) creates exactly one Job for the due fire with name, annotation and ownerRef", func() {
		wa := createWA("* * * * *", webauditv1alpha1.Thresholds{})
		res := reconcileOnce()

		fire := now.UTC().Truncate(time.Minute)
		jobs := listJobs()
		Expect(jobs).To(HaveLen(1))
		job := jobs[0]
		Expect(job.Name).To(Equal(fmt.Sprintf("%s-%d", key.Name, fire.Unix())))
		Expect(job.Annotations).To(HaveKeyWithValue(audit.FireTimeAnnotation, fire.Format(time.RFC3339)))
		owner := metav1.GetControllerOf(&job)
		Expect(owner).NotTo(BeNil())
		Expect(owner.UID).To(Equal(wa.UID))

		Expect(res.RequeueAfter).To(Equal(fire.Add(time.Minute).Sub(now)))
		got := getWA()
		Expect(got.Status.NextRunTime.Time.Equal(fire.Add(time.Minute))).To(BeTrue())
		Expect(got.Status.ObservedGeneration).To(Equal(got.Generation))
		ready := condition(webauditv1alpha1.ConditionReady)
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(ready.Reason).To(Equal(webauditv1alpha1.ReasonScheduled))
	})

	It("(b) reconciling twice for the same time yields one Job", func() {
		createWA("* * * * *", webauditv1alpha1.Thresholds{})
		reconcileOnce()
		reconcileOnce()
		Expect(listJobs()).To(HaveLen(1))
	})

	It("(c) a breaching result sets ThresholdsMet=False/ThresholdsBreached", func() {
		lcp := metav1.Duration{Duration: 2500 * time.Millisecond}
		job := runOnce(webauditv1alpha1.Thresholds{LCP: &lcp})
		completeJob(job)
		terminatedPod(job, `{"lcpMs":3100,"tbtMs":100,"cls":0.05}`)
		reconcileOnce()

		c := condition(webauditv1alpha1.ConditionThresholdsMet)
		Expect(c.Status).To(Equal(metav1.ConditionFalse))
		Expect(c.Reason).To(Equal(webauditv1alpha1.ReasonThresholdsBreached))
		lr := getWA().Status.LastRun
		Expect(lr.JobName).To(Equal(job.Name))
		Expect(lr.Passed).To(HaveValue(BeFalse()))
		Expect(lr.LCP.Duration).To(Equal(3100 * time.Millisecond))
		Expect(lr.TBT.Duration).To(Equal(100 * time.Millisecond))
		Expect(lr.CLS.AsApproximateFloat64()).To(BeNumerically("~", 0.05, 1e-9))
		Expect(recorder.Events).To(Receive(HavePrefix("Warning ThresholdsBreached")))

		By("not re-recording the same Job")
		reconcileOnce()
		Expect(recorder.Events).NotTo(Receive())
	})

	It("(d) a passing result sets ThresholdsMet=True", func() {
		lcp := metav1.Duration{Duration: 2500 * time.Millisecond}
		job := runOnce(webauditv1alpha1.Thresholds{LCP: &lcp})
		completeJob(job)
		terminatedPod(job, `{"lcpMs":2000,"tbtMs":100,"cls":0.05}`)
		reconcileOnce()

		c := condition(webauditv1alpha1.ConditionThresholdsMet)
		Expect(c.Status).To(Equal(metav1.ConditionTrue))
		Expect(c.Reason).To(Equal(webauditv1alpha1.ReasonThresholdsMet))
		Expect(getWA().Status.LastRun.Passed).To(HaveValue(BeTrue()))
		Expect(recorder.Events).To(Receive(HavePrefix("Normal ThresholdsMet")))
	})

	It("(e) a failed Job without a Pod sets ThresholdsMet=Unknown/AuditFailed", func() {
		job := runOnce(webauditv1alpha1.Thresholds{})
		failJob(job)
		reconcileOnce()
		expectAuditFailed()
	})

	It("(f) a garbage termination message is an audit failure", func() {
		job := runOnce(webauditv1alpha1.Thresholds{})
		completeJob(job)
		terminatedPod(job, "garbage")
		reconcileOnce()
		expectAuditFailed()
	})

	It("(f) an empty termination message is an audit failure", func() {
		job := runOnce(webauditv1alpha1.Thresholds{})
		completeJob(job)
		terminatedPod(job, "")
		reconcileOnce()
		expectAuditFailed()
	})

	It("(g) a malformed schedule sets Ready=False/InvalidSchedule without a Job or requeue", func() {
		createWA("* * *", webauditv1alpha1.Thresholds{})
		res := reconcileOnce()
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(listJobs()).To(BeEmpty())
		ready := condition(webauditv1alpha1.ConditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(webauditv1alpha1.ReasonInvalidSchedule))
	})

	It("(g) a valid-but-never schedule creates no Job and does not panic", func() {
		createWA("0 0 31 2 *", webauditv1alpha1.Thresholds{})
		Expect(func() { reconcileOnce() }).NotTo(Panic())
		Expect(listJobs()).To(BeEmpty())
		Expect(condition(webauditv1alpha1.ConditionReady).Reason).To(Equal(webauditv1alpha1.ReasonInvalidSchedule))
	})

	It("runs once after a 70-day gap instead of reporting InvalidSchedule", func() {
		createWA("* * * * *", webauditv1alpha1.Thresholds{})
		now = now.Add(70 * 24 * time.Hour)
		reconcileOnce()
		ready := condition(webauditv1alpha1.ConditionReady)
		Expect(ready.Reason).To(Equal(webauditv1alpha1.ReasonScheduled))
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(listJobs()).To(HaveLen(1))
	})

	It("(h) creates no new Job while one is active", func() {
		runOnce(webauditv1alpha1.Thresholds{})
		now = now.Add(3 * time.Minute)
		reconcileOnce()
		Expect(listJobs()).To(HaveLen(1))
	})

	It("(i) prunes to the 3 newest finished Jobs", func() {
		wa := createWA("* * * * *", webauditv1alpha1.Thresholds{})
		base := wa.CreationTimestamp.UTC().Truncate(time.Minute)
		var want []string
		for i := 1; i <= 5; i++ {
			fire := base.Add(time.Duration(i) * time.Minute)
			job := audit.BuildJob(wa, fire, testImage)
			Expect(ctrl.SetControllerReference(wa, job, k8sClient.Scheme())).To(Succeed())
			Expect(k8sClient.Create(ctx, job)).To(Succeed())
			failJob(job)
			if i > 2 {
				want = append(want, job.Name)
			}
			now = fire.Add(30 * time.Second)
		}
		reconcileOnce()

		jobs := listJobs()
		names := make([]string, 0, len(jobs))
		for _, j := range jobs {
			names = append(names, j.Name)
		}
		Expect(names).To(ConsistOf(want))
	})

	It("(j) deleting the WebAudit removes its metric series", func() {
		job := runOnce(webauditv1alpha1.Thresholds{})
		failJob(job)
		reconcileOnce()
		Expect(seriesFor(key)).NotTo(BeZero())

		Expect(k8sClient.Delete(ctx, getWA())).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, key, &webauditv1alpha1.WebAudit{}))
		}).Should(BeTrue())
		reconcileOnce()
		Expect(seriesFor(key)).To(BeZero())
	})
})

// seriesFor counts webaudit_* series labelled with key in the controller-runtime registry.
func seriesFor(key types.NamespacedName) int {
	families, err := crmetrics.Registry.Gather()
	Expect(err).NotTo(HaveOccurred())
	n := 0
	for _, f := range families {
		if !strings.HasPrefix(f.GetName(), "webaudit_") {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["namespace"] == key.Namespace && labels["name"] == key.Name {
				n++
			}
		}
	}
	return n
}
