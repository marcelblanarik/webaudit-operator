package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	webauditv1alpha1 "github.com/marcelblanarik/webaudit-operator/api/v1alpha1"
)

// WebAuditReconciler reconciles a WebAudit object
type WebAuditReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=webaudit.simpleit.sk,resources=webaudits,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=webaudit.simpleit.sk,resources=webaudits/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=webaudit.simpleit.sk,resources=webaudits/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the WebAudit object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.25.0/pkg/reconcile
func (r *WebAuditReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WebAuditReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&webauditv1alpha1.WebAudit{}).
		Named("webaudit").
		Complete(r)
}
