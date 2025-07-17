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

package nodematch

import (
	"context"
	"testing"
	"time"

	kt "github.com/digitalocean/flipop/pkg/k8stest"
	"github.com/digitalocean/flipop/pkg/log"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	flipopv1alpha1 "github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
)

type mockNodeEnableDisabler struct {
	nodes map[string]*corev1.Node
}

func (mned *mockNodeEnableDisabler) EnableNodes(ctx context.Context, nodes ...*corev1.Node) {
	for _, n := range nodes {
		mned.nodes[n.Name] = n
	}
}

func (mned *mockNodeEnableDisabler) DisableNodes(ctx context.Context, nodes ...*corev1.Node) {
	for _, n := range nodes {
		delete(mned.nodes, n.Name)
	}
}

func (mned *mockNodeEnableDisabler) names() []string {
	var out []string
	for n := range mned.nodes {
		out = append(out, n)
	}
	return out
}

func TestControllerIsNodeMatch(t *testing.T) {
	tcs := []struct {
		name        string
		node        *corev1.Node
		match       *flipopv1alpha1.Match
		expectMatch bool
	}{
		{
			name: "full match",
			node: kt.MakeNode("", "",
				kt.SetLabels(kt.MatchingNodeLabels),
				kt.SetTaints([]corev1.Taint{
					corev1.Taint{Key: "shields", Value: "down", Effect: corev1.TaintEffectNoExecute},
				}),
				kt.MarkReady,
			),
			match: &flipopv1alpha1.Match{
				NodeLabel:   "system=bajor",
				Tolerations: kt.PodTolerations,
			},
			expectMatch: true,
		},
		{
			name: "bad taint",
			node: kt.MakeNode("", "",
				kt.SetTaints([]corev1.Taint{
					corev1.Taint{Key: "shields", Value: "up", Effect: corev1.TaintEffectNoExecute},
				}),
				kt.MarkReady,
			),
			match: &flipopv1alpha1.Match{
				Tolerations: kt.PodTolerations,
			},
		},
		{
			name: "bad label",
			node: kt.MakeNode("", "",
				kt.SetLabels(kt.MatchingNodeLabels),
				kt.MarkReady,
			),
			match: &flipopv1alpha1.Match{
				NodeLabel: "system=klingon",
			},
		},
		{
			name:        "all nodes",
			node:        kt.MakeNode("", "", kt.MarkReady),
			match:       &flipopv1alpha1.Match{},
			expectMatch: true,
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			m := &Controller{log: log.NewTestLogger(t)}
			m.SetCriteria(tc.match)
			m.primed = true
			n := &node{
				k8sNode:      tc.node,
				matchingPods: make(map[string]*corev1.Pod),
			}
			require.Equal(t, tc.expectMatch, m.isNodeMatch(n))
		})
	}
}

