package clusterresourcequota

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	quotav1 "xiaoshiai.cn/clusterresourcequota/apis/quota/v1"
)

type ResourceQuotaRemoveAdmission struct {
	Decoder admission.Decoder
	Client  client.Client
}

func NewResourceQuotaRemoveAdmission(client client.Client) *ResourceQuotaRemoveAdmission {
	return &ResourceQuotaRemoveAdmission{
		Decoder: admission.NewDecoder(client.Scheme()),
		Client:  client,
	}
}

func (c *ResourceQuotaRemoveAdmission) Handle(ctx context.Context, req admission.Request) admission.Response {
	switch req.Operation {
	case v1.Update:
		return c.handleUpdate(ctx, req)
	case v1.Delete:
		return c.handleDelete(ctx, req)
	default:
		return admission.Allowed("Operation allowed")
	}
}

// handleUpdate prevents removal of LabelClusterResourceQuota when the ResourceQuota
// was previously managed by a ClusterResourceQuota.
func (c *ResourceQuotaRemoveAdmission) handleUpdate(ctx context.Context, req admission.Request) admission.Response {
	log := logr.FromContextOrDiscard(ctx)
	inst := &quotav1.ResourceQuota{}
	if err := c.Decoder.Decode(req, inst); err != nil {
		log.Error(err, "Decode request")
		return admission.Errored(http.StatusBadRequest, err)
	}
	// decode old object
	old := &quotav1.ResourceQuota{}
	if req.OldObject.Raw != nil {
		if err := c.Decoder.DecodeRaw(req.OldObject, old); err != nil {
			log.Error(err, "Decode old object")
			return admission.Errored(http.StatusBadRequest, err)
		}
	}
	oldName := ""
	if old.Labels != nil {
		oldName = old.Labels[LabelClusterResourceQuota]
	}
	newName := ""
	if inst.Labels != nil {
		newName = inst.Labels[LabelClusterResourceQuota]
	}
	if oldName != "" && newName != oldName {
		msg := fmt.Errorf("cannot change label %q of managed ResourceQuota from %q to %q", LabelClusterResourceQuota, oldName, newName)
		log.V(1).Error(msg, "Forbidden label change")
		return admission.Errored(http.StatusForbidden, msg)
	}
	return admission.Allowed("ResourceQuota update validated")
}

// handleDelete prevents deletion of a ResourceQuota when the corresponding
// ClusterResourceQuota still exists.
func (c *ResourceQuotaRemoveAdmission) handleDelete(ctx context.Context, req admission.Request) admission.Response {
	log := logr.FromContextOrDiscard(ctx)
	if req.OldObject.Raw == nil {
		return admission.Allowed("Unknown")
	}
	target := &quotav1.ResourceQuota{}
	if err := c.Decoder.DecodeRaw(req.OldObject, target); err != nil {
		log.Error(err, "Decode request")
		return admission.Errored(http.StatusBadRequest, err)
	}
	// Resource may be in OldObject for delete
	if len(target.Labels) == 0 {
		return admission.Allowed("Not managed by ClusterResourceQuota")
	}
	clusterresourcequotaname := target.Labels[LabelClusterResourceQuota]
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
	if clusterresourcequota.DeletionTimestamp != nil {
		return admission.Allowed("ClusterResourceQuota is being deleted")
	}
	// if clusterresourcequota exists, forbid deletion
	msg := fmt.Errorf("resourcequota managed by ClusterResourceQuota %q cannot be deleted", clusterresourcequotaname)
	log.V(1).Error(msg, "Forbidden deletion")
	return admission.Errored(http.StatusForbidden, msg)
}
