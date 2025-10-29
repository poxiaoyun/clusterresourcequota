package clusterresourcequota

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	quotav1 "xiaoshiai.cn/clusterresourcequota/apis/quota/v1"
	"xiaoshiai.cn/common"
	"xiaoshiai.cn/common/log"
)

const (
	LabelClusterResourceQuota     = "clusterresourcequota." + common.GroupPrefix
	ClusterResourceQuotaFinalizer = "clusterresourcequota.finalizers." + common.GroupPrefix
)

func NewClusterResourceQuotaReconciler(client client.Client) *ClusterResourceQuotaReconciler {
	return &ClusterResourceQuotaReconciler{Client: client}
}

// ClusterResourceQuotaReconciler is a simple ControllerManagedBy example implementation.
type ClusterResourceQuotaReconciler struct {
	Client client.Client
}

func (a *ClusterResourceQuotaReconciler) Setup(mgr manager.Manager) error {
	return builder.ControllerManagedBy(mgr).
		For(&quotav1.ClusterResourceQuota{}, builder.WithPredicates(OnClusterResourceQuotaSpecChange())).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(a.OnNamespaceChange)).
		Complete(a)
}

func OnClusterResourceQuotaSpecChange() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj := e.ObjectOld.(*quotav1.ClusterResourceQuota)
			newObj := e.ObjectNew.(*quotav1.ClusterResourceQuota)
			return !equality.Semantic.DeepEqual(oldObj.Spec, newObj.Spec)
		},
	}
}

func (a *ClusterResourceQuotaReconciler) OnNamespaceChange(ctx context.Context, obj client.Object) []reconcile.Request {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return []reconcile.Request{}
	}
	clusterresourcequotas := &quotav1.ClusterResourceQuotaList{}
	if err := a.Client.List(ctx, clusterresourcequotas); err != nil {
		return []reconcile.Request{}
	}
	requests := []reconcile.Request{}
	for _, clusterresourcequota := range clusterresourcequotas.Items {
		sel, err := metav1.LabelSelectorAsSelector(clusterresourcequota.Spec.NamespaceSelector)
		if err != nil {
			continue
		}
		if sel.Matches(labels.Set(ns.Labels)) {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&clusterresourcequota),
			})
		}
	}
	return requests
}

func (a *ClusterResourceQuotaReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	clusterresourcequota := &quotav1.ClusterResourceQuota{}
	if err := a.Client.Get(ctx, req.NamespacedName, clusterresourcequota); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}
	if clusterresourcequota.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}
	if err := a.syncResourceQuota(ctx, clusterresourcequota); err != nil {
		return reconcile.Result{}, err
	}
	// update status
	if err := a.Client.Status().Update(ctx, clusterresourcequota); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (rq *ClusterResourceQuotaReconciler) syncResourceQuota(ctx context.Context, clusterResourceQuota *quotav1.ClusterResourceQuota) (err error) {
	log := log.FromContext(ctx)

	matchedNamespaces, err := rq.selectedNamespaces(ctx, clusterResourceQuota)
	if err != nil {
		return err
	}

	totalUsage := corev1.ResourceList{}
	namespaceUsage := []quotav1.NamespaceResourceQuota{}

	var errs []error
	for _, ns := range matchedNamespaces {
		// create or update resource quota in the namespace
		resourceQuota := &quotav1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetNamespaceResourceQuotaName(clusterResourceQuota.Name),
				Namespace: ns.Name,
			},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, rq.Client, resourceQuota, func() error {
			if resourceQuota.Labels == nil {
				resourceQuota.Labels = map[string]string{}
			}
			resourceQuota.Labels[LabelClusterResourceQuota] = clusterResourceQuota.Name
			// set resource quota spec to cluster resource quota spec
			// single namespace resource quota should not larger than cluster resource quota
			resourceQuota.Spec = clusterResourceQuota.Spec.ResourceQuotaSpec
			// set owner reference to cluster resource quota so that resource quota will be deleted when cluster resource quota is deleted
			return controllerutil.SetOwnerReference(clusterResourceQuota, resourceQuota, rq.Client.Scheme())
		})
		if err != nil {
			log.Error(err, "failed to create or update resource quota", "namespace", ns)
			errs = append(errs, err)
			continue
		}
		totalUsage = quota.Add(totalUsage, resourceQuota.Status.Used)
		namespaceUsage = append(namespaceUsage, quotav1.NamespaceResourceQuota{Name: ns.Name, Used: resourceQuota.Status.Used})
	}
	clusterResourceQuota.Status.Namespaces = namespaceUsage
	clusterResourceQuota.Status.Hard = clusterResourceQuota.Spec.Hard.DeepCopy()
	clusterResourceQuota.Status.Used = totalUsage.DeepCopy()
	return utilerrors.NewAggregate(errs)
}

func (rq *ClusterResourceQuotaReconciler) selectedNamespaces(ctx context.Context, clusterResourceQuota *quotav1.ClusterResourceQuota) ([]corev1.Namespace, error) {
	namespacelist := &corev1.NamespaceList{}
	if err := rq.Client.List(ctx, namespacelist); err != nil {
		return nil, err
	}
	selector, err := metav1.LabelSelectorAsSelector(clusterResourceQuota.Spec.NamespaceSelector)
	if err != nil {
		return nil, err
	}
	matchedNamespaces := []corev1.Namespace{}
	for _, ns := range namespacelist.Items {
		if selector.Matches(labels.Set(ns.Labels)) {
			matchedNamespaces = append(matchedNamespaces, ns)
		}
	}
	return matchedNamespaces, nil
}

func GetNamespaceResourceQuotaName(name string) string {
	return "clusterresourcequota." + name
}
