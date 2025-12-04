package clusterresourcequota

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	quotav1 "xiaoshiai.cn/clusterresourcequota/apis/quota/v1"
)

func TestClusterResourceQuotaReconciler_Reconcile(t *testing.T) {
	// Register schemes
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = quotav1.AddToScheme(scheme)

	// Define test data
	crqName := "test-crq"
	matchLabel := map[string]string{"env": "prod"}

	crq := &quotav1.ClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name: crqName,
		},
		Spec: quotav1.ClusterResourceQuotaSpec{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: matchLabel,
			},
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("10"),
				},
			},
		},
	}

	nsProd := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "prod-ns",
			Labels: matchLabel,
		},
	}

	nsDev := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "dev-ns",
			Labels: map[string]string{"env": "dev"},
		},
	}

	// Create fake client
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(crq, nsProd, nsDev).
		WithStatusSubresource(crq).
		Build()

	// Create reconciler
	r := &ClusterResourceQuotaReconciler{
		Client: client,
	}

	// Create request
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: crqName,
		},
	}

	// Run Reconcile
	ctx := context.Background()
	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Check if ResourceQuota is created in prod-ns
	rq := &quotav1.ResourceQuota{}
	err = client.Get(ctx, types.NamespacedName{Name: crqName, Namespace: nsProd.Name}, rq)
	if err != nil {
		t.Errorf("Failed to get ResourceQuota in prod-ns: %v", err)
	}

	// Check ResourceQuota spec
	cpuLimit := rq.Spec.Hard[corev1.ResourceCPU]
	if cpuLimit.String() != "10" {
		t.Errorf("Expected CPU limit 10, got %v", cpuLimit)
	}

	// Check owner reference
	if len(rq.OwnerReferences) == 0 {
		t.Error("Expected OwnerReference to be set")
	} else {
		if rq.OwnerReferences[0].Name != crqName {
			t.Errorf("Expected OwnerReference name %s, got %s", crqName, rq.OwnerReferences[0].Name)
		}
	}

	// Check if ResourceQuota is NOT created in dev-ns
	err = client.Get(ctx, types.NamespacedName{Name: crqName, Namespace: nsDev.Name}, rq)
	if err == nil {
		t.Error("ResourceQuota should not exist in dev-ns")
	}

	// Check ClusterResourceQuota status
	updatedCrq := &quotav1.ClusterResourceQuota{}
	err = client.Get(ctx, types.NamespacedName{Name: crqName}, updatedCrq)
	if err != nil {
		t.Fatalf("Failed to get updated ClusterResourceQuota: %v", err)
	}

	if len(updatedCrq.Status.Namespaces) != 1 {
		t.Errorf("Expected 1 namespace in status, got %d", len(updatedCrq.Status.Namespaces))
	} else {
		if updatedCrq.Status.Namespaces[0].Name != nsProd.Name {
			t.Errorf("Expected namespace %s in status, got %s", nsProd.Name, updatedCrq.Status.Namespaces[0].Name)
		}
	}
}
