package clusterresourcequota_test

import (
	"context"
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/client-go/kubernetes/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	api "k8s.io/kubernetes/pkg/apis/core"
	"xiaoshiai.cn/clusterresourcequota"
	thisquotav1 "xiaoshiai.cn/clusterresourcequota/apis/quota/v1"
	thisfake "xiaoshiai.cn/clusterresourcequota/generated/clientset/versioned/fake"
)

func TestAdmitLimitedResourceWithQuota(t *testing.T) {
	resourceQuota := &thisquotav1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "test", ResourceVersion: "124"},
		Spec: corev1.ResourceQuotaSpec{
			Scopes: []corev1.ResourceQuotaScope{
				clusterresourcequota.ResourceQuotaScopeNodeSelector,
			},
			ScopeSelector: &corev1.ScopeSelector{
				MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
					{
						ScopeName: clusterresourcequota.ResourceQuotaScopeNodeSelector,
						Operator:  corev1.ScopeSelectorOpIn,
						Values: []string{
							labels.SelectorFromSet(labels.Set{"nvidia.com/gpu.product": "A100"}).String(),
						},
					},
				},
			},
			Hard: corev1.ResourceList{
				corev1.ResourceName("requests.nvidia.com/gpu"): resource.MustParse("2"),
			},
		},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceName("requests.nvidia.com/gpu"): resource.MustParse("2"),
			},
			Used: corev1.ResourceList{
				corev1.ResourceName("requests.nvidia.com/gpu"): resource.MustParse("1"),
			},
		},
	}

	ctx := t.Context()

	context := NewFakeControllerContext(ctx, []runtime.Object{}, []runtime.Object{resourceQuota})

	controller, admissionHandler, err := clusterresourcequota.NewResourceQuota(ctx, context, nil)
	if err != nil {
		t.Errorf("Error occurred while creating admission plugin: %v", err)
	}
	_ = controller

	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"nvidia.com/gpu.product": "A100",
			},
			Containers: []corev1.Container{
				{
					Name: "container",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
						},
					},
				},
			},
		},
	}
	attr := admission.NewAttributesRecord(newPod, nil, api.Kind("Pod").WithVersion("version"), newPod.Namespace, newPod.Name, corev1.Resource("pods").WithVersion("version"), "", admission.Create, &metav1.CreateOptions{}, false, nil)
	err = admissionHandler.Validate(ctx, attr, nil)
	if err != nil {
		if !apierrors.IsForbidden(err) {
			t.Errorf("unexpected error: %v", err)
		}
		t.Logf("expected error: %v", err)
	} else {
		t.Errorf("expected forbidden error but got none")
	}
}

func NewFakeControllerContext(ctx context.Context, kubeobjects []runtime.Object, thisobjects []runtime.Object) *clusterresourcequota.ControllerContext {
	schema := clusterresourcequota.GetScheme()
	kubeClient := fake.NewSimpleClientset(kubeobjects...)
	metadataClient := metadatafake.NewSimpleMetadataClient(schema, kubeobjects...)
	thisclientset := thisfake.NewSimpleClientset(thisobjects...)
	c, err := clusterresourcequota.NewControllerContextFromClientSet(ctx, kubeClient, thisclientset, metadataClient, 0)
	if err != nil {
		panic(err)
	}
	quotasinformer := c.ThisInformerFactory.Quota().V1().ResourceQuotas().Informer()
	for _, obj := range thisobjects {
		if err := quotasinformer.GetStore().Add(obj); err != nil {
			panic(err)
		}
	}
	return c
}

func NewPod(name string, numContainers int, resources api.ResourceRequirements) *api.Pod {
	pod := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test"},
		Spec:       api.PodSpec{},
	}
	pod.Spec.Containers = make([]api.Container, 0, numContainers)
	for i := range numContainers {
		pod.Spec.Containers = append(pod.Spec.Containers, api.Container{
			Image:     "foo:V" + strconv.Itoa(i),
			Resources: resources,
		})
	}
	return pod
}
