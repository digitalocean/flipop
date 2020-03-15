package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitalocean/godo"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

const (
	// DigitalOcean is an identifier which can be used to select the godo provider.
	DigitalOcean = "digitalocean"

	// doActionExpiration defines how long we'log hold onto an action before expiring it.
	doActionExpiration = 1 * time.Hour

	dnsRecordTypeA = "A"

	dnsRecordsPerPage = 200
)

type doAction struct {
	actionID int
	expires  time.Time
}

type digitalOcean struct {
	// Separate the individual interfaces to make mocking/testing easier.
	domainsService    godo.DomainsService
	ipsService        godo.FloatingIPsService
	ipActionsService  godo.FloatingIPActionsService
	dropletsService   godo.DropletsService
	lock              sync.Mutex
	floatingIPActions map[string]*doAction
	log               logrus.FieldLogger
}

type doTokenSource struct {
	AccessToken string
}

func (t *doTokenSource) Token() (*oauth2.Token, error) {
	token := &oauth2.Token{
		AccessToken: t.AccessToken,
	}
	return token, nil
}

// NewDigitalOcean returns a new provider for DigitalOcean.
func NewDigitalOcean(log logrus.FieldLogger) Provider {
	token := os.Getenv("DIGITALOCEAN_ACCESS_TOKEN")
	if token == "" {
		return nil
	}
	tokenSource := &doTokenSource{
		AccessToken: token,
	}

	oauthClient := oauth2.NewClient(context.Background(), tokenSource)
	client := godo.NewClient(oauthClient)
	return &digitalOcean{
		domainsService:    client.Domains,
		ipsService:        client.FloatingIPs,
		ipActionsService:  client.FloatingIPActions,
		dropletsService:   client.Droplets,
		floatingIPActions: make(map[string]*doAction),
		log:               log.WithField("provider", "digitalocean"),
	}
}

// IPToProviderID loads the current assignment (as Kubernetes listed in Kubernetes core v1
// NodeSpec.ProviderID for a floating IP.
func (do *digitalOcean) IPToProviderID(ctx context.Context, ip string) (string, error) {
	err := do.asyncStatus(ctx, ip)
	if err != nil {
		return "", err
	}
	flip, res, err := do.ipsService.Get(ctx, ip)
	if err != nil {
		if res != nil && res.StatusCode == http.StatusNotFound {
			return "", ErrNotFound
		}
		return "", err
	}
	if flip.Droplet == nil {
		return "", nil
	}
	return fmt.Sprintf("digitalocean://%d", flip.Droplet.ID), nil
}

// AssignIP assigns a floating IP to the specified node.
func (do *digitalOcean) AssignIP(ctx context.Context, ip, providerID string) error {
	log := do.log.WithFields(logrus.Fields{"ip": ip, "provider_id": providerID})
	err := do.asyncStatus(ctx, ip)
	if err != nil {
		log.WithError(err).Debug("checking in-progress status for assign IP")
		return err
	}
	dropletID, err := strconv.ParseInt(strings.TrimPrefix(providerID, "digitalocean://"), 10, 0)
	if err != nil {
		return fmt.Errorf("parsing provider id: %w", err)
	}
	action, res, err := do.ipActionsService.Assign(ctx, ip, int(dropletID))
	if err != nil {
		if res != nil {
			if res.StatusCode == http.StatusNotFound {
				return ErrNotFound
			}
			if res.StatusCode == http.StatusUnprocessableEntity {
				// Typically this means the droplet already has a Floating IP. We will get this
				// error even if we try to assign it the IP it already has.

				// Check the IP's current provider.  If this fails, eat the error and return the
				// assign error.
				curProvider, iErr := do.IPToProviderID(ctx, ip)
				if curProvider != "" && curProvider == providerID {
					return nil // Already set to us. This is success.
				}
				if iErr == nil {
					// It's likely the node already has an IP. Verify that, and return an
					// appropriate error.
					nIP, _ := do.NodeToIP(ctx, curProvider)
					if nIP != "" {
						return ErrNodeInUse
					}
				}
			}
		}
		return err
	}
	if action == nil || action.CompletedAt != nil || action.ID == 0 {
		return nil
	}
	do.lock.Lock()
	defer do.lock.Unlock()
	do.floatingIPActions[ip] = &doAction{
		actionID: action.ID,
		expires:  time.Now().Add(doActionExpiration),
	}
	do.log.WithFields(logrus.Fields{"ip": ip, "action_id": action.ID}).Debug("floating IP assignment initiated")
	return ErrInProgress
}

// CreateIP creates a new floating IP.
func (do *digitalOcean) CreateIP(ctx context.Context, region string) (string, error) {
	flip, _, err := do.ipsService.Create(ctx, &godo.FloatingIPCreateRequest{
		Region: region,
	})
	if err != nil {
		return "", err
	}
	return flip.IP, nil
}

