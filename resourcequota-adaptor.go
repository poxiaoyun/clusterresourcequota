package clusterresourcequota

import (
	"context"
	"maps"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/informers/core"
	informerscorev1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	kubernetescorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	thisquotav1 "xiaoshiai.cn/clusterresourcequota/apis/quota/v1"
	thisclientset "xiaoshiai.cn/clusterresourcequota/generated/clientset/versioned"
	thisclientquotav1 "xiaoshiai.cn/clusterresourcequota/generated/clientset/versioned/typed/quota/v1"
	thisinformers "xiaoshiai.cn/clusterresourcequota/generated/informers/externalversions"
	thisinformerscorev1 "xiaoshiai.cn/clusterresourcequota/generated/informers/externalversions/quota/v1"
)

const AnnotationNodeSelector = "conditionalresourcequota." + thisquotav1.GroupName + "/nodeselector"

var _ kubernetes.Interface = &HijackClientSet{}

// HijackClientSet hijacks the ResourceQuota resources to ConditionalResourceQuota resources.
type HijackClientSet struct {
	kubernetes.Interface
	This thisclientset.Interface
}

func (a HijackClientSet) CoreV1() kubernetescorev1.CoreV1Interface {
	return &HijackCoreV1Client{CoreV1Interface: a.Interface.CoreV1(), This: a.This.QuotaV1()}
}

var _ kubernetescorev1.CoreV1Interface = &HijackCoreV1Client{}

type HijackCoreV1Client struct {
	kubernetescorev1.CoreV1Interface
	This thisclientquotav1.QuotaV1Interface
}

func (a HijackCoreV1Client) ResourceQuotas(namespace string) kubernetescorev1.ResourceQuotaInterface {
	return &HijackResourceQuotaInterface{
		ResourceQuotaInterface: a.This.ResourceQuotas(namespace),
	}
}

var _ kubernetescorev1.ResourceQuotaInterface = HijackResourceQuotaInterface{}

type HijackResourceQuotaInterface struct {
	thisclientquotav1.ResourceQuotaInterface
}

func (a HijackResourceQuotaInterface) Apply(ctx context.Context, resourceQuota *applycorev1.ResourceQuotaApplyConfiguration, opts metav1.ApplyOptions) (result *corev1.ResourceQuota, err error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource(corev1.ResourceQuotas.String()), "apply")
}

func (a HijackResourceQuotaInterface) ApplyStatus(ctx context.Context, resourceQuota *applycorev1.ResourceQuotaApplyConfiguration, opts metav1.ApplyOptions) (result *corev1.ResourceQuota, err error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource(corev1.ResourceQuotas.String()), "apply")
}

func (a HijackResourceQuotaInterface) Create(ctx context.Context, resourceQuota *corev1.ResourceQuota, opts metav1.CreateOptions) (*corev1.ResourceQuota, error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource(corev1.ResourceQuotas.String()), "create")
}

func (a HijackResourceQuotaInterface) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return errors.NewMethodNotSupported(corev1.Resource(corev1.ResourceQuotas.String()), "delete")
}

func (a HijackResourceQuotaInterface) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.NewMethodNotSupported(corev1.Resource(corev1.ResourceQuotas.String()), "deletaBatch")
}

func (a HijackResourceQuotaInterface) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.ResourceQuota, error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource(corev1.ResourceQuotas.String()), "get")
}

func (a HijackResourceQuotaInterface) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *corev1.ResourceQuota, err error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource(corev1.ResourceQuotas.String()), "patch")
}

func (a HijackResourceQuotaInterface) Update(ctx context.Context, resourceQuota *corev1.ResourceQuota, opts metav1.UpdateOptions) (*corev1.ResourceQuota, error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource(corev1.ResourceQuotas.String()), "update")
}

func (a HijackResourceQuotaInterface) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, errors.NewMethodNotSupported(corev1.Resource(corev1.ResourceQuotas.String()), "watch")
}

