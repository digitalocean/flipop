package nodedns

/*
MIT License

Copyright (c) 2020 Digital Ocean, Inc.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

*/

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"sync"
	"time"

	"github.com/digitalocean/flipop/pkg/nodematch"
	"github.com/digitalocean/flipop/pkg/provider"
	"github.com/sirupsen/logrus"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	flipopCS "github.com/digitalocean/flipop/pkg/apis/flipop/generated/clientset/versioned"
	flipopInformers "github.com/digitalocean/flipop/pkg/apis/flipop/generated/informers/externalversions/flipop/v1alpha1"
	flipopv1alpha1 "github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	nodeDNSRecordSetResyncPeriod = 5 * time.Minute
)

// Controller watches for NodeDNSRecordSet resources and reconciles the described state into reality.
type Controller struct {
	kubeCS   kubernetes.Interface
	flipopCS flipopCS.Interface

	providers map[string]provider.Provider

	children map[string]*dnsEnablerDisabler
	lock     sync.Mutex

	log logrus.FieldLogger
	ctx context.Context
}

// NewController creates a new Controller.
func NewController(
	kubeConfig clientcmd.ClientConfig,
	providers map[string]provider.Provider,
	log logrus.FieldLogger,
) (*Controller, error) {
	c := &Controller{
		providers: providers,
		children:  make(map[string]*dnsEnablerDisabler),
		log:       log,
	}
	var err error
	clientConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("building kubernetes client config")
	}
	c.kubeCS, err = kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("building kubernetes clientset: %w", err)
	}
	c.flipopCS, err = flipopCS.NewForConfig(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("building flipop clientset: %w", err)
	}
	return c, nil
}

// Run watches for NodeDNSRecordSets and reconciles their state into reality.
func (c *Controller) Run(ctx context.Context) {
	informer := flipopInformers.NewNodeDNSRecordSetInformer(c.flipopCS, "", nodeDNSRecordSetResyncPeriod, cache.Indexers{})
	informer.AddEventHandler(c)
	c.ctx = ctx
	c.log.Info("NodeDNSRecordSet controller starting")
	informer.Run(ctx.Done())
	c.log.Info("NodeDNSRecordSet controller shutting down")
	c.lock.Lock()
	defer c.lock.Unlock()
	for k, m := range c.children {
		// Our parent's canceling of the context should stop all of the children concurrently.
		// This loop just verifies all children have completed.
		c.log.WithField("node_dns_resource", k).Debug("stopping match controller")
		m.stop()
		c.log.WithField("node_dns_resource", k).Debug("NodeDNSRecordSet shutdown complete")
		delete(c.children, k)
	}
}

// OnAdd implements the shared informer ResourceEventHandler for NodeDNSRecordSets.
func (c *Controller) OnAdd(obj interface{}) {
	nrs, ok := obj.(*flipopv1alpha1.NodeDNSRecordSet)
	if !ok {
		c.log.WithField("unexpected_type", fmt.Sprintf("%T", obj)).Warn("unexpected type")
	}
	c.updateOrAdd(nrs)
}

// OnUpdate implements the shared informer ResourceEventHandler for NodeDNSRecordSets.
func (c *Controller) OnUpdate(_, newObj interface{}) {
	nrs, ok := newObj.(*flipopv1alpha1.NodeDNSRecordSet)
	if !ok {
		c.log.WithField("unexpected_type", fmt.Sprintf("%T", newObj)).Warn("unexpected type")
	}
	c.updateOrAdd(nrs)
}

