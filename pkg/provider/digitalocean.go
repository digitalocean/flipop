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

	// doActionExpiration defines how long we'll hold onto an action before expiring it.
	doActionExpiration = 1 * time.Hour
)

type doAction struct {
	actionID int
	expires  time.Time
}

type digitalOcean struct {
	*godo.Client
	lock              sync.Mutex
	floatingIPActions map[string]*doAction
	ll                logrus.FieldLogger
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
func NewDigitalOcean(ll logrus.FieldLogger) Provider {
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
		Client:            client,
		floatingIPActions: make(map[string]*doAction),
		ll:                ll.WithField("provider", "digitalocean"),
	}
}

// IPToProviderID loads the current assignment (as Kubernetes listed in Kubernetes core v1
// NodeSpec.ProviderID for a floating IP.
func (do *digitalOcean) IPToProviderID(ctx context.Context, ip string) (string, error) {
	err := do.asyncStatus(ctx, ip)
	if err != nil {
		return "", err
	}
	flip, res, err := do.FloatingIPs.Get(ctx, ip)
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
	ll := do.ll.WithFields(logrus.Fields{"ip": ip, "provider_id": providerID})
	err := do.asyncStatus(ctx, ip)
	if err != nil {
		ll.WithError(err).Debug("checking in-progress status for assign IP")
		return err
	}
	dropletID, err := strconv.ParseInt(strings.TrimPrefix(providerID, "digitalocean://"), 10, 0)
	if err != nil {
		return fmt.Errorf("parsing provider id: %w", err)
	}
	action, res, err := do.FloatingIPActions.Assign(ctx, ip, int(dropletID))
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
	do.ll.WithFields(logrus.Fields{"ip": ip, "action_id": action.ID}).Debug("floating IP assignment initiated")
	return ErrInProgress
}

// CreateIP creates a new floating IP.
func (do *digitalOcean) CreateIP(ctx context.Context, region string) (string, error) {
	flip, _, err := do.FloatingIPs.Create(ctx, &godo.FloatingIPCreateRequest{
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
	droplet, res, err := do.Droplets.Get(ctx, int(dropletID))
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
		// All droplets have a public IP. With a FLIP, they'll have 2. If there's only 1, no FLIP.
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
	ll := do.ll.WithFields(logrus.Fields{"ip": ip, "action_id": a.actionID})
	action, _, err := do.FloatingIPActions.Get(ctx, ip, a.actionID)
	if err != nil {
		ll.WithError(err).Error("retrieving action status")
		return err
	}
	do.lock.Lock()
	defer do.lock.Unlock()
	if action == nil || action.Status != godo.ActionInProgress {
		ll.Debug("action completed")
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
			return newRetryError(err, RetryFast)
		}
	}
	if nErr, ok := err.(net.Error); ok {
		if nErr.Temporary() {
			return newRetryError(err, RetryFast)
		}
	}
	return err
}
