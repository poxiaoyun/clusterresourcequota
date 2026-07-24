package clusterresourcequota

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestMutatePodGPUIsolation(t *testing.T) {
	t.Run("forces void and removes duplicate environment variables from containers without GPUs", func(t *testing.T) {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{{
					Name: "init",
					Env: []corev1.EnvVar{{
						Name:      nvidiaVisibleDevicesEnvName,
						ValueFrom: &corev1.EnvVarSource{},
					}},
				}},
				Containers: []corev1.Container{{
					Name: "app",
					Env: []corev1.EnvVar{
						{Name: "EXISTING", Value: "value"},
						{Name: nvidiaVisibleDevicesEnvName, Value: "all"},
						{Name: nvidiaVisibleDevicesEnvName, Value: "0"},
					},
				}},
				EphemeralContainers: []corev1.EphemeralContainer{{
					EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug"},
				}},
			},
		}

		if !MutatePodGPUIsolation(pod) {
			t.Fatal("expected pod to be mutated")
		}
		assertNVIDIAVisibleDevicesVoid(t, pod.Spec.InitContainers[0].Env)
		assertNVIDIAVisibleDevicesVoid(t, pod.Spec.Containers[0].Env)
		assertNVIDIAVisibleDevicesVoid(t, pod.Spec.EphemeralContainers[0].Env)
		if !hasEnv(pod.Spec.Containers[0].Env, "EXISTING", "value") {
			t.Fatal("expected unrelated environment variables to be preserved")
		}
		if MutatePodGPUIsolation(pod) {
			t.Fatal("expected a second mutation to be idempotent")
		}
	})

	t.Run("leaves an NVIDIA GPU container unchanged", func(t *testing.T) {
		pod := podWithResource(nvidiaGPUResource, corev1ResourceLimits, "1")
		pod.Spec.Containers[0].Env = []corev1.EnvVar{{Name: nvidiaVisibleDevicesEnvName, Value: "all"}}

		if MutatePodGPUIsolation(pod) {
			t.Fatal("expected NVIDIA GPU container to remain unchanged")
		}
		if !hasEnv(pod.Spec.Containers[0].Env, nvidiaVisibleDevicesEnvName, "all") {
			t.Fatal("expected GPU container environment variable to remain unchanged")
		}
	})

	t.Run("leaves a Volcano vGPU container unchanged", func(t *testing.T) {
		pod := podWithResource(volcanoVGPUResource, corev1ResourceRequests, "1")

		if MutatePodGPUIsolation(pod) {
			t.Fatal("expected Volcano vGPU container to remain unchanged")
		}
		if len(pod.Spec.Containers[0].Env) != 0 {
			t.Fatalf("expected no environment mutation, got: %#v", pod.Spec.Containers[0].Env)
		}
	})

	t.Run("treats a zero GPU quantity as no GPU request", func(t *testing.T) {
		pod := podWithResource(nvidiaGPUResource, corev1ResourceLimits, "0")

		if !MutatePodGPUIsolation(pod) {
			t.Fatal("expected zero GPU quantity container to be mutated")
		}
		assertNVIDIAVisibleDevicesVoid(t, pod.Spec.Containers[0].Env)
	})
}

func TestPodGPUIsolationAdmissionHuaweiDoesNotMutate(t *testing.T) {
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cpu-only",
			Namespace: "tenant",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}},
		},
	}
	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatalf("marshal pod: %v", err)
	}
	handler := NewPodGPUIsolationAdmission(AcceleratorVendorHuawei, GetScheme())
	response := handler.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: raw},
		},
	})

	if !response.Allowed {
		t.Fatalf("expected Huawei admission request to be allowed: %#v", response.Result)
	}
	if len(response.Patches) != 0 {
		t.Fatalf("expected no patches for Huawei cluster, got: %#v", response.Patches)
	}
}

func TestValidateAcceleratorVendor(t *testing.T) {
	for _, vendor := range []string{AcceleratorVendorNVIDIA, AcceleratorVendorHuawei} {
		if err := ValidateAcceleratorVendor(vendor); err != nil {
			t.Fatalf("expected vendor %q to be valid: %v", vendor, err)
		}
	}
	if err := ValidateAcceleratorVendor("other"); err == nil {
		t.Fatal("expected unsupported vendor to be rejected")
	}
}

type resourceListTarget string

const (
	corev1ResourceLimits   resourceListTarget = "limits"
	corev1ResourceRequests resourceListTarget = "requests"
)

func podWithResource(name corev1.ResourceName, target resourceListTarget, value string) *corev1.Pod {
	requirements := corev1.ResourceRequirements{}
	switch target {
	case corev1ResourceLimits:
		requirements.Limits = corev1.ResourceList{name: resource.MustParse(value)}
	case corev1ResourceRequests:
		requirements.Requests = corev1.ResourceList{name: resource.MustParse(value)}
	}
	return &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:      "app",
				Resources: requirements,
			}},
		},
	}
}

func assertNVIDIAVisibleDevicesVoid(t *testing.T, env []corev1.EnvVar) {
	t.Helper()
	count := 0
	for i := range env {
		if env[i].Name != nvidiaVisibleDevicesEnvName {
			continue
		}
		count++
		if env[i].Value != nvidiaVisibleDevicesVoid || env[i].ValueFrom != nil {
			t.Fatalf("expected NVIDIA_VISIBLE_DEVICES=void, got: %#v", env[i])
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one NVIDIA_VISIBLE_DEVICES entry, got %d: %#v", count, env)
	}
}

func hasEnv(env []corev1.EnvVar, name, value string) bool {
	for i := range env {
		if env[i].Name == name && env[i].Value == value {
			return true
		}
	}
	return false
}
