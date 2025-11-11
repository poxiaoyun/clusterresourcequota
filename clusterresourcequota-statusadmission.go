package clusterresourcequota

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	quotav1 "xiaoshiai.cn/clusterresourcequota/apis/quota/v1"
)

type ResourceQuotaStatusAdmission struct {
	Decoder admission.Decoder
	Cache   *ResourceQuotaCache
	Client  client.Client
}

func NewResourceQuotaStatusAdmission(cache *ResourceQuotaCache, client client.Client) *ResourceQuotaStatusAdmission {
	return &ResourceQuotaStatusAdmission{
		Decoder: admission.NewDecoder(client.Scheme()),
		Cache:   cache,
		Client:  client,
	}
}

func (c *ResourceQuotaStatusAdmission) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logr.FromContextOrDiscard(ctx)
	inst := &quotav1.ResourceQuota{}
	if err := c.Decoder.Decode(req, inst); err != nil {
		log.Error(err, "Decode request")
		return admission.Errored(http.StatusBadRequest, err)
	}
	if inst.Labels == nil {
		return admission.Allowed("Not managed by ClusterResourceQuota")
	}
	clusterresourcequotaname := inst.Labels[LabelClusterResourceQuota]
	if clusterresourcequotaname == "" {
		log.V(1).Info("Not managed by ClusterResourceQuota; Allowed")
		return admission.Allowed("Not managed by ClusterResourceQuota")
	}
	backoff := wait.Backoff{
		Duration: 100 * time.Millisecond,
		Factor:   2.0,
		Jitter:   0.1,
		Steps:    5,
	}
	err := retry.RetryOnConflict(backoff, func() error {
		return c.validate(ctx, inst, clusterresourcequotaname)
	})
	if err != nil {
		log.Error(err, "Validate ResourceQuota status against ClusterResourceQuota")
		if apierrors.IsForbidden(err) {
			return admission.Errored(http.StatusForbidden, err)
		}
		return admission.Errored(http.StatusInternalServerError, err)
	}
	log.V(2).Info("ResourceQuota status validated")
	return admission.Allowed("ResourceQuota status validated")
}

func (c *ResourceQuotaStatusAdmission) validate(ctx context.Context, rq *quotav1.ResourceQuota, clusterresourcequotaname string) error {
	return c.Cache.GetOrCreate(ctx, clusterresourcequotaname).OnLock(ctx, func(cache *ClusterResourceQuotaCache) error {
		crq := &quotav1.ClusterResourceQuota{}
		if err := c.Client.Get(ctx, client.ObjectKey{Name: clusterresourcequotaname}, crq); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}

		oldtotal := corev1.ResourceList{}
		for _, usage := range cache.Quotas {
			oldtotal = quota.Add(oldtotal, usage.Used)
		}
		var oldusage corev1.ResourceList
		if usage, ok := cache.Quotas[rq.Namespace]; ok {
			oldusage = usage.Used
		}
		delta := quota.Subtract(rq.Status.Used, oldusage)
		newtotal := quota.Add(oldtotal, delta)

		// add current request and check against clusterresourcequota status hard limit
		if ok, exceeded := quota.LessThanOrEqual(newtotal, crq.Status.Hard); !ok {
			err := fmt.Errorf("exceeded cluster quota: %s, requested: %s, used: %s, limited: %s",
				crq.Name,
				prettyPrint(quota.Mask(delta, exceeded)),
				prettyPrint(quota.Mask(oldtotal, exceeded)),
				prettyPrint(quota.Mask(crq.Status.Hard, exceeded)))
			return apierrors.NewForbidden(schema.GroupResource{}, "", err)
		}
		// update clusterresourcequota status
		updateClusterResourceQuotaStatusUsed(crq, rq, newtotal)
		// atomic update
		if err := c.Client.Status().Update(ctx, crq); err != nil {
			return err
		}
		// update clusterresourcequota status used
		cache.Quotas[rq.Namespace] = &ResourceUsageInfo{LastUpdate: time.Now(), Used: rq.Status.Used}
		return nil
	})
}

func updateClusterResourceQuotaStatusUsed(crq *quotav1.ClusterResourceQuota, rq *quotav1.ResourceQuota, newtotal corev1.ResourceList) {
	crq.Status.Used = newtotal
	i := slices.IndexFunc(crq.Status.Namespaces, func(n quotav1.NamespaceResourceQuota) bool {
		return n.Name == rq.Namespace
	})
	if i == -1 {
		crq.Status.Namespaces = append(crq.Status.Namespaces, quotav1.NamespaceResourceQuota{Name: rq.Namespace, Used: rq.Status.Used})
	} else {
		crq.Status.Namespaces[i].Used = rq.Status.Used
	}
}

// prettyPrint formats a resource list for usage in errors
// it outputs resources sorted in increasing order
func prettyPrint(item corev1.ResourceList) string {
	parts := []string{}
	keys := []string{}
	for key := range item {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := item[corev1.ResourceName(key)]
		constraint := key + "=" + value.String()
		parts = append(parts, constraint)
	}
	return strings.Join(parts, ",")
}
