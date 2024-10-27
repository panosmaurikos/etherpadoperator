package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type EtherpadInstanceSpec struct {
	Deployments []DeploymentSpec `json:"deployments,omitempty"`

	ConfigMapData map[string]string `json:"configMapData,omitempty"`
}

type DeploymentSpec struct {
	Name  string `json:"name"`
	Image string `json:"image"`

	Replicas *int32   `json:"replicas"`
	Command  []string `json:"command,omitempty"`

	Args []string `json:"args,omitempty"`

	ContainerPorts []corev1.ContainerPort `json:"containerPorts,omitempty"`

	Env []corev1.EnvVar `json:"env,omitempty"`

	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	Volumes []corev1.Volume `json:"volumes,omitempty"`

	ServicePort int32 `json:"servicePort"`
}

type EtherpadInstanceStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

type EtherpadInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EtherpadInstanceSpec   `json:"spec,omitempty"`
	Status EtherpadInstanceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

type EtherpadInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EtherpadInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EtherpadInstance{}, &EtherpadInstanceList{})
}
