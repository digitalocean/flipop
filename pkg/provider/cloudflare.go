package provider

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudflare/cloudflare-go"
	"github.com/sirupsen/logrus"
	"net"
	"os"
)

const Cloudflare = "cloudflare"

type cloudflareDNS struct {
	api *cloudflare.API
	log logrus.FieldLogger
}

func NewCloudflare(log logrus.FieldLogger) (DNSProvider, error) {
	token := os.Getenv("CLOUDFLARE_TOKEN")
	if token == "" {
		return nil, nil
	}
	api, err := cloudflare.NewWithAPIToken(token)
	return &cloudflareDNS{api: api, log: log}, err
}

func (c *cloudflareDNS) GetProviderName() string {
	return Cloudflare
}

func (c *cloudflareDNS) EnsureDNSARecordSet(ctx context.Context, zone, recordName string, ips []string, ttl int) error {
	log := c.log.WithFields(logrus.Fields{"zone": zone, "record_name": recordName})
	var toDelete []string
	ipSet := make(map[string]struct{})
	for _, ip := range ips {
		ipSet[ip] = struct{}{}
	}
	existing, err := c.api.DNSRecords(zone, cloudflare.DNSRecord{
		Name: recordName,
		Type: dnsRecordTypeA,
	})
	if err != nil {
		return err
	}
	recordTemplate := cloudflare.DNSRecord{
		Type: dnsRecordTypeA,
		Name: recordName,
		TTL:  ttl,
	}
	for _, record := range existing {
		_, ok := ipSet[record.Content]
		if ok {
			if record.TTL != ttl || record.Proxied {
				update := recordTemplate
				update.Content = record.Content
				update.ID = record.ID
				log.WithField("record_id", record.ID).Debug("updating record")
				err := c.api.UpdateDNSRecord(zone, record.ID, update)
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
		update := recordTemplate
		update.Content = ip
		if len(toDelete) > 0 {
			log.WithFields(logrus.Fields{"record_id": toDelete[0], "ip": ip}).Debug("updating record target")
			err := c.api.UpdateDNSRecord(zone, toDelete[0], update)
			if err != nil {
				return c.toRetryError(fmt.Errorf("updating record: %w", err))
			}
			toDelete = toDelete[1:]
		} else {
			log.WithField("ip", ip).Debug("creating new record")
			_, err := c.api.CreateDNSRecord(zone, update)
			if err != nil {
				return c.toRetryError(fmt.Errorf("creating record: %w", err))
			}
		}
	}

	// Clear any unneeded records.
	for _, id := range toDelete {
		log.WithField("record_id", id).Debug("deleting unneeded record")
		err := c.api.DeleteDNSRecord(zone, id)
		if err != nil {
			return c.toRetryError(fmt.Errorf("deleting record: %w", err))
		}
	}
	log.Debug("completed updating a record")
	return nil
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
