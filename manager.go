package clusterresourcequota

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metainternal "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/metadata/metadatainformer"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/controller-manager/pkg/informerfactory"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	thisquotav1 "xiaoshiai.cn/clusterresourcequota/apis/quota/v1"
	thisclientset "xiaoshiai.cn/clusterresourcequota/generated/clientset/versioned"
	thisinformers "xiaoshiai.cn/clusterresourcequota/generated/informers/externalversions"
	"xiaoshiai.cn/common"
	liblog "xiaoshiai.cn/common/log"
	libnet "xiaoshiai.cn/common/net"
)

// nolint: gochecknoinits
func GetScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(thisquotav1.AddToScheme(scheme))
	utilruntime.Must(metainternal.AddToScheme(scheme))
	return scheme
}

type Options struct {
	LeaderElection *LeaderElectionOptions `json:"leaderElection,omitempty"`
	Webhook        *WebhookOptions        `json:"webhook,omitempty"`
	Metrics        *MetricsOptions        `json:"metrics,omitempty"`
	ResyncPeriod   time.Duration          `json:"resyncPeriod,omitempty" description:"The resync period of informer factories"`
	Probe          *ProbeOptions          `json:"probe,omitempty"`
}

type WebhookOptions struct {
	Enabled bool   `json:"enabled,omitempty" description:"Enable webhook"`
	Addr    string `json:"addr,omitempty" description:"The address the webhook server binds to."`
	CertDir string `json:"certDir,omitempty" description:"The directory that contains the server key and certificate."`
}

type LeaderElectionOptions struct {
	Enabled bool   `json:"enabled,omitempty" description:"Enable leader election"`
	ID      string `json:"id,omitempty" description:"Leader election ID"`
}

type MetricsOptions struct {
	Enabled bool   `json:"enabled,omitempty" description:"Enable metrics endpoint"`
	Addr    string `json:"addr,omitempty" description:"The address the metric endpoint binds to."`
}

type ProbeOptions struct {
	Enabled bool   `json:"enabled,omitempty" description:"Enable health probe endpoint"`
	Addr    string `json:"addr,omitempty" description:"The address the health probe endpoint binds to."`
}

func NewDefaultOptions() *Options {
	return &Options{
		LeaderElection: &LeaderElectionOptions{
			Enabled: false,
			ID:      "clusterresourcequota" + common.GroupPrefix,
		},
		Webhook: &WebhookOptions{
			Enabled: true,
			Addr:    ":8443",
			CertDir: "certs",
		},
		Metrics: &MetricsOptions{
			Enabled: true,
			Addr:    ":9090",
		},
		Probe: &ProbeOptions{
			Enabled: true,
			Addr:    ":8080",
		},
		ResyncPeriod: time.Hour,
	}
}

func Run(ctx context.Context, options *Options) error {
	log.SetLogger(liblog.DefaultLogger)
	setupLog := log.Log.WithName("setup")

	managerOptions := manager.Options{
		BaseContext: func() context.Context { return ctx },
		Scheme:      GetScheme(),
	}
	if options.LeaderElection.Enabled {
		setupLog.Info("leader election enabled")
		managerOptions.LeaderElection = true
		managerOptions.LeaderElectionID = options.LeaderElection.ID
	}
	if options.Webhook.Enabled {
		webhookHost, webhookportstr := libnet.SplitHostPort(options.Webhook.Addr)
		webhookPort, _ := strconv.Atoi(webhookportstr)
		webhookOptions := webhook.Options{
			Host: webhookHost, Port: webhookPort,
			CertDir: options.Webhook.CertDir,
		}
		managerOptions.WebhookServer = webhook.NewServer(webhookOptions)
	}
	if options.Metrics.Enabled {
		managerOptions.Metrics = server.Options{BindAddress: options.Metrics.Addr}
	}
	if options.Probe.Enabled {
		managerOptions.HealthProbeBindAddress = options.Probe.Addr
	}
	cfg, err := config.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}
	mgr, err := manager.New(cfg, managerOptions)
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		return err
	}
	if err := Setup(ctx, mgr, options); err != nil {
		setupLog.Error(err, "unable to create plugin controller", "controller", "plugin")
		return err
	}
	mgr.AddHealthzCheck("ping", healthz.Ping)

	setupLog.Info("starting manager")
	return mgr.Start(ctx)
}