func (a HijackResourceQuotaInterface) List(ctx context.Context, opts metav1.ListOptions) (*corev1.ResourceQuotaList, error) {
	crqlist, err := a.ResourceQuotaInterface.List(ctx, opts)
	if err != nil {
		return nil, err
	}
	items := make([]corev1.ResourceQuota, len(crqlist.Items))
	for i := range crqlist.Items {
		items[i] = *toQuota(&crqlist.Items[i])
	}
	rqlist := &corev1.ResourceQuotaList{
		ListMeta: metav1.ListMeta{
			ResourceVersion:    crqlist.ResourceVersion,
			Continue:           crqlist.Continue,
			RemainingItemCount: crqlist.RemainingItemCount,
		},
		Items: items,
	}
	return rqlist, nil
}

func (a HijackResourceQuotaInterface) UpdateStatus(ctx context.Context, resourceQuota *corev1.ResourceQuota, opts metav1.UpdateOptions) (*corev1.ResourceQuota, error) {
	crq := fromQuota(resourceQuota)
	result, err := a.ResourceQuotaInterface.UpdateStatus(ctx, crq, opts)
	if err != nil {
		return nil, err
	}
	return toQuota(result), nil
}

var _ informers.SharedInformerFactory = &HijackSharedInformerFactory{}

// HijackSharedInformerFactory adapt the ResourceQuota resources to ConditionalResourceQuota resources.
type HijackSharedInformerFactory struct {
	informers.SharedInformerFactory
	This thisinformers.SharedInformerFactory
}

func (a HijackSharedInformerFactory) Core() core.Interface {
	return HijackCoreInterface{Interface: a.SharedInformerFactory.Core(), This: a.This}
}

// Shutdown implements informers.SharedInformerFactory.
func (a HijackSharedInformerFactory) Shutdown() {
	a.SharedInformerFactory.Shutdown()
	a.This.Shutdown()
}

// Start implements informers.SharedInformerFactory.
func (a HijackSharedInformerFactory) Start(stopCh <-chan struct{}) {
	a.SharedInformerFactory.Start(stopCh)
	a.This.Start(stopCh)
}

// WaitForCacheSync implements informers.SharedInformerFactory.
func (a HijackSharedInformerFactory) WaitForCacheSync(stopCh <-chan struct{}) map[reflect.Type]bool {
	var result map[reflect.Type]bool
	maps.Copy(result, a.SharedInformerFactory.WaitForCacheSync(stopCh))
	maps.Copy(result, a.This.WaitForCacheSync(stopCh))
	return result
}

var _ core.Interface = HijackCoreInterface{}

type HijackCoreInterface struct {
	Interface core.Interface
	This      thisinformers.SharedInformerFactory
}

// V1 implements core.Interface.
func (a HijackCoreInterface) V1() informerscorev1.Interface {
	return HijackInformersCoreV1Interface{Interface: a.Interface.V1(), This: a.This}
}

var _ informerscorev1.Interface = HijackInformersCoreV1Interface{}

// HijackInformersCoreV1Interface hijacks the ResourceQuota informer to ConditionalResourceQuota informer.
type HijackInformersCoreV1Interface struct {
	informerscorev1.Interface
	This thisinformers.SharedInformerFactory
}

func (a HijackInformersCoreV1Interface) ResourceQuotas() informerscorev1.ResourceQuotaInformer {
	return HijackResourceQuotaInformer{i: a.This.Quota().V1().ResourceQuotas()}
}

var _ informerscorev1.ResourceQuotaInformer = HijackResourceQuotaInformer{}

// HijackResourceQuotaInformer hijacks the [listerscorev1.ResourceQuotaLister] informer to [thislisterquotav1.ConditionalResourceQuotaLister] informer.
type HijackResourceQuotaInformer struct {
	i thisinformerscorev1.ResourceQuotaInformer
}

func (a HijackResourceQuotaInformer) Informer() cache.SharedIndexInformer {
	// make sure informer's transform is set
	return a.i.Informer()
}

func (a HijackResourceQuotaInformer) Lister() listerscorev1.ResourceQuotaLister {
	return listerscorev1.NewResourceQuotaLister(a.Informer().GetIndexer())
}

func fromQuota(quota *corev1.ResourceQuota) *thisquotav1.ResourceQuota {
	return &thisquotav1.ResourceQuota{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: quota.ObjectMeta,
		Spec:       quota.Spec,
		Status:     quota.Status,
	}
}

func toQuota(quota *thisquotav1.ResourceQuota) *corev1.ResourceQuota {
	return &corev1.ResourceQuota{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: quota.ObjectMeta,
		Spec:       quota.Spec,
		Status:     quota.Status,
	}
}
