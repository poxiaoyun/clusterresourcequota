package clusterresourcequota

import (
	"context"
	"errors"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apiserveradmission "k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/authentication/user"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type ValidationInterfaceAdaptor struct {
	Validation apiserveradmission.ValidationInterface
	Schema     *runtime.Scheme
}

func (v ValidationInterfaceAdaptor) Handle(ctx context.Context, req admission.Request) admission.Response {
	if !v.Validation.Handles(apiserveradmission.Operation(req.Operation)) {
		return admission.Allowed("")
	}
	current, old, options, err := v.decodeObject(ctx, req)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	attr := apiserveradmission.NewAttributesRecord(
		current,
		old,
		schema.GroupVersionKind{Group: req.Kind.Group, Version: req.Kind.Version, Kind: req.Kind.Kind},
		req.Namespace,
		req.Name,
		schema.GroupVersionResource{Group: req.Resource.Group, Version: req.Resource.Version, Resource: req.Resource.Resource},
		req.SubResource,
		apiserveradmission.Operation(req.Operation),
		options,
		false,
		&user.DefaultInfo{
			UID:    req.UserInfo.UID,
			Groups: req.UserInfo.Groups,
			Name:   req.UserInfo.Username,
			Extra:  v.convertUserInfoExtra(req.UserInfo.Extra),
		},
	)
	objectCreator := apiserveradmission.NewObjectInterfacesFromScheme(v.Schema)
	if err := v.Validation.Validate(ctx, attr, objectCreator); err != nil {
		var statusErr apierrors.APIStatus
		if !errors.As(err, &statusErr) {
			return admission.Errored(http.StatusBadRequest, err)
		}
		return admission.Response{
			AdmissionResponse: admissionv1.AdmissionResponse{
				Allowed: false,
				Result:  ptr.To(statusErr.Status()),
			},
		}
	}
	return admission.Allowed("")
}

func (v ValidationInterfaceAdaptor) decodeObject(_ context.Context, req admission.Request) (runtime.Object, runtime.Object, runtime.Object, error) {
	dec := admission.NewDecoder(v.Schema)
	var current, old, options runtime.Object
	gvk := schema.GroupVersionKind{Group: req.Kind.Group, Version: req.Kind.Version, Kind: req.Kind.Kind}
	if req.Object.Raw != nil {
		empty, err := v.Schema.New(gvk)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := dec.DecodeRaw(req.Object, empty); err != nil {
			return nil, nil, nil, err
		}
		current = empty
	}
	if req.OldObject.Raw != nil {
		empty, err := v.Schema.New(gvk)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := dec.DecodeRaw(req.OldObject, empty); err != nil {
			return nil, nil, nil, err
		}
		old = empty
	}
	if req.Options.Raw != nil {
		switch req.Operation {
		case admissionv1.Create:
			create := &metav1.CreateOptions{}
			if err := dec.DecodeRaw(req.Options, create); err != nil {
				return nil, nil, nil, err
			}
			options = create
		case admissionv1.Update:
			update := &metav1.UpdateOptions{}
			if err := dec.DecodeRaw(req.Options, update); err != nil {
				return nil, nil, nil, err
			}
			options = update
		case admissionv1.Delete:
			delete := &metav1.DeleteOptions{}
			if err := dec.DecodeRaw(req.Options, delete); err != nil {
				return nil, nil, nil, err
			}
			options = delete
		}
	}
	return current, old, options, nil
}

func (v ValidationInterfaceAdaptor) convertUserInfoExtra(in map[string]authenticationv1.ExtraValue) map[string][]string {
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = []string(v)
	}
	return out
}
