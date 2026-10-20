package controller

import (
	"context"
	"sort"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	webauditv1alpha1 "github.com/marcelblanarik/webaudit-operator/api/v1alpha1"
	"github.com/marcelblanarik/webaudit-operator/internal/audit"
	"github.com/marcelblanarik/webaudit-operator/internal/metrics"
)

// keepFinishedJobs is how many finished Jobs survive pruning.
const keepFinishedJobs = 3

// WebAuditReconciler reconciles a WebAudit object
type WebAuditReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Recorder     record.EventRecorder
	DefaultImage string
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// +kubebuilder:rbac:groups=webaudit.simpleit.sk,resources=webaudits,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=webaudit.simpleit.sk,resources=webaudits/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=webaudit.simpleit.sk,resources=webaudits/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile records finished audit Jobs, prunes old ones and starts the next Job when one is due.
func (r *WebAuditReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	clock := r.Now
	if clock == nil {
		clock = time.Now
	}
	now := clock()

	wa := &webauditv1alpha1.WebAudit{}
	if err := r.Get(ctx, req.NamespacedName, wa); err != nil {
		if apierrors.IsNotFound(err) {
			metrics.Forget(req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	jobs, err := r.ownedJobs(ctx, wa)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Results are persisted before anything else so a later failure cannot lose them.
	runs, err := r.recordFinished(ctx, wa, jobs)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(runs) > 0 {
		if stop, res, err := r.updateStatus(ctx, wa); stop {
			return res, err
		}
		for _, run := range runs {
			r.Recorder.Event(wa, run.eventType, run.reason, run.message)
			metrics.ObserveRun(req.NamespacedName, run.outcome, run.result, now)
		}
	}

	r.pruneJobs(ctx, jobs)

	next, err := audit.NextFire(wa.Spec.Schedule, now)
	var fire time.Time
	var due bool
	if err == nil {
		since := wa.CreationTimestamp.Time
		if len(jobs) > 0 {
			since = fireTime(&jobs[0])
		}
		fire, due, err = audit.MostRecentFire(wa.Spec.Schedule, since, now)
	}
	if err != nil {
		// A spec edit triggers a new reconcile; retrying an invalid schedule would not help.
		wa.Status.ObservedGeneration = wa.Generation
		wa.Status.NextRunTime = nil
		r.setCondition(wa, webauditv1alpha1.ConditionReady, metav1.ConditionFalse, webauditv1alpha1.ReasonInvalidSchedule, err.Error())
		_, res, err := r.updateStatus(ctx, wa)
		return res, err
	}

	if due && !anyActive(jobs) {
		job := audit.BuildJob(wa, fire, r.DefaultImage)
		if err := ctrl.SetControllerReference(wa, job, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		// The name is derived from the fire time, so a repeated create for the same fire is a no-op.
		if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		log.Info("started audit job", "job", job.Name, "fire", fire)
	}

	wa.Status.ObservedGeneration = wa.Generation
	wa.Status.NextRunTime = &metav1.Time{Time: next}
	r.setCondition(wa, webauditv1alpha1.ConditionReady, metav1.ConditionTrue, webauditv1alpha1.ReasonScheduled, "next run at "+next.Format(time.RFC3339))
	if stop, res, err := r.updateStatus(ctx, wa); stop {
		return res, err
	}
	return ctrl.Result{RequeueAfter: max(next.Sub(now), time.Second)}, nil
}

// updateStatus writes wa.Status. stop is true when the caller must return res, err:
// a conflict requeues so the next run re-reads the object, a deleted object needs nothing.
func (r *WebAuditReconciler) updateStatus(ctx context.Context, wa *webauditv1alpha1.WebAudit) (stop bool, res ctrl.Result, err error) {
	err = r.Status().Update(ctx, wa)
	switch {
	case err == nil:
		return false, ctrl.Result{}, nil
	case apierrors.IsConflict(err):
		return true, ctrl.Result{RequeueAfter: time.Second}, nil
	case apierrors.IsNotFound(err):
		return true, ctrl.Result{}, nil
	default:
		return true, ctrl.Result{}, err
	}
}

func (r *WebAuditReconciler) setCondition(wa *webauditv1alpha1.WebAudit, t string, s metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&wa.Status.Conditions, metav1.Condition{
		Type: t, Status: s, Reason: reason, Message: msg, ObservedGeneration: wa.Generation,
	})
}

// ownedJobs lists the Jobs controlled by wa, newest fire first.
func (r *WebAuditReconciler) ownedJobs(ctx context.Context, wa *webauditv1alpha1.WebAudit) ([]batchv1.Job, error) {
	var list batchv1.JobList
	if err := r.List(ctx, &list, client.InNamespace(wa.Namespace),
		client.MatchingLabels{audit.WebAuditLabel: wa.Name}); err != nil {
		return nil, err
	}
	var jobs []batchv1.Job
	for _, j := range list.Items {
		// The label alone is not ownership: a recreated WebAudit has a new UID.
		if owner := metav1.GetControllerOf(&j); owner != nil && owner.UID == wa.UID {
			jobs = append(jobs, j)
		}
	}
	sort.Slice(jobs, func(a, b int) bool { return fireTime(&jobs[a]).After(fireTime(&jobs[b])) })
	return jobs, nil
}

// pruneJobs deletes finished Jobs beyond the newest keepFinishedJobs. jobs must be sorted newest first.
func (r *WebAuditReconciler) pruneJobs(ctx context.Context, jobs []batchv1.Job) {
	kept := 0
	for i := range jobs {
		if _, finished := jobFinished(&jobs[i]); !finished {
			continue
		}
		if kept++; kept <= keepFinishedJobs {
			continue
		}
		// Background propagation also removes the Job's Pods.
		err := r.Delete(ctx, &jobs[i], client.PropagationPolicy(metav1.DeletePropagationBackground))
		if client.IgnoreNotFound(err) != nil {
			// Best effort: the next reconcile retries.
			logf.FromContext(ctx).Error(err, "failed to prune job", "job", jobs[i].Name)
		}
	}
}

func anyActive(jobs []batchv1.Job) bool {
	for i := range jobs {
		if _, finished := jobFinished(&jobs[i]); !finished {
			return true
		}
	}
	return false
}

// jobFinished reports whether the Job is Complete or Failed, and failed is true for Failed.
func jobFinished(job *batchv1.Job) (failed, finished bool) {
	for _, c := range job.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete:
			return false, true
		case batchv1.JobFailed:
			return true, true
		}
	}
	return false, false
}

// fireTime is the scheduled fire time recorded on the Job, falling back to its creation time.
func fireTime(job *batchv1.Job) time.Time {
	if t, err := time.Parse(time.RFC3339, job.Annotations[audit.FireTimeAnnotation]); err == nil {
		return t
	}
	return job.CreationTimestamp.Time
}

// SetupWithManager sets up the controller with the Manager.
func (r *WebAuditReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&webauditv1alpha1.WebAudit{}).
		Owns(&batchv1.Job{}).
		Named("webaudit").
		Complete(r)
}
