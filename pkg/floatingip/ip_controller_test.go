package floatingip

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/digitalocean/flipop/pkg/log"
	"github.com/digitalocean/flipop/pkg/provider"
)

func TestIPControllerReconcileDesiredIPs(t *testing.T) {
	type createIPRes struct {
		ip     string
		err    error
		region string
	}
	tcs := []struct {
		name             string
		desiredIPs       int
		existingIPs      []string
		region           string
		responses        []createIPRes
		expectPendingIPs []string
		expectIPRetry    bool
	}{
		{
			name:             "success",
			desiredIPs:       3,
			existingIPs:      []string{"192.168.1.1"},
			responses:        []createIPRes{{ip: "192.168.1.2", region: "earth"}, {ip: "192.168.1.3", region: "earth"}},
			expectPendingIPs: []string{"192.168.1.2", "192.168.1.3"},
		},
		{
			name:          "create fails",
			desiredIPs:    3,
			existingIPs:   []string{"192.168.1.1"},
			responses:     []createIPRes{{err: errors.New("nope"), region: "earth"}},
			expectIPRetry: true,
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			i := &ipController{
				desiredIPs: tc.desiredIPs,
				ips:        tc.existingIPs,
				region:     tc.region,
				provider: &provider.MockProvider{
					CreateIPFunc: func(_ context.Context, region string) (string, error) {
						require.GreaterOrEqual(t, len(tc.responses), 1, "unexpected call to CreateIPFunc")
						require.Equal(t, tc.region, region)
						ip := tc.responses[0].ip
						err := tc.responses[0].err
						tc.responses = tc.responses[1:]
						return ip, err
					},
				},
				ll: log.NewTestLogger(t),
			}
			i.reconcileDesiredIPs(ctx)
			require.ElementsMatch(t, tc.expectPendingIPs, i.pendingIPs)
			require.Equal(t, tc.expectIPRetry, i.nextRetry != time.Time{})
			require.Empty(t, tc.responses) // We should have used all expected responses.
		})
	}
}

func TestIPControllerReconcilePendingIPs(t *testing.T) {
	tcs := []struct {
		name           string
		pendingIPs     []string
		existingIPs    []string
		onNewIPsReturn error
		expectedIPs    []string
	}{}
	for _, tc := range tcs {
		tc := tc
		ctx := context.Background()
		t.Run(tc.name, func(t *testing.T) {

			i := &ipController{
				pendingIPs: tc.pendingIPs,

				onNewIPs: func(ctx context.Context, ips []string) error {
					require.EqualValues(t, tc.expectedIPs, ips)
					return tc.onNewIPsReturn
				},
				ll: logrus.New(),
			}
			copy(i.ips, tc.existingIPs)
			copy(i.pendingIPs, tc.pendingIPs)
			i.reconcilePendingIPs(ctx)
			if tc.onNewIPsReturn == nil {
				require.EqualValues(t, tc.expectedIPs, i.ips)
				require.False(t, i.nextRetry != time.Time{}, "unexpected retry")
			} else {
				require.True(t, i.nextRetry != time.Time{}, "expected retry")
				require.EqualValues(t, tc.existingIPs, i.ips)
				require.EqualValues(t, tc.pendingIPs, i.pendingIPs)
			}
		})
	}
}

