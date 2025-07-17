// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2021 Digital Ocean, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

const (
	// NodeDNSRecordActive indicates that the DNS record has been created.
	NodeDNSRecordActive NodeDNSRecordState = "active"
	// NodeDNSRecordInProgress indicates an DNS record action in progress.
	NodeDNSRecordInProgress NodeDNSRecordState = "in-progress"
	// NodeDNSRecordError indicates the last  DNS record action failed.
	NodeDNSRecordError NodeDNSRecordState = "error"
)

const (
	IPv4ReservedIPAnnotation = "flipop.digitalocean.com/ipv4-reserved-ip"
)

// FloatingIPPoolSpec defines the desired state of FloatingIPPool.
type FloatingIPPoolSpec struct {
	// IPs is a list of floating IP addresses for assignment. IPs may be omitted or incomplete if
	// DesiredIPs is provided.
	IPs []string `json:"ips"`

	// DesiredIPs specifies the total number of IPs which should be available for assignment. If
	// DesiredIPs is 0 or omitted, all IPs in the IPs field will be used.
	DesiredIPs int `json:"desiredIPs,omitempty"`

	// BaseProvider describes the provider hosting the specified IPs. It's assumed all matching nodes
	// are associated with the specified provider.
	Provider string `json:"provider,omitempty"`

	// Region describes the region associated with the specified IPs. It's assumed all matching
	// nodes are associated with the specified region.
	Region string `json:"region,omitempty"`

	// Match describes the set of nodes to assign IPs.
	Match Match `json:"match"`

	// DNS describes a DNS record which should point to the floating IPs.
	DNSRecordSet *DNSRecordSet `json:"dnsRecordSet,omitempty"`

	// AssignmentCoolOffSeconds sets a delay between IP assignments. This functionality can be used
	// to prevent rapid reshuffling and reduces the chances/impact of concurrent assignments.
	AssignmentCoolOffSeconds float64 `json:"assignmentCoolOffSeconds,omitempty"`
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

// NodeDNSRecordState describes the condition of a NodeDNSRecordSet.
type NodeDNSRecordState string

// IPStatus describes the mapping between IPs and the matching
// resources responsible for their attachment.
type IPStatus struct {
	State      IPState `json:"state"`
	NodeName   string  `json:"nodeName,omitempty"`
	ProviderID string  `json:"providerID,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// DNSRecordSet describes parameters for creating/updating a DNS record set.
type DNSRecordSet struct {
	Provider   string `json:"provider"`
	Zone       string `json:"zone"`
	RecordName string `json:"recordName"`
	TTL        int    `json:"ttl"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +resource:path=floatingippools

// FloatingIPPool defines a schema describing a desired mapping of floating IPs to nodes.
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

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +resource:path=nodednsrecordset

// NodeDNSRecordSet defines a schema for updating a DNS record set to target matching nodes.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NodeDNSRecordSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeDNSRecordSetSpec   `json:"spec,omitempty"`
	Status NodeDNSRecordSetStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +resource:path=nodednsrecordset

// NodeDNSRecordSetList contains a list of NodeDNSRecordSet
type NodeDNSRecordSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeDNSRecordSet `json:"items"`
}

// NodeDNSRecordSetStatus defines the observed state of NodeDNSRecordSet.
type NodeDNSRecordSetStatus struct {
	Error string             `json:"error,omitempty"`
	State NodeDNSRecordState `json:"state"`
}

// NodeDNSRecordSetSpec defines the desired state of NodeDNSRecordSet.
type NodeDNSRecordSetSpec struct {
	// Match describes the set of nodes to assign DNS entries.
	Match Match `json:"match"`

	// DNS describes a DNS record which should point to matching nodes.
	DNSRecordSet DNSRecordSet `json:"dnsRecordSet"`

	// AddressType defines which node IP to add (ex. ExternalIP or InternalIP), currently hostname
	// and IPv6 address types are not supported. Defaults to ExternalIP.
	AddressType corev1.NodeAddressType `json:"addressType"`
}
