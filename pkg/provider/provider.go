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

const (
	dnsRecordTypeA = "A"
)

// BaseProvider describes all providers.
type BaseProvider interface {
	// GetProviderName returns an identifier for the provider which can be used in resources.
	GetProviderName() string
}

// IPProvider defines a platform which offers kubernetes VMs and floating ips.
type IPProvider interface {
	BaseProvider

	// IPToProviderID loads the current assignment (as Kubernetes listed in Kubernetes core v1
	// NodeSpec.ProviderID for a floating IP.
	IPToProviderID(ctx context.Context, ip string) (string, error)

	// AssignIP assigns a floating IP to the specified node.
	AssignIP(ctx context.Context, ip, providerID string) error

	// NodeToIP attempts to find any floating IPs bound to the specified node.
	NodeToIP(ctx context.Context, providerID string) (string, error)

	// CreateIP creates a new floating IP.
	CreateIP(ctx context.Context, region string) (string, error)
}

// DNSProvider defines a service which can register and serve DNS records.
type DNSProvider interface {
	BaseProvider

	// EnsureDNSARecordSet ensures that the record set w/ name `recordName` contains all IPs listed in `ips`
	// and no others.
	EnsureDNSARecordSet(ctx context.Context, zone, recordName string, ips []string, ttl int) error
}
