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

// Package k8stest provides example objects for testing.
package k8stest

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	flipopv1alpha1 "github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
	"k8s.io/apimachinery/pkg/util/uuid"
)

// PodTolerations is an example kubernetes node Toleration.
var PodTolerations = []corev1.Toleration{
	corev1.Toleration{
		Key:      "shields",
		Value:    "down",
		Operator: corev1.TolerationOpEqual,
		Effect:   corev1.TaintEffectNoExecute,
	},
	corev1.Toleration{
		Key:      "alert",
		Value:    "red",
		Operator: corev1.TolerationOpEqual,
		Effect:   corev1.TaintEffectNoSchedule,
	},
}

// MatchingPodLabels is a set of pod labels which match the selector from MakeMatch.
var MatchingPodLabels = labels.Set(map[string]string{
	"vessel": "runabout",
	"class":  "danube",
})

// MatchingNodeLabels is a set of node labels which match the selector from MakeMatch.
var MatchingNodeLabels = labels.Set(map[string]string{
	"system":   "bajor",
	"quadrant": "alpha",
})

// SetLabels returns a mutator callback which adds the specified labels to an object.
func SetLabels(l labels.Set) func(metav1.Object) metav1.Object {
	return func(o metav1.Object) metav1.Object {
		o.SetLabels(l)
		return o
	}
}

// NoSchedule is a set taints which can be applied to a node to make it unschedulable.
var NoSchedule = []corev1.Taint{
	corev1.Taint{
		Key:    "node.kubernetes.io/unschedulable",
		Effect: corev1.TaintEffectNoSchedule,
	},
}

// SetTaints returns a mutator callback which adds the specified taints to a node.
func SetTaints(t []corev1.Taint) func(metav1.Object) metav1.Object {
	return func(o metav1.Object) metav1.Object {
		n := o.(*corev1.Node)
		n.Spec.Taints = t
		return o
	}
}

// MakePod creates a pod and optionally applies a list of mutators to customize the object.
func MakePod(name, nodeName string, mutators ...func(pod metav1.Object) metav1.Object) *corev1.Pod {
	var p metav1.Object = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: labels.Set(map[string]string{
				"vessel": "starship",
				"class":  "galaxy",
			}),
			Namespace: "star-fleet",
			UID:       uuid.NewUUID(),
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{
				corev1.PodCondition{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}
	for _, f := range mutators {
		p = f(p)
	}
	return p.(*corev1.Pod)
}

// MakeNode creates a node object and applies the specified mutators to customize the object.
func MakeNode(name, providerID string, mutators ...func(node metav1.Object) metav1.Object) *corev1.Node {
	var n metav1.Object = &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: labels.Set(map[string]string{
				"vessel": "starship",
				"class":  "galaxy",
			}),
			UID: uuid.NewUUID(),
		},
		Spec: corev1.NodeSpec{
			ProviderID: providerID,
		},
		Status: corev1.NodeStatus{
			Phase: corev1.NodePending,
			Conditions: []corev1.NodeCondition{
				corev1.NodeCondition{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}
	for _, f := range mutators {
		n = f(n)
	}
	return n.(*corev1.Node)
}

// MarkReady returns a mutator which marks a pod or node as ready.
func MarkReady(o metav1.Object) metav1.Object {
	switch r := o.(type) {
	case *corev1.Node:
		r.Status.Conditions = []corev1.NodeCondition{
			corev1.NodeCondition{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			},
		}
	case *corev1.Pod:
		r.Status.Conditions = []corev1.PodCondition{
			corev1.PodCondition{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			},
		}
	default:
		panic(fmt.Sprintf("unexpected type: %T", r))
	}
	return o
}

// MarkRunning returns a mutator which marks a pod as running.
func MarkRunning(o metav1.Object) metav1.Object {
	pod := o.(*corev1.Pod)
	pod.Status.Phase = corev1.PodRunning
	return pod
}

// MarkDeleting returns a mutator which marks the object as deleting.
func MarkDeleting(o metav1.Object) metav1.Object {
	now := metav1.Now()
	o.SetDeletionTimestamp(&now)
	return o
}

// SetNamespace returns a mutator which sets the namespace of a kubernetes object.
func SetNamespace(ns string) func(o metav1.Object) metav1.Object {
	return func(o metav1.Object) metav1.Object {
		o.SetNamespace(ns)
		return o
	}
}

// AsRuntimeObjects returns a slice of runtime.Objects based upon a slice of metav1.Object.
func AsRuntimeObjects(in []metav1.Object) (out []runtime.Object) {
	for _, m := range in {
		out = append(out, m.(runtime.Object))
	}
	return out
}

// MakeMatch returns flipop match object which selects the pods/nodes specified in MatchingPodLabels,
// and MatchingNodeLabels.
func MakeMatch() flipopv1alpha1.Match {
	return flipopv1alpha1.Match{
		NodeLabel:    "system=bajor",
		PodNamespace: "star-fleet",
		PodLabel:     "vessel=runabout,class=danube",
		Tolerations:  PodTolerations,
	}
}

// MakeDNS returns an example DNSRecordSet.
func MakeDNS() flipopv1alpha1.DNSRecordSet {
	return flipopv1alpha1.DNSRecordSet{
		Zone:       "example.com",
		RecordName: "nodes",
		TTL:        120,
		Provider:   "mock",
	}
}

// SetNodeAddress returns a mutator which adds the specified address to a node.
func SetNodeAddress(t corev1.NodeAddressType, addr string) func(o metav1.Object) metav1.Object {
	return func(o metav1.Object) metav1.Object {
		n := o.(*corev1.Node)
		n.Status.Addresses = append(n.Status.Addresses, corev1.NodeAddress{
			Type:    t,
			Address: addr,
		})
		return n
	}
}
