package clusterresourcequota_test

import (
	"context"
	"encoding/json"
	"testing"

	admv1 "k8s.io/api/admission/v1"
	authnv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"xiaoshiai.cn/clusterresourcequota"
	thisquotav1 "xiaoshiai.cn/clusterresourcequota/apis/quota/v1"
)

func TestResourceQuotaStatusAdmission_Handle(t *testing.T) {
	ctx := context.Background()

	// prepare scheme
	scheme := clusterresourcequota.GetScheme()

	objects := []runtime.Object{
		&thisquotav1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq"},
			Status: thisquotav1.ClusterResourceQuotaStatus{
				ResourceQuotaStatus: corev1.ResourceQuotaStatus{
					Hard: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("2"),
					},
					Used: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("0"),
					},
				},
			},
		},
		&thisquotav1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "clusterresourcequota.crq",
				Namespace: "ns1",
				Labels:    map[string]string{clusterresourcequota.LabelClusterResourceQuota: "crq"},
			},
			Status: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1"),
				},
			},
		},
		&thisquotav1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "clusterresourcequota.crq",
				Namespace: "ns2",
				Labels:    map[string]string{clusterresourcequota.LabelClusterResourceQuota: "crq"},
			},
			Status: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1"),
				},
			},
		},
	}

	// fake client with the ClusterResourceQuota present
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()

	cache := clusterresourcequota.NewResourceQuotaCache()
	// initialize the cache
	quotaslist := &thisquotav1.ResourceQuotaList{}
	if err := client.List(ctx, quotaslist); err != nil {
		t.Fatalf("failed to list ResourceQuotas: %v", err)
	}
	cache.Sync(quotaslist.Items)

	handler := clusterresourcequota.NewResourceQuotaStatusAdmission(cache, client)

	t.Run("not-from-admission-controller-allowed", func(t *testing.T) {
		rq := &thisquotav1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "otherrq", Namespace: "ns"},
		}
		req := admission.Request{AdmissionRequest: admv1.AdmissionRequest{Object: toRawExtension(rq), UserInfo: authnv1.UserInfo{Username: "someone"}}}
		resp := handler.Handle(ctx, req)
		// Expect allowed because the request is not from resourcequota admission controller
		if !resp.AdmissionResponse.Allowed {
			t.Fatalf("expected allowed response, got: %+v", resp)
		}
	})

	t.Run("from-admission-controller-over-quota-forbidden", func(t *testing.T) {
		// prepare a ResourceQuota whose status used cpu=3 (exceeds crq hard=2)
		rq := &thisquotav1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "clusterresourcequota.crq",
				Namespace: "ns1",
				Labels:    map[string]string{clusterresourcequota.LabelClusterResourceQuota: "crq"},
			},
			Spec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2"),
				},
			},
			Status: corev1.ResourceQuotaStatus{
				Used: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2"),
				},
			},
		}
		// UserInfo nil or username=ResourceQuotaAdmissionControllerUsername is treated as admission controller
		req := admission.Request{AdmissionRequest: admv1.AdmissionRequest{Object: toRawExtension(rq), UserInfo: authnv1.UserInfo{Username: "system:apiserver"}}}
		resp := handler.Handle(ctx, req)
		// Expect forbidden (errored) because total used (3) > crq hard (2)
		if resp.AdmissionResponse.Allowed {
			t.Fatalf("expected forbidden response, got allowed: %+v", resp)
		}
		// status code should be 403
		if resp.AdmissionResponse.Result == nil || resp.AdmissionResponse.Result.Code != 403 {
			t.Fatalf("expected status code 403, got: %+v", resp.AdmissionResponse.Result)
		}
	})
}

func toRawExtension(obj runtime.Object) runtime.RawExtension {
	raw, _ := json.Marshal(obj)
	return runtime.RawExtension{Raw: raw}
}
