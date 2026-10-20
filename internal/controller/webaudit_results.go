package controller

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	webauditv1alpha1 "github.com/marcelblanarik/webaudit-operator/api/v1alpha1"
	"github.com/marcelblanarik/webaudit-operator/internal/audit"
	"github.com/marcelblanarik/webaudit-operator/internal/metrics"
)

// run is one newly recorded Job result, emitted as Event and metrics once the status is persisted.
type run struct {
	eventType, reason, message string
	outcome                    metrics.Outcome
	result                     *audit.Result
}

// recordFinished writes every finished Job newer than status.lastRun into wa.Status, oldest first.
// jobs must be sorted newest fire first.
func (r *WebAuditReconciler) recordFinished(ctx context.Context, wa *webauditv1alpha1.WebAudit, jobs []batchv1.Job) ([]run, error) {
	recorded := int64(-1)
	if lr := wa.Status.LastRun; lr != nil {
		// Job names end in the fire unix time, so this works after the recorded Job is pruned.
		if i := strings.LastIndex(lr.JobName, "-"); i >= 0 {
			if v, err := strconv.ParseInt(lr.JobName[i+1:], 10, 64); err == nil {
				recorded = v
			}
		}
	}
	var runs []run
	for i := len(jobs) - 1; i >= 0; i-- {
		job := &jobs[i]
		failed, finished := jobFinished(job)
		fire := fireTime(job).Unix()
		if !finished || fire <= recorded || (wa.Status.LastRun != nil && job.Name == wa.Status.LastRun.JobName) {
			continue
		}
		rn, err := r.recordResult(ctx, wa, job, failed)
		if err != nil {
			return nil, err
		}
		runs = append(runs, rn)
		recorded = fire
	}
	return runs, nil
}

// recordResult sets lastRun and ThresholdsMet from one finished Job.
func (r *WebAuditReconciler) recordResult(ctx context.Context, wa *webauditv1alpha1.WebAudit, job *batchv1.Job, failed bool) (run, error) {
	lr := &webauditv1alpha1.LastRun{
		JobName:        job.Name,
		StartTime:      job.Status.StartTime,
		CompletionTime: job.Status.CompletionTime,
	}
	wa.Status.LastRun = lr

	auditFailed := func(msg string) run {
		lr.Message = msg
		r.setCondition(wa, webauditv1alpha1.ConditionThresholdsMet, metav1.ConditionUnknown, webauditv1alpha1.ReasonAuditFailed, msg)
		return run{corev1.EventTypeWarning, webauditv1alpha1.ReasonAuditFailed, msg, metrics.OutcomeFailed, nil}
	}

	if failed {
		return auditFailed(fmt.Sprintf("audit job %s failed: %s", job.Name, failReason(job))), nil
	}
	msg, ok, err := r.readTerminationMessage(ctx, job)
	if err != nil {
		return run{}, err
	}
	if !ok {
		return auditFailed(fmt.Sprintf("audit job %s has no terminated %q container", job.Name, audit.AuditContainer)), nil
	}
	res, err := audit.ParseResult([]byte(msg))
	if err != nil {
		return auditFailed(fmt.Sprintf("audit job %s: %v", job.Name, err)), nil
	}

	lr.LCP = &metav1.Duration{Duration: time.Duration(res.LCPMs * float64(time.Millisecond))}
	lr.TBT = &metav1.Duration{Duration: time.Duration(res.TBTMs * float64(time.Millisecond))}
	lr.CLS = resource.NewScaledQuantity(int64(math.Round(res.CLS*1e9)), resource.Nano)

	if breaches := audit.Breaches(wa.Spec.Thresholds, res); len(breaches) > 0 {
		lr.Passed = new(false)
		lr.Message = strings.Join(breaches, "; ")
		r.setCondition(wa, webauditv1alpha1.ConditionThresholdsMet, metav1.ConditionFalse, webauditv1alpha1.ReasonThresholdsBreached, lr.Message)
		return run{corev1.EventTypeWarning, webauditv1alpha1.ReasonThresholdsBreached, lr.Message, metrics.OutcomeBreach, &res}, nil
	}
	lr.Passed = new(true)
	lr.Message = "all thresholds met"
	r.setCondition(wa, webauditv1alpha1.ConditionThresholdsMet, metav1.ConditionTrue, webauditv1alpha1.ReasonThresholdsMet, lr.Message)
	return run{corev1.EventTypeNormal, webauditv1alpha1.ReasonThresholdsMet, lr.Message, metrics.OutcomeSuccess, &res}, nil
}

// readTerminationMessage returns the termination message of the audit container of the Job's Pod.
// A cache miss is retried uncached: the Job's Complete event can arrive before the Pod's terminated status.
func (r *WebAuditReconciler) readTerminationMessage(ctx context.Context, job *batchv1.Job) (string, bool, error) {
	readers := []client.Reader{r.Client}
	if r.APIReader != nil {
		readers = append(readers, r.APIReader)
	}
	for _, rd := range readers {
		var pods corev1.PodList
		if err := rd.List(ctx, &pods, client.InNamespace(job.Namespace),
			client.MatchingLabels{"job-name": job.Name}); err != nil {
			return "", false, err
		}
		for _, p := range pods.Items {
			for _, cs := range p.Status.ContainerStatuses {
				if cs.Name == audit.AuditContainer && cs.State.Terminated != nil {
					return cs.State.Terminated.Message, true, nil
				}
			}
		}
	}
	return "", false, nil
}

func failReason(job *batchv1.Job) string {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return strings.TrimSpace(c.Reason + " " + c.Message)
		}
	}
	return "unknown reason"
}
