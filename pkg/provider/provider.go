package provider

import (
	"context"
	"errors"
)

var (
	// ErrNotFound wraps provider specific not found errors.
	ErrNotFound = NewRetryError(errors.New("not found"), RetrySlow)

	// ErrInProgress is returned if the action is in-progress, but otherwise unerrored.
	ErrInProgress = NewRetryError(errors.New("action in progress"), RetryFast)

	// ErrNodeInUse is returned when the action cannot be completed because the IP already
	// has an IP.
	ErrNodeInUse = NewRetryError(errors.New("node in use"), RetrySlow)
)

// Provider defines a platform which offers kubernetes VMs and floating ips.
type Provider interface {
	// IPToProviderID loads the current assignment (as Kubernetes listed in Kubernetes core v1
	// NodeSpec.ProviderID for a floating IP.
	IPToProviderID(ctx context.Context, ip string) (string, error)

	// AssignIP assigns a floating IP to the specified node.
	AssignIP(ctx context.Context, ip, providerID string) error

	// NodeToIP attempts to find any floating IPs bound to the specified node.
	NodeToIP(ctx context.Context, providerID string) (string, error)

	// CreateIP creates a new floating IP.
	CreateIP(ctx context.Context, region string) (string, error)

	// EnsureDNSARecordSet ensures that the record set w/ name `recordName` contains all IPs listed in `ips`
	// and no others.
	EnsureDNSARecordSet(ctx context.Context, zone, recordName string, ips []string, ttl int) error
}
