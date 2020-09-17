package provider

import (
	"context"
)

// Mock is the provider name for the MockProvider.
const Mock = "mock"

// MockProvider implements the full provider interface for testing.
type MockProvider struct {
	*MockIPProvider
	*MockDNSProvider
}

// GetProviderName returns an identifier for the provider which can be used in resources.
func (m *MockProvider) GetProviderName() string {
	return Mock
}

// MockIPProvider implements the IPProvider interface for testing.
type MockIPProvider struct {
	IPToProviderIDFunc func(ctx context.Context, ip string) (string, error)
	AssignIPFunc       func(ctx context.Context, ip, providerID string) error
	NodeToIPFunc       func(ctx context.Context, providerID string) (string, error)
	CreateIPFunc       func(ctx context.Context, region string) (string, error)
}

// IPToProviderID loads the current assignment (as Kubernetes listed in Kubernetes core v1
// NodeSpec.ProviderID for a floating IP.
func (m *MockIPProvider) IPToProviderID(ctx context.Context, ip string) (string, error) {
	return m.IPToProviderIDFunc(ctx, ip)
}

// AssignIP assigns a floating IP to the specified node.
func (m *MockIPProvider) AssignIP(ctx context.Context, ip, providerID string) error {
	return m.AssignIPFunc(ctx, ip, providerID)
}

// NodeToIP attempts to find any floating IPs bound to the specified node.
func (m *MockIPProvider) NodeToIP(ctx context.Context, providerID string) (string, error) {
	return m.NodeToIPFunc(ctx, providerID)
}

// CreateIP creates a new floating IP.
func (m *MockIPProvider) CreateIP(ctx context.Context, region string) (string, error) {
	return m.CreateIPFunc(ctx, region)
}

// GetProviderName returns an identifier for the provider which can be used in resources.
func (m *MockIPProvider) GetProviderName() string {
	return Mock
}

// MockDNSProvider implements the DNSProvider interface for testing.
type MockDNSProvider struct {
	EnsureDNSARecordSetFunc func(ctx context.Context, zone, recordName string, ips []string, ttl int) error
}

// GetProviderName returns an identifier for the provider which can be used in resources.
func (m *MockDNSProvider) GetProviderName() string {
	return Mock
}

// EnsureDNSARecordSet ensures that the record set w/ name `recordName` contains all IPs listed in `ips`
// and no others.
func (m *MockDNSProvider) EnsureDNSARecordSet(ctx context.Context, zone, recordName string, ips []string, ttl int) error {
	return m.EnsureDNSARecordSetFunc(ctx, zone, recordName, ips, ttl)
}