func Setup(ctx context.Context, mgr ctrl.Manager, options *Options) error {
	if err := NewClusterResourceQuota(ctx, mgr); err != nil {
		return fmt.Errorf("create cluster resource quota controller: %w", err)
	}
	cli, restconfig := mgr.GetClient(), mgr.GetConfig()
	context, err := NewControllerContext(ctx, restconfig, options)
	if err != nil {
		return err
	}
	resourceQuotaController, resourceQuotaAdmission, err := NewResourceQuota(ctx, context)
	if err != nil {
		return fmt.Errorf("create resource quota admission: %w", err)
	}
	go context.Start(ctx)
	go resourceQuotaController.Run(ctx)

	mgr.GetWebhookServer().
		Register("/validate",
			&admission.Webhook{
				Handler: ValidationInterfaceAdaptor{
					Validation: resourceQuotaAdmission,
					Schema:     cli.Scheme(),
				},
			})

	return nil
}

func TrimMetadata(obj any) (any, error) {
	if accessor, err := meta.Accessor(obj); err == nil {
		if accessor.GetManagedFields() != nil {
			accessor.SetManagedFields(nil)
		}
	}
	return obj, nil
}

func ResyncPeriod(d time.Duration) func() time.Duration {
	return func() time.Duration {
		factor := rand.Float64() + 1
		return time.Duration(float64(d.Nanoseconds()) * factor)
	}
}

func NewControllerContext(ctx context.Context, restconfig *rest.Config, options *Options) (*ControllerContext, error) {
	clientset, err := kubernetes.NewForConfig(restconfig)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	metadataClient, err := metadata.NewForConfig(restconfig)
	if err != nil {
		return nil, fmt.Errorf("create metadata client: %w", err)
	}
	thisclientset, err := thisclientset.NewForConfig(restconfig)
	if err != nil {
		return nil, fmt.Errorf("create this client: %w", err)
	}
	return NewControllerContextFromClientSet(ctx, clientset, thisclientset, metadataClient, options.ResyncPeriod)
}

func NewControllerContextFromClientSet(ctx context.Context,
	clientset kubernetes.Interface,
	thisclientset thisclientset.Interface,
	metadataClient metadata.Interface,
	resyncPeriod time.Duration,
) (*ControllerContext, error) {
	sharedInformers := informers.NewSharedInformerFactoryWithOptions(clientset, ResyncPeriod(resyncPeriod)())
	thisinformersfactory := thisinformers.NewSharedInformerFactoryWithOptions(thisclientset, ResyncPeriod(resyncPeriod)())
	// hijack informer is same as thisinformersfactory but ConditionalResourceQuotaInformer is adapted to ResourceQuotaInformer
	hijackedthisinformersfactory := thisinformers.NewSharedInformerFactoryWithOptions(thisclientset, ResyncPeriod(resyncPeriod)())
	metadataInformers := metadatainformer.NewSharedInformerFactoryWithOptions(metadataClient, ResyncPeriod(resyncPeriod)(), metadatainformer.WithTransform(TrimMetadata))
	context := &ControllerContext{
		InformersStarted:                make(chan struct{}),
		Clientset:                       clientset,
		InformerFactory:                 sharedInformers,
		ObjectOrMetadataInformerFactory: informerfactory.NewInformerFactory(sharedInformers, metadataInformers),
		ThisClientSet:                   thisclientset,
		ThisInformerFactory:             thisinformersfactory,
		HijackedInformerFactory:         hijackedthisinformersfactory,
	}
	return context, nil
}

type ControllerContext struct {
	InformersStarted chan struct{}

	Clientset                       kubernetes.Interface
	InformerFactory                 informers.SharedInformerFactory
	ObjectOrMetadataInformerFactory informerfactory.InformerFactory

	ThisClientSet           thisclientset.Interface
	ThisInformerFactory     thisinformers.SharedInformerFactory
	HijackedInformerFactory thisinformers.SharedInformerFactory
}

func (c *ControllerContext) Start(ctx context.Context) {
	go c.ThisInformerFactory.Start(ctx.Done())
	go c.InformerFactory.Start(ctx.Done())
	go c.ObjectOrMetadataInformerFactory.Start(ctx.Done())
	go c.HijackedInformerFactory.Start(ctx.Done())
	close(c.InformersStarted)
}
