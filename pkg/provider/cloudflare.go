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
	"net"
	"os"

	"github.com/cloudflare/cloudflare-go"
	"github.com/digitalocean/flipop/pkg/metacontext"
	"github.com/sirupsen/logrus"
)

// Cloudflare is the provider identifier used to identify the Cloudflare provider.
const Cloudflare = "cloudflare"

// CloudflareOption defines a create option for the Cloudflare provider.
type CloudflareOption func(c *cloudflareDNS)

// CloudflareWithLog sets a logrus.FieldLogger on the Cloudflare provider.
func CloudflareWithLog(ll logrus.FieldLogger) CloudflareOption {
	return func(c *cloudflareDNS) {
		c.log = ll.WithField("provider", Cloudflare)
	}
}

func cloudflareWithMetrics(m *metrics) CloudflareOption {
	return func(c *cloudflareDNS) {
		c.metrics = m.withProvider(Cloudflare)
	}
}

type cloudflareDNS struct {
	token   string
	api     *cloudflare.API
	log     logrus.FieldLogger
	metrics *metrics
}

// RegisterCloudflare creates the Cloudflare DNS provider and registers it in the registry.
func (r *Registry) RegisterCloudflare(opts ...CloudflareOption) error {
	opts = append([]CloudflareOption{
		CloudflareWithLog(r.log),
		cloudflareWithMetrics(r.metrics),
	}, opts...)
	cf, err := NewCloudflare(opts...)
	if err != nil {
		return err
	}
	r.providers[cf.GetProviderName()] = cf
	return nil
}

// NewCloudflare creates a new Cloudflare DNS provider.
func NewCloudflare(opts ...CloudflareOption) (DNSProvider, error) {
	c := &cloudflareDNS{
		token: os.Getenv("CLOUDFLARE_TOKEN"),
	}
	for _, o := range opts {
		o(c)
	}
	if c.token == "" {
		return nil, errNoCredentials
	}
	var cfOptions []cloudflare.Option
	if baseURL := os.Getenv("CLOUDFLARE_API_URL"); baseURL != "" {
		cfOptions = append(cfOptions, cloudflare.BaseURL(baseURL))
	}
	if userAgent := os.Getenv("CLOUDFLARE_USER_AGENT"); userAgent != "" {
		cfOptions = append(cfOptions, cloudflare.UserAgent(userAgent))
	} else {
		cfOptions = append(cfOptions, cloudflare.UserAgent(defaultUserAgent()))
	}
	var err error
	c.api, err = cloudflare.NewWithAPIToken(c.token, cfOptions...)
	return c, err
}

func (c *cloudflareDNS) GetProviderName() string {
	return Cloudflare
}

func (c *cloudflareDNS) EnsureDNSARecordSet(ctx context.Context, zone, recordName string, ips []string, ttl int) (err error) {
	done := c.metrics.startCall(ctx)
	defer done(&err)
	var log logrus.FieldLogger = c.log.WithFields(logrus.Fields{"zone": zone, "record_name": recordName})
	log = metacontext.AddKubeMetadataToLogger(ctx, log)
	var toDelete []string
	ipSet := make(map[string]struct{})
	for _, ip := range ips {
		ipSet[ip] = struct{}{}
	}
	existing, _, err := c.api.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(zone), cloudflare.ListDNSRecordsParams{
		Name: recordName,
		Type: dnsRecordTypeA,
	})
	if err != nil {
		return fmt.Errorf("fetching existing cloudflare dns records: %w", err)
	}
	recordTemplate := cloudflare.CreateDNSRecordParams{
		Type: dnsRecordTypeA,
		Name: recordName,
		TTL:  ttl,
	}
	for _, record := range existing {
		_, ok := ipSet[record.Content]
		if ok {
			if record.TTL != ttl || (record.Proxied != nil && *record.Proxied) {
				update := cloudflare.UpdateDNSRecordParams{
					ID:      record.ID,
					Content: record.Content,

					Type: recordTemplate.Type,
					Name: recordTemplate.Name,
					TTL:  recordTemplate.TTL,
				}
				log.WithField("record_id", record.ID).Debug("updating record")
				_, err := c.api.UpdateDNSRecord(ctx, cloudflare.ZoneIdentifier(zone), update)
				if err != nil {
					return c.toRetryError(fmt.Errorf("updating record: %w", err))
				}
			}
			delete(ipSet, record.Content)
		} else {
			toDelete = append(toDelete, record.ID)
		}
	}

	// Update/Create records for any IPs without records.
	for ip := range ipSet {
		if len(toDelete) > 0 {
			update := cloudflare.UpdateDNSRecordParams{
				ID:      toDelete[0],
				Content: ip,

				Type: recordTemplate.Type,
				Name: recordTemplate.Name,
				TTL:  recordTemplate.TTL,
			}
			log.WithFields(logrus.Fields{"record_id": toDelete[0], "ip": ip}).Debug("updating record target")
			_, err := c.api.UpdateDNSRecord(ctx, cloudflare.ZoneIdentifier(zone), update)
			if err != nil {
				return c.toRetryError(fmt.Errorf("updating record: %w", err))
			}
			toDelete = toDelete[1:]
		} else {
			recordTemplate.Content = ip
			log.WithField("ip", ip).Debug("creating new record")
			_, err := c.api.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(zone), recordTemplate)
			if err != nil {
				return c.toRetryError(fmt.Errorf("creating record: %w", err))
			}
		}
	}

	// Clear any unneeded records.
	for _, id := range toDelete {
		log.WithField("record_id", id).Debug("deleting unneeded record")
		err := c.api.DeleteDNSRecord(ctx, cloudflare.ZoneIdentifier(zone), id)
		if err != nil {
			return c.toRetryError(fmt.Errorf("deleting record: %w", err))
		}
	}
	log.Debug("completed updating a record")
	return nil
}

// RecordNameAndZoneToFQDN translates a zone+recordName into a fully-qualified domain name.
func (c *cloudflareDNS) RecordNameAndZoneToFQDN(_, recordName string) string {
	return recordName
}

func (c *cloudflareDNS) toRetryError(err error) error {
	// cloudflare-go manages its own rate-limiting backoff
	var nErr net.Error
	if errors.As(err, &nErr) {
		if nErr.Temporary() {
			return NewRetryError(err, RetryFast)
		}
	}
	return err
}
