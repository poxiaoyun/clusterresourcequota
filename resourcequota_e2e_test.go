package clusterresourcequota

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	thisquotav1 "xiaoshiai.cn/clusterresourcequota/apis/quota/v1"
)

var _ = Describe("ResourceQuota controller e2e", func() {
	It("updates ResourceQuota status when a Service is created", func() {
		ctx := context.Background()
		name := fmt.Sprintf("rq-e2e-%d", time.Now().UnixNano())
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		rq := &thisquotav1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "rq-test", Namespace: name},
			Spec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceServices: resource.MustParse("10"),
				},
			},
		}
		Expect(k8sClient.Create(ctx, rq)).To(Succeed())

		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-test", Namespace: name},
			Spec: corev1.ServiceSpec{
				Ports:     []corev1.ServicePort{{Port: 80}},
				ClusterIP: "",
			},
		}
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())

		// wait for ResourceQuota status to be updated for services
		Eventually(func() bool {
			var got thisquotav1.ResourceQuota
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: name, Name: "rq-test"}, &got)
			if err != nil {
				return false
			}
			if q, ok := got.Status.Used[corev1.ResourceServices]; ok {
				return q.Cmp(resource.MustParse("1")) >= 0
			}
			return false
		}, 20*time.Second, 500*time.Millisecond).Should(BeTrue())

		// cleanup
		Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
		Expect(k8sClient.Delete(ctx, rq)).To(Succeed())
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	It("counts Pod cpu requests correctly", func() {
		ctx := context.Background()
		name := fmt.Sprintf("rq-pod-%d", time.Now().UnixNano())
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		rq := &thisquotav1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "rq-pod", Namespace: name},
			Spec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{corev1.ResourceName("requests.cpu"): resource.MustParse("2")},
			},
		}
		Expect(k8sClient.Create(ctx, rq)).To(Succeed())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-cpu", Namespace: name},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "c",
					Image: "busybox",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
					},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())

		Eventually(func() bool {
			var got thisquotav1.ResourceQuota
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: name, Name: "rq-pod"}, &got)
			if err != nil {
				return false
			}
			if q, ok := got.Status.Used[corev1.ResourceName("requests.cpu")]; ok {
				return q.Cmp(resource.MustParse("500m")) >= 0
			}
			return false
		}, 20*time.Second, 500*time.Millisecond).Should(BeTrue())

		// cleanup
		Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		Expect(k8sClient.Delete(ctx, rq)).To(Succeed())
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	It("decrements Pod requests when a Pod is deleted", func() {
		ctx := context.Background()
		name := fmt.Sprintf("rq-pod-del-%d", time.Now().UnixNano())
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		rq := &thisquotav1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "rq-pod-del", Namespace: name},
			Spec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{corev1.ResourceName("requests.cpu"): resource.MustParse("2")},
			},
		}
		Expect(k8sClient.Create(ctx, rq)).To(Succeed())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-del", Namespace: name},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "c",
					Image: "busybox",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
					},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())

		// wait used >= 1
		Eventually(func() bool {
			var got thisquotav1.ResourceQuota
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: name, Name: "rq-pod-del"}, &got)
			if err != nil {
				return false
			}
			if q, ok := got.Status.Used[corev1.ResourceName("requests.cpu")]; ok {
				return q.Cmp(resource.MustParse("1")) >= 0
			}
			return false
		}, 20*time.Second, 500*time.Millisecond).Should(BeTrue())

		// delete pod and expect used == 0
		Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		Eventually(func() bool {
			var got thisquotav1.ResourceQuota
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: name, Name: "rq-pod-del"}, &got)
			if err != nil {
				return false
			}
			if q, ok := got.Status.Used[corev1.ResourceName("requests.cpu")]; ok {
				return q.Cmp(resource.MustParse("0")) == 0
			}
			return false
		}, 20*time.Second, 500*time.Millisecond).Should(BeTrue())

		// cleanup
		Expect(k8sClient.Delete(ctx, rq)).To(Succeed())
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	It("decrements ResourceQuota when a Service is deleted", func() {
		ctx := context.Background()
		name := fmt.Sprintf("rq-del-%d", time.Now().UnixNano())
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		rq := &thisquotav1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "rq-del", Namespace: name},
			Spec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{corev1.ResourceServices: resource.MustParse("10")},
			},
		}
		Expect(k8sClient.Create(ctx, rq)).To(Succeed())

		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-del", Namespace: name}, Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}, ClusterIP: ""}}
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())

		// wait used >=1
		Eventually(func() bool {
			var got thisquotav1.ResourceQuota
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: name, Name: "rq-del"}, &got)
			if err != nil {
				return false
			}
			if q, ok := got.Status.Used[corev1.ResourceServices]; ok {
				return q.Cmp(resource.MustParse("1")) >= 0
			}
			return false
		}, 20*time.Second, 500*time.Millisecond).Should(BeTrue())

		// delete service and expect used == 0
		Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
		Eventually(func() bool {
			var got thisquotav1.ResourceQuota
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: name, Name: "rq-del"}, &got)
			if err != nil {
				return false
			}
			if q, ok := got.Status.Used[corev1.ResourceServices]; ok {
				return q.Cmp(resource.MustParse("0")) == 0
			}
			return false
		}, 20*time.Second, 500*time.Millisecond).Should(BeTrue())

		// cleanup
		Expect(k8sClient.Delete(ctx, rq)).To(Succeed())
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	It("counts multiple Services correctly", func() {
		ctx := context.Background()
		name := fmt.Sprintf("rq-multi-%d", time.Now().UnixNano())
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		rq := &thisquotav1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "rq-multi", Namespace: name},
			Spec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{corev1.ResourceServices: resource.MustParse("10")},
			},
		}
		Expect(k8sClient.Create(ctx, rq)).To(Succeed())

		services := make([]*corev1.Service, 0, 3)
		for i := 0; i < 3; i++ {
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("svc-%d", i), Namespace: name}, Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}, ClusterIP: ""}}
			Expect(k8sClient.Create(ctx, svc)).To(Succeed())
			services = append(services, svc)
		}

		Eventually(func() bool {
			var got thisquotav1.ResourceQuota
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: name, Name: "rq-multi"}, &got)
			if err != nil {
				return false
			}
			if q, ok := got.Status.Used[corev1.ResourceServices]; ok {
				return q.Cmp(resource.MustParse("3")) >= 0
			}
			return false
		}, 20*time.Second, 500*time.Millisecond).Should(BeTrue())

		// cleanup
		for _, s := range services {
			Expect(k8sClient.Delete(ctx, s)).To(Succeed())
		}
		Expect(k8sClient.Delete(ctx, rq)).To(Succeed())
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})
})