func TestIPControllerReconcileIPStatus(t *testing.T) {
	type ipToProviderIDRes struct {
		ip            string
		err           error
		providerID    string
		expectIPRetry bool
	}
	tcs := []struct {
		name                  string
		ips                   []string
		responses             []ipToProviderIDRes
		setup                 func(i *ipController)
		expectProviderIDToIP  map[string]string
		expectIPRetry         bool
		expectAssignableIPs   []string
		expectAssignableNodes []string
	}{
		{
			name:      "new ips",
			ips:       []string{"192.168.1.1", "192.168.1.2"},
			responses: []ipToProviderIDRes{{ip: "192.168.1.1", providerID: "mock://1"}, {ip: "192.168.1.2"}},
			expectProviderIDToIP: map[string]string{
				"mock://1": "192.168.1.1",
			},
			expectAssignableIPs: []string{"192.168.1.1", "192.168.1.2"},
		},
		{
			name:      "provider error",
			ips:       []string{"192.168.1.1"},
			responses: []ipToProviderIDRes{{ip: "192.168.1.1", err: provider.ErrInProgress}},
			expectProviderIDToIP: map[string]string{
				"mock://1": "192.168.1.1",
			},
			setup: func(i *ipController) {
				i.providerIDToIP["mock://1"] = "192.168.1.1"
				i.ipToStatus["192.168.1.1"] = &ipStatus{
					nodeProviderID: "mock://1",
				}
			},
			expectIPRetry: true,
		},
		{
			name:      "ip not found",
			ips:       []string{"192.168.1.1"},
			responses: []ipToProviderIDRes{{ip: "192.168.1.1", err: provider.ErrNotFound}},
			setup: func(i *ipController) {
				i.providerIDToIP["mock://1"] = "192.168.1.1"
				i.ipToStatus["192.168.1.1"] = &ipStatus{
					nodeProviderID: "mock://1",
				}
				i.providerIDToNodeName["mock://1"] = "some-node"
			},
			expectIPRetry:         true,
			expectProviderIDToIP:  map[string]string{},
			expectAssignableIPs:   []string{},
			expectAssignableNodes: []string{"mock://1"},
		},
		{
			name: "provider reports ip reassigned",
			ips:  []string{"192.168.1.1", "172.16.2.2"},
			responses: []ipToProviderIDRes{
				{ip: "192.168.1.1", providerID: "mock://2"},
				// report in-progress for 172.16.2.2 to avoid impacting results.
				{ip: "172.16.2.2", err: provider.ErrInProgress},
			},
			setup: func(i *ipController) {
				i.providerIDToIP["mock://1"] = "192.168.1.1"
				i.providerIDToIP["mock://2"] = "172.16.2.2"
				i.ipToStatus["192.168.1.1"] = &ipStatus{
					nodeProviderID: "mock://1",
				}
				i.ipToStatus["172.16.2.2"] = &ipStatus{
					nodeProviderID: "mock://2",
				}
				i.providerIDToNodeName["mock://1"] = "mock-one"
				i.providerIDToNodeName["mock://2"] = "mock-two"
			},
			expectIPRetry:         true,
			expectProviderIDToIP:  map[string]string{"mock://2": "192.168.1.1"},
			expectAssignableIPs:   []string{"172.16.2.2"},
			expectAssignableNodes: []string{"mock://1"},
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			i := newIPController(logrus.New(), nil, nil)
			i.updateProvider(&provider.MockProvider{
				IPToProviderIDFunc: func(_ context.Context, ip string) (string, error) {
					require.GreaterOrEqual(t, len(tc.responses), 1, "unexpected call to IPToProviderIDFunc")
					require.Equal(t, tc.responses[0].ip, ip)
					providerID := tc.responses[0].providerID
					err := tc.responses[0].err
					tc.responses = tc.responses[1:]
					return providerID, err
				},
			}, "")
			i.ips = tc.ips
			if tc.setup != nil {
				tc.setup(i)
			}
			i.reconcileIPStatus(ctx)

			require.Equal(t, tc.expectProviderIDToIP, i.providerIDToIP)
			for providerID, ip := range tc.expectProviderIDToIP {
				status := i.ipToStatus[ip]
				require.NotNil(t, status)
				require.Equal(t, providerID, status.nodeProviderID)
			}

			require.Equal(t, tc.expectIPRetry, i.nextRetry != time.Time{})
			require.Equal(t, len(tc.expectAssignableIPs), i.assignableIPs.Len())
			for _, ip := range tc.expectAssignableIPs {
				require.True(t, i.assignableIPs.IsSet(ip))
			}
			for _, ip := range tc.expectAssignableIPs {
				require.Contains(t, i.ipToStatus, ip)
			}

			require.Equal(t, len(tc.expectAssignableNodes), i.assignableNodes.Len())
			for _, providerID := range tc.expectAssignableNodes {
				require.True(t, i.assignableNodes.IsSet(providerID))
			}
		})
	}
}

