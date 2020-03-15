package k8stest

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	flipopv1alpha1 "github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
)

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

var MatchingPodLabels = labels.Set(map[string]string{
	"vessel": "runabout",
	"class":  "danube",
})

var MatchingNodeLabels = labels.Set(map[string]string{
	"system":   "bajor",
	"quadrant": "alpha",
})

func SetLabels(l labels.Set) func(metav1.Object) metav1.Object {
	return func(o metav1.Object) metav1.Object {
		o.SetLabels(l)
		return o
	}
}

var NoSchedule = []corev1.Taint{
	corev1.Taint{
		Key:    "node.kubernetes.io/unschedulable",
		Effect: corev1.TaintEffectNoSchedule,
	},
}

func SetTaints(t []corev1.Taint) func(metav1.Object) metav1.Object {
	return func(o metav1.Object) metav1.Object {
		n := o.(*corev1.Node)
		n.Spec.Taints = t
		return o
	}
}

func MakePod(name, nodeName string, manipulations ...func(pod metav1.Object) metav1.Object) *corev1.Pod {
	var p metav1.Object = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: labels.Set(map[string]string{
				"vessel": "starship",
				"class":  "galaxy",
			}),
			Namespace: "star-fleet",
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
	for _, f := range manipulations {
		p = f(p)
	}
	return p.(*corev1.Pod)
}

func MakeNode(name, providerID string, manipulations ...func(node metav1.Object) metav1.Object) *corev1.Node {
	var n metav1.Object = &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: labels.Set(map[string]string{
				"vessel": "starship",
				"class":  "galaxy",
			}),
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
	for _, f := range manipulations {
		n = f(n)
	}
	return n.(*corev1.Node)
}

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

func MarkRunning(o metav1.Object) metav1.Object {
	pod := o.(*corev1.Pod)
	pod.Status.Phase = corev1.PodRunning
	return pod
}

func MarkDeleting(o metav1.Object) metav1.Object {
	now := metav1.Now()
	o.SetDeletionTimestamp(&now)
	return o
}

func SetNamespace(ns string) func(o metav1.Object) metav1.Object {
	return func(o metav1.Object) metav1.Object {
		o.SetNamespace(ns)
		return o
	}
}

func AsRuntimeObjects(in []metav1.Object) (out []runtime.Object) {
	for _, m := range in {
		out = append(out, m.(runtime.Object))
	}
	return out
}

func MakeMatch() flipopv1alpha1.Match {
	return flipopv1alpha1.Match{
		NodeLabel:    "system=bajor",
		PodNamespace: "star-fleet",
		PodLabel:     "vessel=runabout,class=danube",
		Tolerations:  PodTolerations,
	}
}

func MakeDNS() flipopv1alpha1.DNSRecordSet {
	return flipopv1alpha1.DNSRecordSet{
		Zone:       "example.com",
		RecordName: "nodes",
		TTL:        120,
	}
}

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
