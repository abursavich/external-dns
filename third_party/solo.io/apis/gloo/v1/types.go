package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SEE: https://docs.solo.io/gloo-edge/latest/reference/api/github.com/solo-io/gloo/projects/gloo/api/v1/proxy.proto.sk

//+genclient
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+resourceName=proxies

type Proxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProxySpec `json:"spec,omitempty"`
	Status Status    `json:"status,omitempty"`
}

//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Proxy `json:"items"`
}

type ProxySpec struct {
	Listeners []Listener `json:"listener,omitempty"`
}

type Listener struct {
	HTTPListener HTTPListener `json:"httpListener,omitempty"`
}

type HTTPListener struct {
	VirtualHosts []VirtualHost `json:"virtualHosts,omitempty"`
}

type VirtualHost struct {
	Domains  []string            `json:"domains,omitempty"`
	Metadata VirtualHostMetadata `json:"metadata,omitempty"`
}

type VirtualHostMetadata struct {
	Source []VirtualHostMetadataSource `json:"source,omitempty"`
}

type VirtualHostMetadataSource struct {
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

//+genclient
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+resourceName=virtualservices

type VirtualService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VirtualServiceSpec `json:"spec,omitempty"`
	Status Status             `json:"status,omitempty"`
}

type VirtualServiceSpec struct{}

//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type VirtualServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []VirtualService `json:"items"`
}

type Status struct {
	// State is the enum indicating the state of the resource.
	State State `json:"state,omitempty"`
	// Reason is a description of the error for Rejected resources.
	// If the resource is pending or accepted, this field will be empty.
	Reason string `json:"reason,omitempty"`
	// Reference to the reporter who wrote this status.
	ReportedBy string `json:"reported_by,omitempty"`
}

type State string

const (
	// Pending status indicates the resource has not yet been validated.
	StatePending State = "Pending"
	// Accepted indicates the resource has been validated.
	StateAccepted State = "Accepted"
	// Rejected indicates an invalid configuration by the user.
	// Rejected resources may be propagated to the xDS server depending on their severity.
	StateRejected State = "Rejected"
	// Warning indicates a partially invalid configuration by the user.
	// Resources with Warnings may be partially accepted by a controller, depending on the implementation.
	StateWarning State = "Warning"
)
