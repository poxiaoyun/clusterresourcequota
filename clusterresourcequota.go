package clusterresourcequota

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func NewClusterResourceQuota(ctx context.Context, mgr manager.Manager) error {
	controller := NewClusterResourceQuotaReconciler(mgr.GetClient())
	if err := controller.Setup(mgr); err != nil {
		return err
	}
	cache := NewResourceQuotaCache()
	// copy quotas from client cache to our cache periodically
	mgr.Add(&CacheSyner{Cache: cache, Client: mgr.GetClient(), Interval: 30 * time.Second})
	webhook := NewResourceQuotaStatusAdmission(cache, mgr.GetClient())
	mgr.GetWebhookServer().Register("/validate-resourcequota-status", &admission.Webhook{Handler: webhook})
	webhookRemove := NewResourceQuotaRemoveAdmission(mgr.GetClient())
	mgr.GetWebhookServer().Register("/validate-resourcequota-remove", &admission.Webhook{Handler: webhookRemove})
	return nil
}
