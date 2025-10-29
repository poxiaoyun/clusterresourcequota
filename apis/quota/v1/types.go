package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Request",type="string",JSONPath=".status.used",description="Resource Request"
// +kubebuilder:printcolumn:name="Limit",type="string",JSONPath=".status.hard",description="Resource Limit"
type ClusterResourceQuota struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec defines the behavior of the License.
	// +optional
	Spec ClusterResourceQuotaSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`

	// Status describes the current status of a License.
	// +optional
	Status ClusterResourceQuotaStatus `json:"status" protobuf:"bytes,3,opt,name=status"`
}

type ClusterResourceQuotaSpec struct {
	corev1.ResourceQuotaSpec `json:",inline" protobuf:"bytes,1,opt,name=resourceQuotaSpec"`

	// NamespaceSelector is the selector that is used to select namespaces
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty" protobuf:"bytes,2,opt,name=namespaceSelector"`
}

// ClusterStatus is information about the current status of a License.
type ClusterResourceQuotaStatus struct {
	corev1.ResourceQuotaStatus `json:",inline" protobuf:"bytes,1,opt,name=resourceQuotaStatus"`

	// +optional
	// +listType=map
	// +listMapKey=name
	// Namespaces is the list of namespaces on which the resource quota is applied
	Namespaces []NamespaceResourceQuota `json:"namespaces,omitempty" protobuf:"bytes,2,rep,name=namespaces"`
}

type NamespaceResourceQuota struct {
	// Name is the name of the namespace
	// +required
	Name string              `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	Used corev1.ResourceList `json:"used,omitempty" protobuf:"bytes,2,rep,name=used,casttype=ResourceList,castkey=ResourceName"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
type ClusterResourceQuotaList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Items is the list of License objects in the list.
	Items []ClusterResourceQuota `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Request",type="string",JSONPath=".status.used",description="Resource Request"
// +kubebuilder:printcolumn:name="Limit",type="string",JSONPath=".status.hard",description="Resource Limit"
type ResourceQuota struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec defines the behavior of the ConditionalResourceQuota.
	// +optional
	Spec corev1.ResourceQuotaSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`

	// Status describes the current status of a ConditionalResourceQuota.
	// +optional
	Status corev1.ResourceQuotaStatus `json:"status" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
type ResourceQuotaList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Items is the list of ConditionalResourceQuota objects in the list.
	Items []ResourceQuota `json:"items" protobuf:"bytes,2,rep,name=items"`
}
