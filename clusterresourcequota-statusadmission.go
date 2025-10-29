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
	authnv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	quota "k8s.io/apiserver/pkg/quota/v1"
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
	// only handle requests from our Resource Quota Admission,
	// AKA, only handle status update requests from apiserver
	if !isResourceQuotaAdmissionControllerServiceAccount(&req.UserInfo) {
		log.V(1).Info("Request is not from Kubernetes Resource Quota Admission; Allowed")
		return admission.Allowed("Request is not from Kubernetes Resource Quota Admission")
	}
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
	clusterresourcequota := &quotav1.ClusterResourceQuota{}
	if err := c.Client.Get(ctx, client.ObjectKey{Name: clusterresourcequotaname}, clusterresourcequota); err != nil {
		if apierrors.IsNotFound(err) {
			return admission.Allowed("Not managed by ClusterResourceQuota")
		}
		return admission.Errored(http.StatusInternalServerError, err)
	}
	if err := c.validate(ctx, inst, clusterresourcequota); err != nil {
		log.Error(err, "Validate ResourceQuota status")
		return admission.Errored(http.StatusForbidden, err)
	}
	log.V(2).Info("ResourceQuota status validated")
	return admission.Allowed("ResourceQuota status validated")
}

func isResourceQuotaAdmissionControllerServiceAccount(user *authnv1.UserInfo) bool {
	// Treat nil user same as admission controller SA so that unit tests do not need to
	// specify admission controller SA.
	return user == nil || user.Username == "kubernetes-admin" ||
		slices.Contains(user.Groups, "system:masters")
}

func (c *ResourceQuotaStatusAdmission) validate(ctx context.Context, rq *quotav1.ResourceQuota, crq *quotav1.ClusterResourceQuota) error {
	return c.Cache.GetOrCreate(ctx, crq.Name).OnLock(ctx, func(cache *ClusterResourceQuotaCache) error {
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
			return fmt.Errorf("exceeded cluster quota: %s, requested: %s, used: %s, limited: %s",
				crq.Name,
				prettyPrint(quota.Mask(delta, exceeded)),
				prettyPrint(quota.Mask(oldtotal, exceeded)),
				prettyPrint(quota.Mask(crq.Status.Hard, exceeded)))
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
