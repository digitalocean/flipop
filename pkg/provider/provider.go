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
	"fmt"
	"runtime"

	"github.com/digitalocean/flipop/pkg/version"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

var (
	// ErrNotFound wraps provider specific not found errors.
	ErrNotFound = NewRetryError(errors.New("not found"), RetrySlow)

	// ErrInProgress is returned if the action is in-progress, but otherwise unerrored.
	ErrInProgress = NewRetryError(errors.New("action in progress"), RetryFast)

	// ErrNodeInUse is returned when the action cannot be completed because the IP already
	// has an IP.
	ErrNodeInUse = NewRetryError(errors.New("node in use"), RetrySlow)

	errNoCredentials = errors.New("no credentials found")
)

const (
	dnsRecordTypeA = "A"

	metricNamespace = "flipop"

	metricSubsystem = "providers"
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

	// RecordNameAndZoneToFQDN translates a zone+recordName into a fully-qualified domain name.
	RecordNameAndZoneToFQDN(zone, recordName string) string
}

// Registry holds active providers and the material to initialize them.
type Registry struct {
	providers      map[string]BaseProvider
	metrics        *metrics
	log            logrus.FieldLogger
	keepLastRecord bool
}

// NewRegistry creates a new registry with the provided options.
func NewRegistry(opts ...RegistryOption) *Registry {
	r := &Registry{
		providers: make(map[string]BaseProvider),
		metrics:   initMetrics(),
	}

	for _, o := range opts {
		o(r)
	}
	return r
}

// Get returns the named provider.
func (r *Registry) Get(name string) BaseProvider {
	return r.providers[name]
}

// Register adds a provider to the registry.
func (r *Registry) Register(name string, p BaseProvider) {
	r.providers[name] = p
}

// Init attempts to initialize all registries with credentials loaded from the environment.
func (r *Registry) Init() error {
	err := r.RegisterDigitalOcean()
	if err != nil && err != errNoCredentials {
		return fmt.Errorf("initializing %s provider: %w", DigitalOcean, err)
	}
	err = r.RegisterCloudflare()
	if err != nil && err != errNoCredentials {
		return fmt.Errorf("initializing %s provider: %w", Cloudflare, err)
	}
	if len(r.providers) == 0 {
		return errors.New("no providers initialized; set DIGITALOCEAN_ACCESS_TOKEN")
	}
	return nil
}

// Collect implements prometheus.Collector
func (r *Registry) Collect(ch chan<- prometheus.Metric) {
	r.metrics.callLatency.Collect(ch)
	r.metrics.callsTotal.Collect(ch)
}

// Describe implements prometheus.Collector
func (r *Registry) Describe(ch chan<- *prometheus.Desc) {
	r.metrics.callLatency.Describe(ch)
	r.metrics.callsTotal.Describe(ch)
}

// RegistryOption describes an option function for Registry.
type RegistryOption func(*Registry)

// WithLogger sets a logger on the registry which can be used when initializing providers.
func WithLogger(log logrus.FieldLogger) RegistryOption {
	return func(r *Registry) {
		r.log = log
	}
}

// WithKeepLastDNSRecord sets the flag for keeping the last DNS record for a record name.
// If all records for a record name are deleted we could get NXDOMAIN with possibly a very long TTL.
// This may be undesireable.
// Setting this flag to true will attempt to keep the last DNS record even though it may
// point to an unhealthy IP.
func WithKeepLastDNSRecord(v bool) RegistryOption {
	return func(r *Registry) {
		r.keepLastRecord = v
	}
}

// WithProvider initializes the registry with the specified provider.
func WithProvider(p BaseProvider) RegistryOption {
	return func(r *Registry) {
		r.providers[p.GetProviderName()] = p
	}
}

func defaultUserAgent() string {
	return fmt.Sprintf("flipop/%s (%s %s)", version.Version, runtime.GOOS, runtime.GOARCH)
}
