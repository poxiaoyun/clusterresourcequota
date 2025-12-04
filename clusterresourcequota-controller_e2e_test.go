package clusterresourcequota

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	quotav1 "xiaoshiai.cn/clusterresourcequota/apis/quota/v1"
)

var _ = Describe("ClusterResourceQuota", func() {
	It("creates a namespaced ResourceQuota for matching namespaces", func() {
		crqName := "test-crq"
		nsName := "test-ns"
		labelKey := "test-env-create"
		labelValue := "e2e"

		// Create ClusterResourceQuota
		crq := &quotav1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: crqName,
			},
			Spec: quotav1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						labelKey: labelValue,
					},
				},
				ResourceQuotaSpec: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("1"),
					},
				},
			},
		}

		Expect(k8sClient.Create(ctx, crq)).To(Succeed())

		// Create Namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,
				Labels: map[string]string{
					labelKey: labelValue,
				},
			},
		}

		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Wait for ResourceQuota to be created in the namespace
		rqName := types.NamespacedName{Name: crqName, Namespace: nsName}
		rq := &quotav1.ResourceQuota{}

		err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			err := k8sClient.Get(ctx, rqName, rq)
			if err != nil {
				return false, nil
			}
			return true, nil
		})

		Expect(err).NotTo(HaveOccurred())

		// Verify ResourceQuota spec
		cpuLimit := rq.Spec.Hard[corev1.ResourceCPU]
		Expect(cpuLimit.String()).To(Equal("1"))

		// Cleanup
		_ = k8sClient.Delete(context.Background(), ns)
		_ = k8sClient.Delete(context.Background(), crq)
		_ = wait.PollUntilContextTimeout(context.Background(), 100*time.Millisecond, 5*time.Second, true, func(ctx context.Context) (bool, error) {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &corev1.Namespace{})
			if err != nil {
				return true, nil
			}
			return false, nil
		})
	})

	It("does not create ResourceQuota for non-matching namespace", func() {
		crqName := "test-crq-nomatch"
		nsName := "test-ns-nomatch"
		labelKey := "test-env-nomatch"
		// labelValue intentionally different
		crqLabelValue := "e2e"
		nsLabelValue := "other"

		crq := &quotav1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: crqName},
			Spec: quotav1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{labelKey: crqLabelValue}},
				ResourceQuotaSpec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName, Labels: map[string]string{labelKey: nsLabelValue}}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Ensure ResourceQuota is NOT created
		rqName := types.NamespacedName{Name: crqName, Namespace: nsName}
		rq := &quotav1.ResourceQuota{}
		err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 3*time.Second, true, func(ctx context.Context) (bool, error) {
			err := k8sClient.Get(ctx, rqName, rq)
			if err != nil {
				// continue waiting until timeout, but we expect NotFound so return false,nil
				return false, nil
			}
			// if found, that's a failure
			return true, nil
		})

		// If err == nil means the above returned true (found) which is failure; we expect timeout error
		Expect(err).To(HaveOccurred())

		// Cleanup
		_ = k8sClient.Delete(context.Background(), ns)
		_ = k8sClient.Delete(context.Background(), crq)
	})

	It("updates namespaced ResourceQuota when ClusterResourceQuota changes and deletes on CRQ removal", func() {
		crqName := "test-crq-update"
		nsName := "test-ns-update"
		labelKey := "test-env-update"
		labelValue := "e2e"

		crq := &quotav1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: crqName},
			Spec: quotav1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{labelKey: labelValue}},
				ResourceQuotaSpec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName, Labels: map[string]string{labelKey: labelValue}}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		rqName := types.NamespacedName{Name: crqName, Namespace: nsName}
		rq := &quotav1.ResourceQuota{}
		Expect(wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			err := k8sClient.Get(ctx, rqName, rq)
			if err != nil {
				return false, nil
			}
			return true, nil
		})).To(Succeed())

		// Update CRQ to set CPU to 2
		fetched := &quotav1.ClusterResourceQuota{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crqName}, fetched)).To(Succeed())
		fetched.Spec.ResourceQuotaSpec.Hard[corev1.ResourceCPU] = resource.MustParse("2")
		Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

		// Wait until namespaced ResourceQuota reflects update
		Expect(wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			err := k8sClient.Get(ctx, rqName, rq)
			if err != nil {
				return false, nil
			}
			cpuLimit := rq.Spec.Hard[corev1.ResourceCPU]
			return cpuLimit.String() == "2", nil
		})).To(Succeed())

		// Now delete ClusterResourceQuota and expect namespaced ResourceQuota to be removed
		Expect(k8sClient.Delete(ctx, fetched)).To(Succeed())
		// Wait for deletion
		Expect(wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			err := k8sClient.Get(ctx, rqName, rq)
			if err != nil {
				// Not found -> success
				return true, nil
			}
			return false, nil
		})).To(Succeed())

		// Cleanup namespace
		_ = k8sClient.Delete(context.Background(), ns)
		_ = wait.PollUntilContextTimeout(context.Background(), 100*time.Millisecond, 5*time.Second, true, func(ctx context.Context) (bool, error) {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &corev1.Namespace{})
			if err != nil {
				return true, nil
			}
			return false, nil
		})
	})
})
