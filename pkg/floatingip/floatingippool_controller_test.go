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

package floatingip

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	errors2 "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/uuid"

	kt "github.com/digitalocean/flipop/pkg/k8stest"
	"github.com/digitalocean/flipop/pkg/log"
	"github.com/digitalocean/flipop/pkg/provider"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	kubeCSFake "k8s.io/client-go/kubernetes/fake"

	flipCSFake "github.com/digitalocean/flipop/pkg/apis/flipop/generated/clientset/versioned/fake"
	flipopv1alpha1 "github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
)

// These tests try to approximate an end-to-end workflow.  The ipController and matchController
// also have their own more comprehensive tests.
func TestFloatingIPPoolUpdateK8s(t *testing.T) {
	tcs := []struct {
		name                  string
		objs                  []metav1.Object
		manip                 func(*flipopv1alpha1.FloatingIPPool, *Controller)
		initialIPAssignment   map[string]string
		createIPs             []string
		expectError           string
		expectAssignableNodes int
		expectAssignIPCalls   int
		expectIPState         map[flipopv1alpha1.IPState]int
		expectIPAssignment    map[string]string // expect a specific node to have a specific ip
		expectSetDNSCalls     int
		expectMetrics         string
	}{
		{
			name: "happy path",
			objs: []metav1.Object{
				kt.MakeNode("rio-grande", "mock://1", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)),
				kt.MakeNode("ganges", "mock://2"), // should be ignored
				kt.MakeNode("orinoco", "mock://3", // should also be ignored because of taint.
					kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels), kt.SetTaints(kt.NoSchedule)),
				kt.MakePod("benjamin-sisko", "rio-grande",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
				kt.MakePod("worf", "orinoco",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
			},
			expectIPState: map[flipopv1alpha1.IPState]int{
				flipopv1alpha1.IPStateActive:     1,
				flipopv1alpha1.IPStateUnassigned: 1,
			},
			expectAssignIPCalls: 1,
			expectSetDNSCalls:   1,
			expectMetrics: `flipop_floatingippoolcontroller_ip_assignments{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",provider="mock"} 1
flipop_floatingippoolcontroller_ip_assignments{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",provider="mock"} 0
flipop_floatingippoolcontroller_ip_node{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",node="rio-grande",provider="mock",provider_id="mock://1"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",provider="mock",state="active"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",provider="mock",state="unassigned"} 1
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="assigned"} 1
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="available"} 0
`,
		},
		{
			name: "happy path with other DNS provider",
			objs: []metav1.Object{
				kt.MakeNode("rio-grande", "mock://1", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)),
				kt.MakePod("benjamin-sisko", "rio-grande",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
				kt.MakePod("worf", "orinoco",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
			},
			expectIPState: map[flipopv1alpha1.IPState]int{
				flipopv1alpha1.IPStateActive:     1,
				flipopv1alpha1.IPStateUnassigned: 1,
			},
			expectAssignIPCalls: 1,
			expectSetDNSCalls:   1,
			manip: func(f *flipopv1alpha1.FloatingIPPool, c *Controller) {
				f.Spec.DNSRecordSet.Provider = "other"
				c.providers.Register("other", c.providers.Get(provider.Mock).(*provider.MockProvider).MockDNSProvider)
				c.providers.Register(provider.Mock, c.providers.Get(provider.Mock).(*provider.MockProvider).MockIPProvider)
			},
			expectMetrics: `flipop_floatingippoolcontroller_ip_assignments{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",provider="mock"} 1
flipop_floatingippoolcontroller_ip_assignments{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",provider="mock"} 0
flipop_floatingippoolcontroller_ip_node{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",node="rio-grande",provider="mock",provider_id="mock://1"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",provider="mock",state="active"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",provider="mock",state="unassigned"} 1
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="assigned"} 1
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="available"} 0
`,
		},
		{
			name: "create new ips",
			objs: []metav1.Object{
				kt.MakeNode("rio-grande", "mock://1", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)),
				kt.MakePod("benjamin-sisko", "rio-grande",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
			},
			createIPs: []string{"10.0.1.1", "10.0.2.2"},
			manip: func(f *flipopv1alpha1.FloatingIPPool, c *Controller) {
				f.Spec.IPs = nil
				f.Spec.DesiredIPs = 2
			},
			expectIPState: map[flipopv1alpha1.IPState]int{
				flipopv1alpha1.IPStateActive:     1,
				flipopv1alpha1.IPStateUnassigned: 1,
			},
			expectAssignIPCalls: 1,
			expectSetDNSCalls:   1,
			expectMetrics: `flipop_floatingippoolcontroller_ip_assignments{dns="deep-space-nine.example.com",ip="10.0.1.1",name="deep-space-nine",namespace="",provider="mock"} 0
flipop_floatingippoolcontroller_ip_assignments{dns="deep-space-nine.example.com",ip="10.0.2.2",name="deep-space-nine",namespace="",provider="mock"} 1
flipop_floatingippoolcontroller_ip_node{dns="deep-space-nine.example.com",ip="10.0.2.2",name="deep-space-nine",namespace="",node="rio-grande",provider="mock",provider_id="mock://1"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="10.0.1.1",name="deep-space-nine",namespace="",provider="mock",state="unassigned"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="10.0.2.2",name="deep-space-nine",namespace="",provider="mock",state="active"} 1
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="assigned"} 1
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="available"} 0
`,
		},
		{
			name: "already has ip",
			objs: []metav1.Object{
				kt.MakeNode("rio-grande", "mock://1", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)),
				kt.MakeNode("ganges", "mock://2"), // should be ignored
				kt.MakeNode("orinoco", "mock://3", // should also be ignored because of taint.
					kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels), kt.SetTaints(kt.NoSchedule)),
				kt.MakePod("benjamin-sisko", "rio-grande",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
				kt.MakePod("worf", "orinoco",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
			},
			initialIPAssignment: map[string]string{
				"172.16.2.2": "mock://1",
			},
			expectIPAssignment: map[string]string{
				"172.16.2.2": "mock://1",
			},
			expectIPState: map[flipopv1alpha1.IPState]int{
				flipopv1alpha1.IPStateActive:     1,
				flipopv1alpha1.IPStateUnassigned: 1,
			},
			expectAssignIPCalls: 0,
			expectSetDNSCalls:   1,
			expectMetrics: `flipop_floatingippoolcontroller_ip_node{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",node="rio-grande",provider="mock",provider_id="mock://1"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",provider="mock",state="active"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",provider="mock",state="unassigned"} 1
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="assigned"} 1
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="available"} 0
`,
		},
		{
			name: "no node selector",
			objs: []metav1.Object{
				kt.MakeNode("rio-grande", "mock://1", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)),
				kt.MakeNode("ganges", "mock://2", kt.MarkReady),
				kt.MakePod("benjamin-sisko", "rio-grande",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
				kt.MakePod("worf", "ganges",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
			},
			manip: func(f *flipopv1alpha1.FloatingIPPool, c *Controller) {
				f.Spec.Match.NodeLabel = ""
			},
			expectIPState: map[flipopv1alpha1.IPState]int{
				flipopv1alpha1.IPStateActive: 2,
			},
			expectAssignIPCalls: 2,
			expectSetDNSCalls:   1,
			expectMetrics: `flipop_floatingippoolcontroller_ip_assignments{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",provider="mock"} 1
flipop_floatingippoolcontroller_ip_assignments{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",provider="mock"} 1
flipop_floatingippoolcontroller_ip_node{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",node="ganges",provider="mock",provider_id="mock://2"} 1
flipop_floatingippoolcontroller_ip_node{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",node="rio-grande",provider="mock",provider_id="mock://1"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",provider="mock",state="active"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",provider="mock",state="active"} 1
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="assigned"} 2
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="available"} 0
`,
		},
		{
			name: "bad pod matches",
			objs: []metav1.Object{
				kt.MakeNode("rio-grande", "mock://1", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)),
				kt.MakeNode("ganges", "mock://2", kt.MarkReady), // should be ignored
				kt.MakePod("odo", "rio-grande", // wrong namespace
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("bajoran"), kt.SetLabels(kt.MatchingPodLabels)),
				kt.MakePod("jadzia-dax", "rio-grande", // wrong-labels
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet")),
				kt.MakePod("nog", "rio-grande", // not ready
					kt.MarkRunning, kt.SetNamespace("star-fleet")),
				kt.MakePod("julian-bashir", "rio-grande", // not running (pending)
					kt.MarkReady, kt.SetNamespace("star-fleet")),
				kt.MakePod("miles-obrien", "ganges", // wrong node
					kt.MarkReady, kt.SetNamespace("star-fleet")),
			},
			expectIPState: map[flipopv1alpha1.IPState]int{
				flipopv1alpha1.IPStateUnassigned: 2,
			},
			expectSetDNSCalls: 0,
			expectMetrics: `flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",provider="mock",state="unassigned"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",provider="mock",state="unassigned"} 1
`,
		},
		{
			name: "no pod constraints",
			objs: []metav1.Object{
				kt.MakeNode("rio-grande", "mock://1", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)),
				kt.MakeNode("ganges", "mock://2"), // should be ignored, not ready.
				kt.MakeNode("orinoco", "mock://3",
					kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels), kt.SetTaints(kt.NoSchedule)),
			},
			manip: func(f *flipopv1alpha1.FloatingIPPool, c *Controller) {
				f.Spec.Match.PodNamespace = ""
				f.Spec.Match.PodLabel = ""
			},
			expectIPState: map[flipopv1alpha1.IPState]int{
				flipopv1alpha1.IPStateActive:     1,
				flipopv1alpha1.IPStateUnassigned: 1,
			},
			expectAssignIPCalls: 1,
			expectSetDNSCalls:   1,
			expectMetrics: `flipop_floatingippoolcontroller_ip_assignments{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",provider="mock"} 1
flipop_floatingippoolcontroller_ip_assignments{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",provider="mock"} 0
flipop_floatingippoolcontroller_ip_node{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",node="rio-grande",provider="mock",provider_id="mock://1"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",provider="mock",state="active"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",provider="mock",state="unassigned"} 1
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="assigned"} 1
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="available"} 0
`,
		},
		{
			name: "IP needs to be reassigned",
			objs: []metav1.Object{
				kt.MakeNode("rio-grande", "mock://1", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)), // match
				kt.MakeNode("ganges", "mock://2"), // should be ignored - labels don't match
				kt.MakeNode("orinoco", "mock://3", // tainted
					kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels), kt.SetTaints(kt.NoSchedule)),
				kt.MakeNode("rubicon", "mock://4", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)),    // match
				kt.MakeNode("shenandoah", "mock://5", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)), // match
			},
			initialIPAssignment: map[string]string{
				"192.168.1.1": "mock://3", // orinoco is tainted
				"172.16.2.2":  "mock://5",
			},
			manip: func(f *flipopv1alpha1.FloatingIPPool, c *Controller) {
				f.Spec.Match.PodNamespace = ""
				f.Spec.Match.PodLabel = ""
				f.Spec.AssignmentCoolOffSeconds = 1.0
			},
			expectIPAssignment: map[string]string{
				// rio-grande will get 192.168.1.1 because if all else is equal, nodematch adds
				// matching nodes sorted by name. 172.16.2.2 should stay w/ shenandoah.
				"172.16.2.2": "mock://5",
			},
			expectAssignableNodes: 1, // We have 3 matching nodes, but only 2 ips, one has to wait.
			expectIPState: map[flipopv1alpha1.IPState]int{
				flipopv1alpha1.IPStateActive: 2,
			},
			expectAssignIPCalls: 1,
			expectSetDNSCalls:   1,
			expectMetrics: `flipop_floatingippoolcontroller_ip_assignments{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",provider="mock"} 0
flipop_floatingippoolcontroller_ip_assignments{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",provider="mock"} 1
flipop_floatingippoolcontroller_ip_node{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",node="shenandoah",provider="mock",provider_id="mock://5"} 1
flipop_floatingippoolcontroller_ip_node{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",node="rio-grande",provider="mock",provider_id="mock://1"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="172.16.2.2",name="deep-space-nine",namespace="",provider="mock",state="active"} 1
flipop_floatingippoolcontroller_ip_state{dns="deep-space-nine.example.com",ip="192.168.1.1",name="deep-space-nine",namespace="",provider="mock",state="active"} 1
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="assigned"} 2
flipop_floatingippoolcontroller_node_status{dns="deep-space-nine.example.com",name="deep-space-nine",namespace="",provider="mock",status="available"} 1
`,
		},
		{
			name: "invalid pod selector",
			objs: []metav1.Object{},
			manip: func(f *flipopv1alpha1.FloatingIPPool, c *Controller) {
				f.Spec.Match.PodLabel = "#invalid#"
			},
			expectError: "Error parsing pod selector: unable to parse requirement: <nil>: " +
				"Invalid value: \"#invalid#\": name part must consist of alphanumeric characters, " +
				"'-', '_' or '.', and must start and end with an alphanumeric character " +
				"(e.g. 'MyName',  or 'my.name',  or '123-abc', regex used for validation is '([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]')",
		},
		{
			name: "invalid node selector",
			objs: []metav1.Object{},
			manip: func(f *flipopv1alpha1.FloatingIPPool, c *Controller) {
				f.Spec.Match.NodeLabel = "#invalid#"
			},
			expectError: "Error parsing node selector: unable to parse requirement: <nil>: " +
				"Invalid value: \"#invalid#\": name part must consist of alphanumeric characters, " +
				"'-', '_' or '.', and must start and end with an alphanumeric character " +
				"(e.g. 'MyName',  or 'my.name',  or '123-abc', regex used for validation is '([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]')",
		},
		{
			name: "unknown provider",
			objs: []metav1.Object{},
			manip: func(f *flipopv1alpha1.FloatingIPPool, c *Controller) {
				f.Spec.Provider = "Ferengi"
			},
			expectError: "unknown provider \"Ferengi\"",
		},
		{
			name: "no ips or desired ips",
			objs: []metav1.Object{},
			manip: func(f *flipopv1alpha1.FloatingIPPool, c *Controller) {
				f.Spec.IPs = nil
				f.Spec.DesiredIPs = 0
			},
			expectError: "ips or desiredIPs must be provided",
		},
		{
			name: "main provider does not support dns; no dns provider given",
			objs: []metav1.Object{},
			manip: func(f *flipopv1alpha1.FloatingIPPool, c *Controller) {
				f.Spec.DNSRecordSet.Provider = ""
				c.providers.Register(provider.Mock, c.providers.Get(provider.Mock).(*provider.MockProvider).MockIPProvider)
			},
			expectError: "FloatingIPPool dns referenced provider without dns capability",
		},
		{
			name: "main provider does not support ip",
			objs: []metav1.Object{},
			manip: func(f *flipopv1alpha1.FloatingIPPool, c *Controller) {
				c.providers.Register(provider.Mock, c.providers.Get(provider.Mock).(*provider.MockProvider).MockDNSProvider)
			},
			expectError: "provider \"mock\" does not provide floating IPs",
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			k8s := makeFloatingIPPool()

			ipAssignment := make(map[string]string)
			for ip, providerIP := range tc.initialIPAssignment {
				ipAssignment[ip] = providerIP
			}

			assignIPCalls := 0

			log := log.NewTestLogger(t)
			var ensureDNSARecordSetCalls int
			c := &Controller{
				kubeCS:   kubeCSFake.NewSimpleClientset(kt.AsRuntimeObjects(tc.objs)...),
				flipopCS: flipCSFake.NewSimpleClientset(k8s),
				// Use assert because provider actions run in a goroutine and testify will miss the
				// panic from require.
				providers: provider.NewRegistry(provider.WithProvider(&provider.MockProvider{
					MockIPProvider: &provider.MockIPProvider{
						IPToProviderIDFunc: func(ctx context.Context, ip string) (string, error) {
							return ipAssignment[ip], nil
						},
						AssignIPFunc: func(ctx context.Context, ip, providerID string) error {
							assignIPCalls++
							ipAssignment[ip] = providerID
							return nil
						},
						CreateIPFunc: func(ctx context.Context, region string) (string, error) {
							assert.GreaterOrEqual(t, len(tc.createIPs), 1, "unexpected CreateIP call")
							ip := tc.createIPs[0]
							tc.createIPs = tc.createIPs[1:]
							return ip, nil
						},
					},
					MockDNSProvider: &provider.MockDNSProvider{
						EnsureDNSARecordSetFunc: func(ctx context.Context, zone, recordName string, ips []string, ttl int) error {
							assert.Equal(t, k8s.Spec.DNSRecordSet.Zone, zone)
							assert.Equal(t, k8s.Spec.DNSRecordSet.RecordName, recordName)
							assert.Equal(t, k8s.Spec.DNSRecordSet.TTL, ttl)
							ensureDNSARecordSetCalls++
							return nil
						},
						RecordNameAndZoneToFQDNFunc: func(zone, recordName string) string {
							return recordName + "." + zone
						},
					},
				},
				)),
				pools: make(map[kubetypes.UID]floatingIPPool),
				ctx:   ctx,
				log:   log,
			}

			if tc.manip != nil {
				tc.manip(k8s, c)
			}

			c.updateOrAdd(k8s)
			if tc.expectError != "" {
				updatedK8s, err := c.flipopCS.FlipopV1alpha1().FloatingIPPools(k8s.Namespace).Get(ctx, k8s.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.NotNil(t, updatedK8s)
				require.Equal(t, tc.expectError, updatedK8s.Status.Error)
				return
			}

			f, ok := c.pools[k8s.GetUID()]
			require.True(t, ok)

			// Watch for status updates.
			w, err := c.flipopCS.FlipopV1alpha1().FloatingIPPools(k8s.Namespace).Watch(ctx, metav1.ListOptions{Watch: true})
			require.NoError(t, err)
			var updatedK8s *flipopv1alpha1.FloatingIPPool
			go func() {
				<-ctx.Done()
				w.Stop() // This will close the ResultChan used below.
			}()
			for e := range w.ResultChan() {
				var ok bool
				updatedK8s, ok = e.Object.(*flipopv1alpha1.FloatingIPPool)
				require.True(t, ok, "unexpected type while watching FloatingIPPools")
				ipState := make(map[flipopv1alpha1.IPState]int)
				for _, status := range updatedK8s.Status.IPs {
					ipState[status.State]++
				}

				if reflect.DeepEqual(ipState, tc.expectIPState) &&
					len(updatedK8s.Status.AssignableNodes) == tc.expectAssignableNodes &&
					assignIPCalls == tc.expectAssignIPCalls {
					t.Logf("ip state looks good %v", updatedK8s.Status)
					w.Stop() // Ok, looks like what we expect.
					break
				}
			}

			f.matchController.Stop()
			f.ipController.stop()
			// synchronously run through the ipController reconcile loop.
			f.ipController.reconcile(ctx)

			updatedK8s, err = c.flipopCS.FlipopV1alpha1().FloatingIPPools(k8s.Namespace).Get(ctx, k8s.Name, metav1.GetOptions{})
			require.NoError(t, err)
			ipState := make(map[flipopv1alpha1.IPState]int)
			for _, status := range updatedK8s.Status.IPs {
				ipState[status.State]++
			}
			require.Equal(t, tc.expectIPState, ipState)
			require.Len(t, updatedK8s.Status.AssignableNodes, tc.expectAssignableNodes)
			require.Equal(t, tc.expectAssignIPCalls, assignIPCalls)
			for ip, providerID := range tc.expectIPAssignment {
				require.Equal(t, providerID, updatedK8s.Status.IPs[ip].ProviderID)
			}
			require.GreaterOrEqual(t,
				ensureDNSARecordSetCalls,
				tc.expectSetDNSCalls,
				"expected at least %d DNS calls, got %d",
				tc.expectSetDNSCalls,
				ensureDNSARecordSetCalls,
			)

			metrics, err := renderMetrics(c)
			require.NoError(t, err)
			assert.Equal(t, tc.expectMetrics, metrics)
		})
	}
}