func TestIPControllerReconcileAssignment(t *testing.T) {
	type assignIPRes struct {
		ip         string
		err        error
		providerID string
	}
	tcs := []struct {
		name                  string
		assignableIPs         []string
		assignableNodes       []string
		setup                 func(i *ipController)
		responses             []assignIPRes
		expectProviderIDToIP  map[string]string
		expectIPRetry         bool
		expectAssignableIPs   []string
		expectAssignableNodes []string
		eval                  func(i *ipController)
	}{
		{
			name:                 "no action",
			expectProviderIDToIP: map[string]string{},
		},
		{
			name:                 "success",
			assignableIPs:        []string{"192.168.1.1"},
			assignableNodes:      []string{"mock://1"},
			expectProviderIDToIP: map[string]string{"mock://1": "192.168.1.1"},
			responses:            []assignIPRes{{ip: "192.168.1.1", providerID: "mock://1"}},
			expectIPRetry:        true, // We always retry, because of assign
			setup: func(i *ipController) {
				i.ipToStatus["192.168.1.1"] = &ipStatus{}
			},
			eval: func(i *ipController) {
				require.Equal(t, provider.RetryFast, i.ipToStatus["192.168.1.1"].retrySchedule)
				require.NotContains(t, i.providerIDToRetry, "mock://1")
			},
		},
		{
			name:                 "assignment error",
			assignableIPs:        []string{"192.168.1.1"},
			assignableNodes:      []string{"mock://1"},
			expectProviderIDToIP: map[string]string{"mock://1": "192.168.1.1"},
			responses:            []assignIPRes{{ip: "192.168.1.1", providerID: "mock://1"}},
			expectIPRetry:        true, // We always retry, because of assign
			setup: func(i *ipController) {
				i.ipToStatus["192.168.1.1"] = &ipStatus{}
			},
			eval: func(i *ipController) {
				require.Equal(t, provider.RetrySlow, i.ipToStatus["192.168.1.1"].retrySchedule)
				require.Contains(t, i.providerIDToRetry, "mock://1")
			},
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			i := newIPController(logrus.New(), nil, nil)
			i.updateProvider(&provider.MockProvider{
				AssignIPFunc: func(_ context.Context, ip string, providerID string) error {
					require.GreaterOrEqual(t, len(tc.responses), 1, "unexpected call to AssignIPFunc")
					require.Equal(t, tc.responses[0].ip, ip)
					require.Equal(t, tc.responses[0].providerID, providerID)
					err := tc.responses[0].err
					tc.responses = tc.responses[1:]
					return err
				},
			}, "")
			for _, ip := range tc.assignableIPs {
				i.assignableIPs.Add(ip, true)
			}
			for _, node := range tc.assignableNodes {
				i.assignableNodes.Add(node, true)
			}
			if tc.setup != nil {
				tc.setup(i)
			}
			i.reconcileAssignment(ctx)

			require.Equal(t, tc.expectProviderIDToIP, i.providerIDToIP)
			for providerID, ip := range tc.expectProviderIDToIP {
				status := i.ipToStatus[ip]
				require.NotNil(t, status)
				require.Equal(t, providerID, status.nodeProviderID)
			}

			require.Equal(t, tc.expectIPRetry, i.nextRetry != time.Time{})
			require.Equal(t, len(tc.expectAssignableIPs), i.assignableIPs.Len())
			for _, ip := range tc.expectAssignableIPs {
				require.True(t, i.assignableIPs.IsSet(ip))
			}
			for _, ip := range tc.expectAssignableIPs {
				require.Contains(t, i.ipToStatus, ip)
			}

			require.Equal(t, len(tc.expectAssignableNodes), i.assignableNodes.Len())
			for _, providerID := range tc.expectAssignableNodes {
				require.True(t, i.assignableNodes.IsSet(providerID))
			}
		})
	}
}

func TestIPControllerDisableNodes(t *testing.T) {
	tcs := []struct {
		name               string
		setup              func(i *ipController)
		expectAssignableIP bool
	}{
		{
			name: "node was assignable",
			setup: func(i *ipController) {
				i.assignableNodes.Add("mock://1", true)
				i.providerIDToIP["mock://1"] = ""
				i.providerIDToNodeName["mock://1"] = "hello-world"
			},
		},
		{
			name: "already assigned",
			setup: func(i *ipController) {
				i.providerIDToIP["mock://1"] = "192.168.1.1"
				i.ipToStatus["192.168.1.1"] = &ipStatus{nodeProviderID: "mock://1"}
				i.providerIDToNodeName["mock://1"] = "hello-world"
			},
			expectAssignableIP: true,
		},
		{
			name: "never seen",
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			i := newIPController(logrus.New(), nil, nil)
			if tc.setup != nil {
				tc.setup(i)
			}
			i.DisableNodes(&corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "hello-world"},
				Spec:       corev1.NodeSpec{ProviderID: "mock://1"},
			})
			require.False(t, i.assignableNodes.IsSet("mock://"))
			require.NotContains(t, i.providerIDToNodeName, "mock://1")
			if tc.expectAssignableIP {
				require.True(t, i.assignableIPs.IsSet("192.168.1.1"))
			}
		})
	}
}

func TestIPControllers(t *testing.T) {
	tcs := []struct {
		name             string
		setup            func(i *ipController)
		expectAssignable bool
	}{
		{
			name:             "simple",
			expectAssignable: true,
		},
		{
			name:             "already assignable",
			expectAssignable: true,
			setup: func(i *ipController) {
				i.providerIDToIP["mock://1"] = ""
				i.assignableNodes.Add("mock://1", true)
				i.providerIDToNodeName["mock://1"] = "hello-world"
			},
		},
		{
			name: "already assigned",
			setup: func(i *ipController) {
				i.providerIDToIP["mock://1"] = "192.168.1.1"
				i.ipToStatus["192.168.1.1"] = &ipStatus{nodeProviderID: "mock://1"}
				i.providerIDToNodeName["mock://1"] = "hello-world"
			},
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			i := newIPController(log.NewTestLogger(t), nil, nil)
			if tc.setup != nil {
				tc.setup(i)
			}
			i.EnableNodes(&corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "hello-world"},
				Spec:       corev1.NodeSpec{ProviderID: "mock://1"},
			})
			require.Equal(t, tc.expectAssignable, i.assignableNodes.IsSet("mock://1"))
			require.Equal(t, "hello-world", i.providerIDToNodeName["mock://1"])
		})
	}
}
