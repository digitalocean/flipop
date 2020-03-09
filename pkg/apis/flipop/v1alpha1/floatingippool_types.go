/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// IPStateActive indicates an IP is assigned to a matching node.
	IPStateActive IPState = "active"
	// IPStateInProgress indicates an action against the IP is in progress.
	IPStateInProgress IPState = "in-progress"
	// IPStateError indicates the last action against the IP failed.
	IPStateError IPState = "error"
	// IPStateNoMatch indicates the IP is assigned, but the target node is not matching.
	IPStateNoMatch IPState = "no-match"
	// IPStateUnassigned indicates the IP is currently unassigned.
	IPStateUnassigned IPState = "unassigned"
	// IPStateDisabled indicates the IP is beyond the desiredIPs limit, and temporarily disabled.
	IPStateDisabled IPState = "disabled"
)

// FloatingIPPoolSpec defines the desired state of FloatingIPPool.
type FloatingIPPoolSpec struct {
	// IPs is a list of floating IP addresses for assignment. IPs may be omitted or incomplete if
	// DesiredIPs is provided.
	IPs []string `json:"ips"`

	// DesiredIPs specifies the total number of IPs which should be available for assignment. If
	// DesiredIPs is 0 or omitted, all IPs in the IPs field will be used.
	DesiredIPs int `json:"desiredIPs,omitempty"`

	// Provider describes the provider hosting the specified IPs. It's assumed all matching nodes
	// are associated with the specified provider.
	Provider string `json:"provider,omitempty"`

	// Region describes the region associated with the specified IPs. It's assumed all matching
	// nodes are associated with the specified region.
	Region string `json:"region,omitempty"`

	// Match describes the set of nodes to assign IPs.
	Match Match `json:"match"`
}

// Match describes a pattern for finding resources the floating-IP should follow.
type Match struct {
	// NodeLabel is used to restrict the nodes IPs can be assigned to. Empty string matches all.
	NodeLabel string `json:"nodeLabel,omitempty"`

	// PodLabel, if specified, requires candidate nodes include at least one matching
	// pod in the "Running" states and "Ready" condition.
	PodLabel string `json:"podLabel,omitempty"`

	// PodNamespace restricts the namespace used for pod matching. If omitted, all namespaces are
	// considered.
	PodNamespace string `json:"podNamespace,omitempty"`

	// Tolerations is a list of node taints we will tolerate when deciding if the
	// node is a suitable candidate.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// FloatingIPPoolStatus defines the observed state of FloatingIPPool.
type FloatingIPPoolStatus struct {
	IPs             map[string]IPStatus `json:"ips,omitempty"`
	NodeErrors      []string            `json:"nodeErrors,omitempty"`
	AssignableNodes []string            `json:"assignableNodes,omitempty"`
	Error           string              `json:"error,omitempty"`
}

// IPState describes the condition of an IP.
type IPState string

// IPStatus describes the mapping between IPs and the matching
// resources responsible for their attachment.
type IPStatus struct {
	State      IPState `json:"state"`
	NodeName   string  `json:"nodeName,omitempty"`
	ProviderID string  `json:"providerID,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +resource:path=floatingippools

// FloatingIPPool is the Schema for the floatingippools API
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type FloatingIPPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FloatingIPPoolSpec   `json:"spec,omitempty"`
	Status FloatingIPPoolStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +resource:path=floatingippools

// FloatingIPPoolList contains a list of FloatingIPPool
type FloatingIPPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FloatingIPPool `json:"items"`
}
