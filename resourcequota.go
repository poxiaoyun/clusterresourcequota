package clusterresourcequota

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/plugin/resourcequota"
	resourcequotaapi "k8s.io/apiserver/pkg/admission/plugin/resourcequota/apis/resourcequota"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/apiserver/pkg/quota/v1/generic"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/controller-manager/pkg/informerfactory"
	pkgcontroller "k8s.io/kubernetes/pkg/controller"
	resourcequotacontroller "k8s.io/kubernetes/pkg/controller/resourcequota"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	quotainstall "k8s.io/kubernetes/pkg/quota/v1/install"
	thisquotav1 "xiaoshiai.cn/clusterresourcequota/apis/quota/v1"
)

func NewResourceQuota(ctx context.Context, context *ControllerContext, rqConfig *resourcequotaapi.Configuration) (*ConditionalResourceQuotaController, admission.ValidationInterface, error) {
	f := quota.ListerForResourceFunc(generic.ListerFuncForResourceFunc(context.InformerFactory.ForResource))
	evaluators := []quota.Evaluator{
		NewConditionalPodEvaluator(context.InformerFactory),
		core.NewServiceEvaluator(f),
		core.NewPersistentVolumeClaimEvaluator(f),
	}
	ignoredResources := quotainstall.DefaultIgnoredResources()

	// it make hijackInformer store corev1.ResourceQuota instead of ConditionalResourceQuota
	hijackInformer := context.HijackedInformerFactory.Quota().V1().ResourceQuotas().Informer()
	hijackInformer.SetTransform(func(i any) (any, error) {
		crq, ok := i.(*thisquotav1.ResourceQuota)
		if !ok {
			return nil, errors.NewBadRequest("not a ConditionalResourceQuota")
		}
		return toQuota(crq), nil
	})

	hijackInformers := HijackSharedInformerFactory{SharedInformerFactory: context.InformerFactory, This: context.HijackedInformerFactory}
	hijackClientSet := HijackClientSet{Interface: context.Clientset, This: context.ThisClientSet}

	config := generic.NewConfiguration(evaluators, ignoredResources)

	admission, err := NewResourceQuotaAdmission(ctx, hijackClientSet, hijackInformers, config, rqConfig)
	if err != nil {
		return nil, nil, err
	}
	quotacontroller, err := NewConditionalResourceQuotaController(ctx,
		context.Clientset.Discovery(),
		hijackClientSet.CoreV1(),
		hijackInformers.Core().V1().ResourceQuotas(),
		context.ObjectOrMetadataInformerFactory, context.InformersStarted, config)
	if err != nil {
		return nil, nil, err
	}
	return quotacontroller, admission, nil
}

func NewResourceQuotaAdmission(ctx context.Context, clientset kubernetes.Interface, informers informers.SharedInformerFactory, c quota.Configuration, rqConfig *resourcequotaapi.Configuration) (*resourcequota.QuotaAdmission, error) {
	quotaAdmission, err := resourcequota.NewResourceQuota(rqConfig, 5)
	if err != nil {
		return nil, err
	}
	quotaAdmission.SetDrainedNotification(ctx.Done())
	quotaAdmission.SetQuotaConfiguration(c)
	quotaAdmission.SetExternalKubeClientSet(clientset)
	quotaAdmission.SetExternalKubeInformerFactory(informers)
	return quotaAdmission, nil
}

func NewConditionalResourceQuotaController(ctx context.Context,
	discovery discovery.DiscoveryInterface,
	quotaclient corev1client.ResourceQuotasGetter,
	quotaInformer coreinformers.ResourceQuotaInformer,
	objectmetadatainformer informerfactory.InformerFactory,
	informersStarted chan struct{},
	quotaConfiguration quota.Configuration,
) (*ConditionalResourceQuotaController, error) {
	discoveryFunc := discovery.ServerPreferredNamespacedResources
	options := &resourcequotacontroller.ControllerOptions{
		QuotaClient:               quotaclient,
		ResourceQuotaInformer:     quotaInformer,
		ResyncPeriod:              pkgcontroller.StaticResyncPeriodFunc(10 * time.Minute),
		InformerFactory:           objectmetadatainformer,
		ReplenishmentResyncPeriod: pkgcontroller.StaticResyncPeriodFunc(12 * time.Hour),
		DiscoveryFunc:             discoveryFunc,
		IgnoredResourcesFunc:      quotaConfiguration.IgnoredResources,
		InformersStarted:          informersStarted,
		Registry:                  generic.NewRegistry(quotaConfiguration.Evaluators()),
		UpdateFilter:              quotainstall.DefaultUpdateFilter(),
	}
	resourceQuotaController, err := resourcequotacontroller.NewController(ctx, options)
	if err != nil {
		return nil, err
	}
	return &ConditionalResourceQuotaController{
		Controller:    resourceQuotaController,
		discoveryFunc: discoveryFunc,
		started:       informersStarted,
	}, nil
}

type ConditionalResourceQuotaController struct {
	started       chan struct{}
	discoveryFunc resourcequotacontroller.NamespacedResourcesFunc
	*resourcequotacontroller.Controller
}

func (c *ConditionalResourceQuotaController) HasStarted() bool {
	select {
	case <-c.started:
		return true
	default:
		return false
	}
}

func (c *ConditionalResourceQuotaController) Run(ctx context.Context) {
	eg := errgroup.Group{}
	eg.Go(func() error {
		c.Controller.Run(ctx, 1)
		return nil
	})
	if c.discoveryFunc != nil {
		eg.Go(func() error {
			// Periodically the quota controller to detect new resource types
			c.Controller.Sync(ctx, c.discoveryFunc, time.Minute)
			return nil
		})
	}
	eg.Wait()
}
