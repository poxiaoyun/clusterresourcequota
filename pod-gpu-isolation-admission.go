package clusterresourcequota

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	AcceleratorVendorNVIDIA = "nvidia"
	AcceleratorVendorHuawei = "huawei"

	nvidiaVisibleDevicesEnvName = "NVIDIA_VISIBLE_DEVICES"
	nvidiaVisibleDevicesVoid    = "void"

	nvidiaGPUResource   corev1.ResourceName = "nvidia.com/gpu"
	volcanoVGPUResource corev1.ResourceName = "volcano.sh/vgpu-number"
)

type PodGPUIsolationAdmission struct {
	acceleratorVendor string
	scheme            *runtime.Scheme
}

func NewPodGPUIsolationAdmission(acceleratorVendor string, scheme *runtime.Scheme) *PodGPUIsolationAdmission {
	return &PodGPUIsolationAdmission{
		acceleratorVendor: acceleratorVendor,
		scheme:            scheme,
	}
}

func ValidateAcceleratorVendor(vendor string) error {
	switch vendor {
	case AcceleratorVendorNVIDIA, AcceleratorVendorHuawei:
		return nil
	default:
		return fmt.Errorf("unsupported accelerator vendor %q, supported values are %q and %q",
			vendor, AcceleratorVendorNVIDIA, AcceleratorVendorHuawei)
	}
}

func (a *PodGPUIsolationAdmission) Handle(_ context.Context, req admission.Request) admission.Response {
	if a.acceleratorVendor != AcceleratorVendorNVIDIA {
		return admission.Allowed("")
	}
	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return admission.Allowed("")
	}

	pod := &corev1.Pod{}
	if err := admission.NewDecoder(a.scheme).Decode(req, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if !MutatePodGPUIsolation(pod) {
		return admission.Allowed("")
	}

	mutated, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, mutated)
}

func MutatePodGPUIsolation(pod *corev1.Pod) bool {
	changed := false
	for i := range pod.Spec.InitContainers {
		if !requestsGPU(pod.Spec.InitContainers[i].Resources) {
			var containerChanged bool
			pod.Spec.InitContainers[i].Env, containerChanged = forceNVIDIAVisibleDevicesVoid(
				pod.Spec.InitContainers[i].Env,
			)
			changed = changed || containerChanged
		}
	}
	for i := range pod.Spec.Containers {
		if !requestsGPU(pod.Spec.Containers[i].Resources) {
			var containerChanged bool
			pod.Spec.Containers[i].Env, containerChanged = forceNVIDIAVisibleDevicesVoid(
				pod.Spec.Containers[i].Env,
			)
			changed = changed || containerChanged
		}
	}
	for i := range pod.Spec.EphemeralContainers {
		if !requestsGPU(pod.Spec.EphemeralContainers[i].Resources) {
			var containerChanged bool
			pod.Spec.EphemeralContainers[i].Env, containerChanged = forceNVIDIAVisibleDevicesVoid(
				pod.Spec.EphemeralContainers[i].Env,
			)
			changed = changed || containerChanged
		}
	}
	return changed
}

func requestsGPU(resources corev1.ResourceRequirements) bool {
	for _, resourceList := range []corev1.ResourceList{resources.Limits, resources.Requests} {
		for _, resourceName := range []corev1.ResourceName{nvidiaGPUResource, volcanoVGPUResource} {
			if quantity, ok := resourceList[resourceName]; ok && quantity.Sign() > 0 {
				return true
			}
		}
	}
	return false
}

func forceNVIDIAVisibleDevicesVoid(env []corev1.EnvVar) ([]corev1.EnvVar, bool) {
	result := make([]corev1.EnvVar, 0, len(env)+1)
	found := false
	changed := false

	for i := range env {
		if env[i].Name != nvidiaVisibleDevicesEnvName {
			result = append(result, env[i])
			continue
		}
		if found {
			changed = true
			continue
		}
		found = true
		replacement := corev1.EnvVar{Name: nvidiaVisibleDevicesEnvName, Value: nvidiaVisibleDevicesVoid}
		if env[i].Value != replacement.Value || env[i].ValueFrom != nil {
			changed = true
		}
		result = append(result, replacement)
	}

	if !found {
		result = append(result, corev1.EnvVar{
			Name:  nvidiaVisibleDevicesEnvName,
			Value: nvidiaVisibleDevicesVoid,
		})
		changed = true
	}
	return result, changed
}
