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

package nodedns

import (
	"context"
	"errors"
	"testing"
	"time"

	kt "github.com/digitalocean/flipop/pkg/k8stest"
	"github.com/digitalocean/flipop/pkg/log"
	"github.com/digitalocean/flipop/pkg/provider"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	kubeCSFake "k8s.io/client-go/kubernetes/fake"

	flipCSFake "github.com/digitalocean/flipop/pkg/apis/flipop/generated/clientset/versioned/fake"
	flipopv1alpha1 "github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
)

var nodeLabels = labels.Set(map[string]string{"system": "wolf359"})

func TestNodeDNSRecordSetController(t *testing.T) {
	nodeDNS := &flipopv1alpha1.NodeDNSRecordSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "next-generation",
			Namespace: "default",
		},
		Spec: flipopv1alpha1.NodeDNSRecordSetSpec{
			DNSRecordSet: kt.MakeDNS(),
			// Simple match only cares about nodes. nodematch.Controller is well tested elsewhere.
			Match: flipopv1alpha1.Match{NodeLabel: "system=wolf359"},
		},
	}
	type setDNSCall struct {
		ips        []string
		err        error
		exec       func(c *Controller)
		cancel     bool
		zone       string
		recordName string
	}
	tcs := []struct {
		name             string
		resource         *flipopv1alpha1.NodeDNSRecordSet
		initialObjs      []metav1.Object
		expectSetDNSCall []setDNSCall
		expectError      string
	}{
		{
			name:     "happy path",
			resource: nodeDNS,
			initialObjs: []metav1.Object{
				kt.MakeNode("melbourne", "mock://1", kt.MarkReady, kt.SetLabels(nodeLabels),
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.1")),
				kt.MakeNode("kyushu", "mock://2", kt.SetLabels(nodeLabels),
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.2")), // not ready, not matching
				kt.MakeNode("saratoga", "mock://3", kt.MarkReady, kt.SetLabels(nodeLabels),
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.3")),
			},
			expectSetDNSCall: []setDNSCall{{ips: []string{"10.0.0.1", "10.0.0.3"}, cancel: true}},
		},
		{
			name:     "retry",
			resource: nodeDNS,
			initialObjs: []metav1.Object{
				kt.MakeNode("melbourne", "mock://1", kt.MarkReady, kt.SetLabels(nodeLabels),
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.1")),
			},
			expectSetDNSCall: []setDNSCall{
				{
					ips: []string{"10.0.0.1"},
					err: provider.NewRetryError(errors.New("do retry"), provider.RetrySchedule{100 * time.Millisecond}),
				},
				{ips: []string{"10.0.0.1"}, cancel: true},
			},
		},
		{
			name:     "update error",
			resource: nodeDNS,
			initialObjs: []metav1.Object{
				kt.MakeNode("melbourne", "mock://1", kt.MarkReady, kt.SetLabels(nodeLabels),
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.1")),
			},
			expectSetDNSCall: []setDNSCall{
				{
					ips:    []string{"10.0.0.1"},
					err:    provider.NewRetryError(errors.New("nope"), provider.RetrySchedule{11 * time.Second}),
					cancel: false, // the error match will cancel context
				},
			},
			expectError: "Failed to update DNS: nope",
		},
		{
			name: "invalid",
			resource: &flipopv1alpha1.NodeDNSRecordSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "next-generation",
					Namespace: "default",
				},
				Spec: flipopv1alpha1.NodeDNSRecordSetSpec{
					Match: flipopv1alpha1.Match{NodeLabel: "system=wolf359"},
					DNSRecordSet: flipopv1alpha1.DNSRecordSet{
						Provider: provider.Mock,
					},
				},
			},
			expectError: "invalid dnsRecordSet specification",
		},
		{
			name:     "invalid update",
			resource: nodeDNS,
			initialObjs: []metav1.Object{
				kt.MakeNode("melbourne", "mock://1", kt.MarkReady, kt.SetLabels(nodeLabels),
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.1")),
			},
			expectSetDNSCall: []setDNSCall{
				{
					ips: []string{"10.0.0.1"},
					exec: func(c *Controller) {
						updatedNodeDNS := nodeDNS.DeepCopy()
						updatedNodeDNS.Spec.DNSRecordSet.Provider = "unknown"
						_, err := c.flipopCS.FlipopV1alpha1().NodeDNSRecordSets(nodeDNS.Namespace).Update(context.TODO(), updatedNodeDNS, metav1.UpdateOptions{})
						require.NoError(t, err)
					},
				},
			},
			expectError: `unknown provider "unknown"`,
		},
		{
			name:     "node no-longer matches",
			resource: nodeDNS,
			initialObjs: []metav1.Object{
				kt.MakeNode("melbourne", "mock://1", kt.MarkReady, kt.SetLabels(nodeLabels),
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.1")),
				kt.MakeNode("saratoga", "mock://3", kt.MarkReady, kt.SetLabels(nodeLabels),
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.3")),
			},
			expectSetDNSCall: []setDNSCall{
				{ // Initial sync is complete, delete a node and watch for update.
					ips: []string{"10.0.0.1", "10.0.0.3"},
					exec: func(c *Controller) {
						err := c.kubeCS.CoreV1().Nodes().Delete(context.TODO(), "saratoga", metav1.DeleteOptions{})
						require.NoError(t, err)
					},
				},
				{ips: []string{"10.0.0.1"}, cancel: true}},
		},
		{
			name:     "new node matches",
			resource: nodeDNS,
			initialObjs: []metav1.Object{
				kt.MakeNode("melbourne", "mock://1", kt.MarkReady, kt.SetLabels(nodeLabels),
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.1")),
			},
			expectSetDNSCall: []setDNSCall{
				{ // Initial sync is complete, add another node to make sure updates work.
					ips: []string{"10.0.0.1"},
					exec: func(c *Controller) {
						_, err := c.kubeCS.CoreV1().Nodes().Create(context.TODO(),
							kt.MakeNode("saratoga", "mock://3", kt.MarkReady, kt.SetLabels(nodeLabels),
								kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.3")), metav1.CreateOptions{})
						require.NoError(t, err)
					},
				},
				{ips: []string{"10.0.0.1", "10.0.0.3"}, cancel: true}},
		},
		{
			name:     "match updated",
			resource: nodeDNS,
			initialObjs: []metav1.Object{
				kt.MakeNode("melbourne", "mock://1", kt.MarkReady, kt.SetLabels(nodeLabels),
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.1")),
				// kyushu doesn't have labels, won't initially match, but will after update.
				kt.MakeNode("kyushu", "mock://2", kt.MarkReady,
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.2")),
				kt.MakeNode("saratoga", "mock://3", kt.MarkReady, kt.SetLabels(nodeLabels),
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.3")),
			},
			expectSetDNSCall: []setDNSCall{
				{
					ips: []string{"10.0.0.1", "10.0.0.3"},
					exec: func(c *Controller) {
						updatedNodeDNS := nodeDNS.DeepCopy()
						updatedNodeDNS.Spec.Match.NodeLabel = ""
						_, err := c.flipopCS.FlipopV1alpha1().NodeDNSRecordSets(nodeDNS.Namespace).Update(
							context.TODO(), updatedNodeDNS, metav1.UpdateOptions{})
						require.NoError(t, err)
					},
				},
				{ips: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}, cancel: true},
			},
		},
		{
			name:     "target updated",
			resource: nodeDNS,
			initialObjs: []metav1.Object{
				kt.MakeNode("melbourne", "mock://1", kt.MarkReady, kt.SetLabels(nodeLabels),
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.1")),
				kt.MakeNode("saratoga", "mock://3", kt.MarkReady, kt.SetLabels(nodeLabels),
					kt.SetNodeAddress(corev1.NodeExternalIP, "10.0.0.3")),
			},
			expectSetDNSCall: []setDNSCall{
				{
					ips: []string{"10.0.0.1", "10.0.0.3"},
					exec: func(c *Controller) {
						updatedNodeDNS := nodeDNS.DeepCopy()
						updatedNodeDNS.Spec.DNSRecordSet.RecordName = "ingress"
						updatedNodeDNS.Spec.DNSRecordSet.Zone = "argolis.cluster"
						_, err := c.flipopCS.FlipopV1alpha1().NodeDNSRecordSets(nodeDNS.Namespace).Update(
							context.TODO(), updatedNodeDNS, metav1.UpdateOptions{})
						require.NoError(t, err)
					},
				},
				{
					ips:        []string{"10.0.0.1", "10.0.0.3"},
					cancel:     true,
					recordName: "ingress",
					zone:       "argolis.cluster",
				},
			},
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			log := log.NewTestLogger(t)
			c := &Controller{
				kubeCS:   kubeCSFake.NewSimpleClientset(kt.AsRuntimeObjects(tc.initialObjs)...),
				flipopCS: flipCSFake.NewSimpleClientset(tc.resource),
				children: make(map[string]*dnsEnablerDisabler),
				ctx:      ctx,
				log:      log,
			}
			c.providers = map[string]provider.BaseProvider{
				provider.Mock: &provider.MockProvider{MockDNSProvider: &provider.MockDNSProvider{
					EnsureDNSARecordSetFunc: func(ctx context.Context, zone, recordName string, ips []string, ttl int) error {
						require.NotEmpty(t, tc.expectSetDNSCall, "unexpected call to EnsureDNSARecordSet")
						expected := tc.expectSetDNSCall[0]
						tc.expectSetDNSCall = tc.expectSetDNSCall[1:]
						if expected.cancel {
							cancel() // this is the last expected call
						}
						require.ElementsMatch(t, expected.ips, ips)
						if expected.zone != "" {
							require.Equal(t, expected.zone, zone)
						} else {
							require.Equal(t, nodeDNS.Spec.DNSRecordSet.Zone, zone)
						}
						if expected.recordName != "" {
							require.Equal(t, expected.recordName, recordName)
						} else {
							require.Equal(t, nodeDNS.Spec.DNSRecordSet.RecordName, recordName)
						}
						require.Equal(t, nodeDNS.Spec.DNSRecordSet.TTL, ttl)
						if expected.exec != nil {
							expected.exec(c)
						}
						return expected.err
					},
				}},
			}

			if tc.expectError != "" {
				// Watch for the error update, so we know when to stop the test.
				w, err := c.flipopCS.FlipopV1alpha1().NodeDNSRecordSets(nodeDNS.Namespace).Watch(context.TODO(), metav1.ListOptions{Watch: true})
				require.NoError(t, err)
				go func() {
					<-ctx.Done()
					w.Stop() // This will close the ResultChan used below.
				}()
				go func() {
					for e := range w.ResultChan() {
						updatedNodeDNS, ok := e.Object.(*flipopv1alpha1.NodeDNSRecordSet)
						require.True(t, ok, "unexpected type while watching NodeDNSRecordSets")
						if updatedNodeDNS.Status.Error == tc.expectError {
							cancel()
							return
						}
					}
				}()
			}
			c.Run(ctx)

			updatedNodeDNS, err := c.flipopCS.FlipopV1alpha1().NodeDNSRecordSets(nodeDNS.Namespace).Get(context.TODO(), nodeDNS.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.NotNil(t, updatedNodeDNS)
			require.Equal(t, tc.expectError, updatedNodeDNS.Status.Error)
			require.Empty(t, tc.expectSetDNSCall)
		})
	}
}
