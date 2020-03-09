package provider

import (
	"context"
)

// MockProvider provides a Provider for testing purposes.
type MockProvider struct {
	IPToProviderIDFunc func(ctx context.Context, ip string) (string, error)
	AssignIPFunc       func(ctx context.Context, ip, providerID string) error
	NodeToIPFunc       func(ctx context.Context, providerID string) (string, error)
	CreateIPFunc       func(ctx context.Context, region string) (string, error)
}

// IPToProviderID loads the current assignment (as Kubernetes listed in Kubernetes core v1
// NodeSpec.ProviderID for a floating IP.
func (m *MockProvider) IPToProviderID(ctx context.Context, ip string) (string, error) {
	return m.IPToProviderIDFunc(ctx, ip)
}

// AssignIP assigns a floating IP to the specified node.
func (m *MockProvider) AssignIP(ctx context.Context, ip, providerID string) error {
	return m.AssignIPFunc(ctx, ip, providerID)
}

// CreateIP creates a new floating IP.
func (m *MockProvider) NodeToIP(ctx context.Context, providerID string) (string, error) {
	return m.NodeToIPFunc(ctx, providerID)
}

// NodeToIP attempts to find any floating IPs bound to the specified node.
func (m *MockProvider) CreateIP(ctx context.Context, region string) (string, error) {
	return m.CreateIPFunc(ctx, region)
}