func makeFloatingIPPool() *flipopv1alpha1.FloatingIPPool {
	return &flipopv1alpha1.FloatingIPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "deep-space-nine",
			UID:  uuid.NewUUID(),
		},
		Spec: flipopv1alpha1.FloatingIPPoolSpec{
			Provider: "mock",
			Region:   "alpha-quadrant",
			Match:    kt.MakeMatch(),
			IPs: []string{
				"192.168.1.1",
				"172.16.2.2",
			},
			DNSRecordSet: &flipopv1alpha1.DNSRecordSet{
				Zone:       "example.com",
				RecordName: "deep-space-nine",
				TTL:        120,
			},
		},
	}
}

type fakeMatchController struct {
	nameToNode map[string]*v1.Node
	err        error
}

func (f *fakeMatchController) GetNodeByName(nodeName string) (*v1.Node, error) {
	if f.err != nil {
		return nil, f.err
	}
	node, ok := f.nameToNode[nodeName]
	if !ok {
		return nil, errors2.NewNotFound(v1.Resource("node"), nodeName)
	}
	return node, nil
}

func TestGetNodeFromControllers(t *testing.T) {
	nodeName := "Galileo"
	node := v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}

	t.Run("node found in pools", func(t *testing.T) {
		controllers := []nodeGetter{
			&fakeMatchController{nameToNode: map[string]*v1.Node{}},
			&fakeMatchController{nameToNode: map[string]*v1.Node{nodeName: &node}},
		}
		result, err := getNodeFromControllers(nodeName, controllers)
		require.NotNil(t, result)
		require.NoError(t, err)
		assert.Equal(t, node, *result)
	})
	t.Run("node not found in pools", func(t *testing.T) {
		controllers := []nodeGetter{
			&fakeMatchController{nameToNode: map[string]*v1.Node{}},
			&fakeMatchController{nameToNode: map[string]*v1.Node{}},
		}
		result, err := getNodeFromControllers(nodeName, controllers)
		require.Nil(t, result)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unable to find node")
	})
	t.Run("Cache Error", func(t *testing.T) {
		controllers := []nodeGetter{
			&fakeMatchController{err: fmt.Errorf("cache exploded")},
		}
		result, err := getNodeFromControllers(nodeName, controllers)
		require.Nil(t, result)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unexpected error")
	})
}