// OnDelete implements the shared informer ResourceEventHandler for NodeDNSRecordSets.
func (c *Controller) OnDelete(obj interface{}) {
	nrs, ok := obj.(*flipopv1alpha1.NodeDNSRecordSet)
	if !ok {
		c.log.WithField("unexpected_type", fmt.Sprintf("%T", obj)).Warn("unexpected type")
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	nodeDNS, ok := c.children[nrs.GetSelfLink()]
	if !ok {
		return
	}
	c.log.WithField("node_dns_resource",
		fmt.Sprintf("%s/%s", nrs.Namespace, nrs.Name)).Info("node dns resource deleted")
	nodeDNS.stop()
	delete(c.children, nrs.GetSelfLink())
}

func (c *Controller) updateOrAdd(nrs *flipopv1alpha1.NodeDNSRecordSet) {
	c.lock.Lock()
	defer c.lock.Unlock()
	log := c.log.WithField("node_dns_resource",
		fmt.Sprintf("%s/%s", nrs.Namespace, nrs.Name))
	isValid := c.validate(log, nrs)

	nodeDNS, known := c.children[nrs.GetSelfLink()]
	if !known {
		if !isValid {
			return // c.validate logs & updates the NodeDNSRecordSet's status to indicate the error.
		}
		nodeDNS = newDNSEnablerDisabler(c.ctx, c.log, c.kubeCS, c.flipopCS)
		log.Info("NodeDNSRecordSet added; beginning reconciliation")
		c.children[nrs.GetSelfLink()] = nodeDNS
	}
	if !isValid {
		log.Info("updated NodeDNSRecordSet spec is invalid")
		nodeDNS.stop()
		delete(c.children, nrs.GetSelfLink())
		return
	}

	nodeDNS.update(nrs, c.providers)
	nodeDNS.start(c.ctx)
}

func (c *Controller) validate(log logrus.FieldLogger, nrs *flipopv1alpha1.NodeDNSRecordSet) bool {
	if _, ok := c.providers[nrs.Spec.Provider]; !ok {
		log.Warn("NodeDNSRecordSet referenced unknown provider")
		status := &flipopv1alpha1.NodeDNSRecordSetStatus{
			Error: fmt.Sprintf("unknown provider %q", nrs.Spec.Provider)}
		err := updateStatus(c.flipopCS, nrs.Name, nrs.Namespace, status)
		if err != nil {
			c.log.WithError(err).Error("updating status")
		}
		return false
	}
	if nrs.Spec.DNSRecordSet.Zone == "" ||
		nrs.Spec.DNSRecordSet.RecordName == "" {
		log.Warn("NodeDNSRecordSet had invalid dnsRecordSet specification")
		status := &flipopv1alpha1.NodeDNSRecordSetStatus{Error: "invalid dnsRecordSet specification"}
		err := updateStatus(c.flipopCS, nrs.Name, nrs.Namespace, status)
		if err != nil {
			c.log.WithError(err).Error("updating status")
		}
		return false
	}
	err := nodematch.ValidateMatch(&nrs.Spec.Match)
	if err != nil {
		log.WithError(err).Warn("NodeDNSRecordSet had invalid match criteria")
		status := &flipopv1alpha1.NodeDNSRecordSetStatus{Error: "Error " + err.Error()}
		err = updateStatus(c.flipopCS, nrs.Name, nrs.Namespace, status)
		if err != nil {
			c.log.WithError(err).Error("updating status")
		}
		return false
	}
	return true
}

// dnsEnablerDisabler manages the state of a single NodeDNSRecordSet resource, and implements
// NodeEnablerDisabler to leverage nodematch.Controller.
type dnsEnablerDisabler struct {
	lock            sync.Mutex
	activeNodes     map[string]*corev1.Node
	provider        provider.Provider
	k8s             *flipopv1alpha1.NodeDNSRecordSet
	ctx             context.Context
	log             logrus.FieldLogger
	kubeCS          kubernetes.Interface
	flipopCS        flipopCS.Interface
	matchController *nodematch.Controller
	retryTimer      *time.Timer
	retries         int
}

func newDNSEnablerDisabler(
	ctx context.Context,
	log logrus.FieldLogger,
	kubeCS kubernetes.Interface,
	flipopCS flipopCS.Interface,
) *dnsEnablerDisabler {
	d := &dnsEnablerDisabler{
		activeNodes: make(map[string]*corev1.Node),
		log:         log,
		ctx:         ctx,
		flipopCS:    flipopCS,
		kubeCS:      kubeCS,
	}
	return d
}

func (d *dnsEnablerDisabler) update(k8s *flipopv1alpha1.NodeDNSRecordSet, provs map[string]provider.Provider) {
	d.lock.Lock()
	defer d.lock.Unlock()
	if d.matchController != nil &&
		(!d.matchController.IsCriteriaEqual(&k8s.Spec.Match) || d.provider != provs[k8s.Spec.Provider]) {
		d.matchController.Stop()
		d.matchController = nil
		d.activeNodes = make(map[string]*corev1.Node)
	}
	d.provider = provs[k8s.Spec.Provider]
	if d.matchController == nil {
		d.matchController = nodematch.NewController(d.log, d.kubeCS, d)
		d.matchController.SetCriteria(&k8s.Spec.Match)
		if d.retryTimer != nil {
			d.retryTimer.Stop()
		}
	}
	d.k8s = k8s.DeepCopy()
}

func (d *dnsEnablerDisabler) EnableNodes(nodes ...*corev1.Node) {
	d.lock.Lock()
	defer d.lock.Unlock()
	for _, node := range nodes {
		d.activeNodes[node.Name] = node.DeepCopy()
	}
	d.applyDNS()
}

func (d *dnsEnablerDisabler) DisableNodes(nodes ...*corev1.Node) {
	d.lock.Lock()
	defer d.lock.Unlock()
	for _, node := range nodes {
		delete(d.activeNodes, node.Name)
	}
	d.applyDNS()
}

func (d *dnsEnablerDisabler) applyDNS() {
	addressType := corev1.NodeExternalIP
	if d.k8s.Spec.AddressType != "" {
		addressType = d.k8s.Spec.AddressType
	}
	ll := d.log.WithField("address_type", string(addressType))
	ll.Debug("ensuring provider dnsRecordSet records")
	var ips []string
	// NOTE, we don't watch nodes for updates to their address list. Since these are typically
	// added only a node bootstrap, and the match controller waits for nodes to enter the Ready
	// condition, this should be fine.
	for _, node := range d.activeNodes {
		var found bool
		ll := ll.WithField("node", node.Name)
		for _, addr := range node.Status.Addresses {
			if addr.Type != addressType {
				continue
			}
			ip := net.ParseIP(addr.Address)
			if ip == nil {
				ll.WithField("address", addr.Address).Warn("Failed to parse IP")
				continue
			}
			ip = ip.To4()
			if ip == nil {
				ll.WithField("address", addr.Address).Warn("IPv6 addresses are NOT currently supported")
				continue
			}
			ips = append(ips, ip.String())
			found = true
		}
		if !found {
			ll.Warn("matching node had no IPs of the expected type")
		}
	}
	status := &flipopv1alpha1.NodeDNSRecordSetStatus{}
	err := d.provider.EnsureDNSARecordSet(
		d.ctx,
		d.k8s.Spec.DNSRecordSet.Zone,
		d.k8s.Spec.DNSRecordSet.RecordName,
		ips,
		d.k8s.Spec.DNSRecordSet.TTL)
	if err != nil {
		retry := provider.ErrorToRetrySchedule(err)
		retryAfter := retry.After(d.retries)
		d.log.WithError(err).
			WithField("next_retry", retryAfter.String()).
			Error("updating DNS record set; scheduling retry")
		d.retryTimer = time.AfterFunc(retryAfter, func() {
			d.log.Debug("initiating retry")
			d.lock.Lock()
			defer d.lock.Unlock()
			d.log.Debug("doing retry")
			d.applyDNS()
		})
		d.retries++
		status.Error = fmt.Sprintf("Failed to update DNS: %s", err.Error())
	} else {
		ll.Info("DNS records updated")
	}
	err = updateStatus(d.flipopCS, d.k8s.Name, d.k8s.Namespace, status)
	if err != nil {
		ll.WithError(err).Error("updating status")
		return
	}
	d.retries = 0
	return
}

func (d *dnsEnablerDisabler) stop() {
	d.lock.Lock()
	defer d.lock.Unlock()
	if d.retryTimer != nil {
		d.retryTimer.Stop()
	}
	if d.matchController != nil {
		d.matchController.Stop()
	}
}

func (d *dnsEnablerDisabler) start(ctx context.Context) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.ctx = ctx
	d.matchController.Start(ctx)
}

func updateStatus(cs flipopCS.Interface, name, namespace string, status *flipopv1alpha1.NodeDNSRecordSetStatus) error {
	k8s, err := cs.FlipopV1alpha1().NodeDNSRecordSets(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("loading NodeDNSRecordSet: %w", err)
	}
	if reflect.DeepEqual(*status, k8s.Status) {
		return nil
	}
	k8s.Status = *status
	_, err = cs.FlipopV1alpha1().NodeDNSRecordSets(namespace).UpdateStatus(k8s)
	if err != nil {
		return fmt.Errorf("updating NodeDNSRecordSet status: %w", err)
	}
	return nil
}