// NodeToIP attempts to find any floating IPs bound to the specified node.
func (do *digitalOcean) NodeToIP(ctx context.Context, providerID string) (string, error) {
	dropletID, err := strconv.ParseInt(strings.TrimPrefix(providerID, "digitalocean://"), 10, 0)
	if err != nil {
		return "", fmt.Errorf("parsing provider id: %w", err)
	}
	droplet, res, err := do.dropletsService.Get(ctx, int(dropletID))
	if err != nil {
		if res != nil && res.StatusCode == http.StatusNotFound {
			return "", ErrNotFound
		}
		return "", err
	}

	if droplet.Networks == nil {
		return "", errors.New("droplet had no networking info")
	}

	var ips []string
	// Floating IPs are listed among node IPs. Likely last.
	for _, v4 := range droplet.Networks.V4 {
		if v4.Type == "public" {
			ips = append(ips, v4.IPAddress)
		}
	}

	if len(ips) < 2 {
		// All droplets have a public IP. With a FLIP, they'log have 2. If there's only 1, no FLIP.
		return "", nil
	}

	// There's nothing explicitly saying the order of IPs, but it seems the FLIP is normally second.
	for i := len(ips) - 1; i >= 0; i++ {
		ip := ips[i]
		ipProvider, err := do.IPToProviderID(ctx, ip)
		if err == ErrNotFound {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("retrieving node ip: %w", err)
		}
		if ipProvider == providerID {
			return ip, nil
		}
	}

	return "", nil
}

// asyncStatus tries to abstract the asynchronous nature of DO's floating IP updates.
func (do *digitalOcean) asyncStatus(ctx context.Context, ip string) error {
	do.lock.Lock()
	// clean our cache
	now := time.Now()
	for ip, a := range do.floatingIPActions {
		if now.After(a.expires) {
			delete(do.floatingIPActions, ip)
		}
	}
	a := do.floatingIPActions[ip]
	do.lock.Unlock()
	if a == nil {
		return nil
	}
	log := do.log.WithFields(logrus.Fields{"ip": ip, "action_id": a.actionID})
	action, _, err := do.ipActionsService.Get(ctx, ip, a.actionID)
	if err != nil {
		log.WithError(err).Error("retrieving action status")
		return err
	}
	do.lock.Lock()
	defer do.lock.Unlock()
	if action == nil || action.Status != godo.ActionInProgress {
		log.Debug("action completed")
		delete(do.floatingIPActions, ip)
		return nil
	}
	return ErrInProgress
}

func (do *digitalOcean) toRetryError(err error, res *godo.Response) error {
	if res != nil {
		switch res.StatusCode {
		case http.StatusNotFound:
			return ErrNotFound
		case http.StatusRequestTimeout,
			http.StatusServiceUnavailable,
			http.StatusTooManyRequests:
			return NewRetryError(err, RetryFast)
		}
	}
	if nErr, ok := errors.Unwrap(err).(net.Error); ok {
		if nErr.Temporary() {
			return NewRetryError(err, RetryFast)
		}
	}
	return err
}

// EnsureDNSARecordSet ensures that the record set w/ name `recordName` contains all IPs listed in `ips`
// and no others.
func (do *digitalOcean) EnsureDNSARecordSet(ctx context.Context, zone, recordName string, ips []string, ttl int) error {
	// First we need to know what records exist.
	log := do.log.WithFields(logrus.Fields{"zone": zone, "record_name": recordName})
	ipSet := make(map[string]struct{})
	for _, ip := range ips {
		ipSet[ip] = struct{}{}
	}
	var toDelete []int
	listOptions := &godo.ListOptions{
		Page:    1,
		PerPage: dnsRecordsPerPage,
	}
	recordTemplate := godo.DomainRecordEditRequest{
		Type: dnsRecordTypeA,
		Name: recordName,
		TTL:  ttl,
	}
	for {
		// Until NETPROD-1354 is addressed we need to iterate the whole zone :-(.
		log.WithField("page", listOptions.Page).Debug("querying records API for zone")
		records, res, err := do.domainsService.Records(ctx, zone, listOptions)
		if err != nil {
			return do.toRetryError(fmt.Errorf("listing zone records: %w", err), res)
		}

		for _, record := range records {
			if (record.Name != recordName) || (record.Type != dnsRecordTypeA) {
				continue
			}
			_, ok := ipSet[record.Data]
			if ok {
				if record.TTL != ttl {
					update := recordTemplate
					update.Data = record.Data
					log.WithField("record_id", record.ID).Debug("updating record TTL")
					_, res, err := do.domainsService.EditRecord(ctx, zone, record.ID, &update)
					if err != nil {
						return do.toRetryError(fmt.Errorf("updating record TTL: %w", err), res)
					}
				}
				delete(ipSet, record.Data)
			} else {
				toDelete = append(toDelete, record.ID)
			}
		}

		if res == nil || res.Links == nil {
			return errors.New("invalid response from DigitalOcean API; no page links")
		}
		if res.Links.IsLastPage() {
			break
		}
		listOptions.Page++
	}

	// Update/Create records for any IPs without records.
	for ip := range ipSet {
		update := recordTemplate
		update.Data = ip
		if len(toDelete) > 0 {
			log.WithFields(logrus.Fields{"record_id": toDelete[0], "ip": ip}).Debug("updating record target")
			_, res, err := do.domainsService.EditRecord(ctx, zone, toDelete[0], &update)
			if err != nil {
				return do.toRetryError(fmt.Errorf("updating record: %w", err), res)
			}
			toDelete = toDelete[1:]
		} else {
			log.WithField("ip", ip).Debug("creating new record")
			_, res, err := do.domainsService.CreateRecord(ctx, zone, &update)
			if err != nil {
				return do.toRetryError(fmt.Errorf("creating record: %w", err), res)
			}
		}
	}

	// Clear any unneeded records.
	for _, id := range toDelete {
		log.WithField("record_id", id).Debug("deleting unneeded record")
		res, err := do.domainsService.DeleteRecord(ctx, zone, id)
		if err != nil {
			return do.toRetryError(fmt.Errorf("deleting record: %w", err), res)
		}
	}
	log.Debug("completed updating a record")
	return nil
}