func TestControllerUpdateNode(t *testing.T) {
	tcs := []struct {
		name                string
		pods                []*corev1.Pod
		initialIPAssignment map[string]string
		updates             []*corev1.Node
		manip               func(*flipopv1alpha1.Match)
		expectEnabledNodes  []string
	}{
		{
			name: "initial update ready",
			pods: []*corev1.Pod{
				kt.MakePod("benjamin-sisko", "rio-grande",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
			},
			updates: []*corev1.Node{
				kt.MakeNode("rio-grande", "mock://1", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)),
			},
			expectEnabledNodes: []string{"rio-grande"},
		},
		{
			name: "initial update not-ready",
			pods: []*corev1.Pod{
				kt.MakePod("benjamin-sisko", "rio-grande",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
			},
			updates: []*corev1.Node{
				kt.MakeNode("rio-grande", "mock://1", kt.SetLabels(kt.MatchingNodeLabels)),
			},
			expectEnabledNodes: []string{},
		},
		{
			name: "initial update no pod match",
			updates: []*corev1.Node{
				kt.MakeNode("rio-grande", "mock://1", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)),
			},
			manip: func(m *flipopv1alpha1.Match) {
				m.PodNamespace = ""
				m.PodLabel = ""
			},
			expectEnabledNodes: []string{"rio-grande"},
		},
		{
			name: "update from not-ready to ready",
			pods: []*corev1.Pod{
				kt.MakePod("benjamin-sisko", "rio-grande",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
				kt.MakePod("worf", "orinoco",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
			},
			updates: []*corev1.Node{
				kt.MakeNode("rio-grande", "mock://1", kt.SetLabels(kt.MatchingNodeLabels)), // not yet ready
				kt.MakeNode("rio-grande", "mock://1", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)),
			},
			expectEnabledNodes: []string{"rio-grande"},
		},
		{
			name: "update from ready to not-ready",
			pods: []*corev1.Pod{
				kt.MakePod("benjamin-sisko", "rio-grande",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
				kt.MakePod("worf", "orinoco",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
			},
			updates: []*corev1.Node{
				kt.MakeNode("rio-grande", "mock://1", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)),
				kt.MakeNode("rio-grande", "mock://1", kt.SetLabels(kt.MatchingNodeLabels)), // not ready
			},
			expectEnabledNodes: []string{},
		},
	}
	for _, tc := range tcs {
		tc := tc

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()

		t.Run(tc.name, func(t *testing.T) {
			m := NewController(log.NewTestLogger(t), nil, nil)
			k8s := kt.MakeMatch()
			if tc.manip != nil {
				tc.manip(&k8s)
			}
			m.SetCriteria(&k8s)
			m.primed = true
			nMock := &mockNodeEnableDisabler{nodes: make(map[string]*corev1.Node)}
			m.action = nMock

			m.podIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
				podNodeNameIndexerName: podNodeNameIndexer,
			})
			for _, p := range tc.pods {
				m.podIndexer.Add(p)
			}

			for _, n := range tc.updates {
				err := m.updateNode(ctx, n)
				require.NoError(t, err)
			}
			require.ElementsMatch(t, tc.expectEnabledNodes, nMock.names())

		})
	}
}

func TestControllerUpdatePod(t *testing.T) {
	tcs := []struct {
		name                string
		initialIPAssignment map[string]string
		updates             []*corev1.Pod
		expectEnabledNodes  []string
	}{
		{
			name: "pod makes node assignable",
			updates: []*corev1.Pod{
				kt.MakePod("benjamin-sisko", "rio-grande",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
			},
			expectEnabledNodes: []string{"rio-grande"},
		},
		{
			name: "pod not-ready causes node to no longer match",
			updates: []*corev1.Pod{
				kt.MakePod("benjamin-sisko", "rio-grande",
					kt.MarkReady, kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
				kt.MakePod("benjamin-sisko", "rio-grande",
					kt.MarkRunning, kt.SetNamespace("star-fleet"), kt.SetLabels(kt.MatchingPodLabels)),
			},
			expectEnabledNodes: []string{},
		},
	}
	for _, tc := range tcs {
		tc := tc

		ctx := context.Background()

		t.Run(tc.name, func(t *testing.T) {
			m := NewController(log.NewTestLogger(t), nil, nil)
			k8s := kt.MakeMatch()
			m.SetCriteria(&k8s)
			m.primed = true
			nMock := &mockNodeEnableDisabler{nodes: make(map[string]*corev1.Node)}
			m.action = nMock

			m.podIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
				podNodeNameIndexerName: podNodeNameIndexer, // Necessary for updateNode used in setup
			})

			err := m.updateNode(ctx, kt.MakeNode("rio-grande", "mock://1", kt.MarkReady, kt.SetLabels(kt.MatchingNodeLabels)))
			require.NoError(t, err)

			for _, p := range tc.updates {
				m.updatePod(p)
			}

			require.ElementsMatch(t, tc.expectEnabledNodes, nMock.names())
		})
	}
}