func TestAnnotationUpdater(t *testing.T) {
	ctx := context.Background()
	nodeName := "Galileo"

	makeGetNodeFunc := func(annotationValue map[string]string) getNodeByNameFunc {
		return func(name string) (*v1.Node, error) {
			return &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        nodeName,
					Annotations: annotationValue,
				},
			}, nil
		}
	}

	newController := func(nodeName string, annotations map[string]string) (*Controller, *logrus.Logger, *kubeCSFake.Clientset) {
		t.Helper()
		kube := kubeCSFake.NewClientset()
		_, err := kube.CoreV1().Nodes().Create(ctx, &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:        nodeName,
				Annotations: annotations,
			},
		}, metav1.CreateOptions{})
		require.NoError(t, err)

		logger := log.NewTestLogger(t)
		return &Controller{
			kubeCS: kube,
			log:    logger,
		}, logger, kube
	}

	t.Run("valid IPv4 address writes annotation", func(t *testing.T) {
		ctrl, logger, kube := newController(nodeName, nil)
		updater := ctrl.annotationUpdater(logger, makeGetNodeFunc(nil))
		err := updater(ctx, nodeName, "192.168.1.1")
		require.NoError(t, err)

		updated, err := kube.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "192.168.1.1", updated.Annotations[flipopv1alpha1.IPv4ReservedIPAnnotation])
	})
	t.Run("invalid IP address", func(t *testing.T) {
		ctrl, logger, _ := newController(nodeName, nil)
		updater := ctrl.annotationUpdater(logger, makeGetNodeFunc(nil))
		err := updater(ctx, nodeName, "invalid-ip")
		require.Error(t, err)
	})
	t.Run("empty string removes annotation", func(t *testing.T) {
		annotations := map[string]string{"flipop.digitalocean.com/ipv4-reserved-ip": "192.168.1.1"}
		ctrl, logger, kube := newController(nodeName, annotations)
		updater := ctrl.annotationUpdater(logger, makeGetNodeFunc(annotations))
		err := updater(ctx, nodeName, "")
		require.NoError(t, err)

		updated, err := kube.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		require.NoError(t, err)
		_, exists := updated.Annotations[flipopv1alpha1.IPv4ReservedIPAnnotation]
		assert.False(t, exists, "expected annotation to be removed")
	})
	t.Run("NoOp when new and current annotation values match", func(t *testing.T) {
		const ip = "192.168.1.1"
		annotations := map[string]string{"flipop.digitalocean.com/ipv4-reserved-ip": ip}
		ctrl, logger, kube := newController(nodeName, annotations)
		var patchCalls int
		kube.Fake.PrependReactor("patch", "nodes",
			func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
				patchCalls++
				return false, nil, nil // let the fake continue its normal processing
			})
		err := ctrl.annotationUpdater(logger, makeGetNodeFunc(annotations))(ctx, nodeName, ip)
		require.NoError(t, err)
		assert.Equal(t, 0, patchCalls, "expected no patch when annotation unchanged")
	})
}

func renderMetrics(c prometheus.Collector) (string, error) {
	metricRegistry := prometheus.NewPedanticRegistry()
	if err := metricRegistry.Register(c); err != nil {
		return "", err
	}
	metrics, err := metricRegistry.Gather()
	if err != nil {
		return "", err
	}

	var rendered bytes.Buffer
	encoder := expfmt.NewEncoder(&rendered, expfmt.NewFormat(expfmt.TypeTextPlain))
families:
	for _, f := range metrics {
		var v float64
		for _, m := range f.GetMetric() {
			switch f.GetType() {
			case dto.MetricType_COUNTER:
				v = m.GetCounter().GetValue()
			case dto.MetricType_GAUGE:
				v = m.Gauge.GetValue()
			default:
				return "", errors.New("unknown metric type")
			}
			if v != 0.0 {
				// only encode metric families w/ values to make tests less verbose.
				if err := encoder.Encode(f); err != nil {
					return "", err
				}
				continue families
			}
		}
	}
	scanner := bufio.NewScanner(&rendered)
	var out string
	for scanner.Scan() {
		l := scanner.Text()
		if strings.HasPrefix(l, "#") {
			continue
		}
		out += l + "\n"
	}
	return out, scanner.Err()
}
