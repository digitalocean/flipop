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

package nodedns

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"sync"
	"time"

	"github.com/digitalocean/flipop/pkg/metacontext"
	"github.com/digitalocean/flipop/pkg/nodematch"
	"github.com/digitalocean/flipop/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	flipopCS "github.com/digitalocean/flipop/pkg/apis/flipop/generated/clientset/versioned"
	flipopInformers "github.com/digitalocean/flipop/pkg/apis/flipop/generated/informers/externalversions/flipop/v1alpha1"
	flipopv1alpha1 "github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
)

const (
	nodeDNSRecordSetResyncPeriod = 5 * time.Minute
)

// Controller watches for NodeDNSRecordSet resources and reconciles the described state into reality.
type Controller struct {
	kubeCS   kubernetes.Interface
	flipopCS flipopCS.Interface

	providers *provider.Registry

	children map[kubetypes.UID]*dnsEnablerDisabler
	lock     sync.Mutex

	log logrus.FieldLogger
	ctx context.Context

	metricRecords *prometheus.GaugeVec
}

// NewController creates a new Controller.
func NewController(
	kubeConfig clientcmd.ClientConfig,
	providers *provider.Registry,
	log logrus.FieldLogger,
) (*Controller, error) {
	c := &Controller{
		providers:     providers,
		children:      make(map[kubetypes.UID]*dnsEnablerDisabler),
		log:           log,
		metricRecords: prometheus.NewGaugeVec(recordsOpts, recordsLabels),
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
func (c *Controller) OnAdd(obj interface{}, _ bool) {
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
	nodeDNS, ok := c.children[nrs.GetUID()]
	if !ok {
		return
	}
	c.log.WithField("node_dns_resource",
		fmt.Sprintf("%s/%s", nrs.Namespace, nrs.Name)).Info("node dns resource deleted")
	nodeDNS.stop()
	c.metricRecords.Delete(nodeDNS.metricLabels())
	delete(c.children, nrs.GetUID())
}

func (c *Controller) updateOrAdd(nrs *flipopv1alpha1.NodeDNSRecordSet) {
	c.lock.Lock()
	defer c.lock.Unlock()
	log := c.log.WithField("node_dns_resource",
		fmt.Sprintf("%s/%s", nrs.Namespace, nrs.Name))
	isValid := c.validate(log, nrs)

	ctx := metacontext.WithKubeObject(c.ctx, nrs)
	nodeDNS, known := c.children[nrs.GetUID()]
	if !known {
		if !isValid {
			return // c.validate logs & updates the NodeDNSRecordSet's status to indicate the error.
		}
		nodeDNS = newDNSEnablerDisabler(ctx, c.log, c.kubeCS, c.flipopCS)
		log.Info("NodeDNSRecordSet added; beginning reconciliation")
		c.children[nrs.GetUID()] = nodeDNS
	}
	if !isValid {
		log.Info("updated NodeDNSRecordSet spec is invalid")
		nodeDNS.stop()
		delete(c.children, nrs.GetUID())
		return
	}

	nodeDNS.update(nrs, c.providers, c.metricRecords)
	nodeDNS.start(ctx)
}

func (c *Controller) validate(log logrus.FieldLogger, nrs *flipopv1alpha1.NodeDNSRecordSet) bool {
	prov := c.providers.Get(nrs.Spec.DNSRecordSet.Provider)
	if prov == nil {
		log.Warn("NodeDNSRecordSet referenced unknown provider")
		status := &flipopv1alpha1.NodeDNSRecordSetStatus{
			Error: fmt.Sprintf("unknown provider %q", nrs.Spec.DNSRecordSet.Provider),
			State: flipopv1alpha1.NodeDNSRecordError,
		}
		err := updateStatus(c.ctx, c.flipopCS, nrs.Name, nrs.Namespace, status)
		if err != nil {
			c.log.WithError(err).Error("updating status")
		}
		return false
	}
	if _, ok := prov.(provider.DNSProvider); !ok {
		log.WithField("provider", nrs.Spec.DNSRecordSet.Provider).
			Warn("NodeDNSRecordSet referenced provider without dns capability")
		status := &flipopv1alpha1.NodeDNSRecordSetStatus{
			Error: fmt.Sprintf("provider %q does not provide DNS", nrs.Spec.DNSRecordSet.Provider),
			State: flipopv1alpha1.NodeDNSRecordError,
		}
		err := updateStatus(c.ctx, c.flipopCS, nrs.Name, nrs.Namespace, status)
		if err != nil {
			c.log.WithError(err).Error("updating status")
		}
		return false
	}
	if nrs.Spec.DNSRecordSet.Zone == "" ||
		nrs.Spec.DNSRecordSet.RecordName == "" {
		log.Warn("NodeDNSRecordSet had invalid dnsRecordSet specification")
		status := &flipopv1alpha1.NodeDNSRecordSetStatus{
			Error: "invalid dnsRecordSet specification",
			State: flipopv1alpha1.NodeDNSRecordError,
		}
		err := updateStatus(c.ctx, c.flipopCS, nrs.Name, nrs.Namespace, status)
		if err != nil {
			c.log.WithError(err).Error("updating status")
		}
		return false
	}
	err := nodematch.ValidateMatch(&nrs.Spec.Match)
	if err != nil {
		log.WithError(err).Warn("NodeDNSRecordSet had invalid match criteria")
		status := &flipopv1alpha1.NodeDNSRecordSetStatus{
			Error: "Error " + err.Error(),
			State: flipopv1alpha1.NodeDNSRecordError,
		}
		err = updateStatus(c.ctx, c.flipopCS, nrs.Name, nrs.Namespace, status)
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
	provider        provider.DNSProvider
	k8s             *flipopv1alpha1.NodeDNSRecordSet
	ctx             context.Context
	log             logrus.FieldLogger
	kubeCS          kubernetes.Interface
	flipopCS        flipopCS.Interface
	matchController *nodematch.Controller
	retryTimer      *time.Timer
	retries         int
	metricRecords   prometheus.Gauge
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

func (d *dnsEnablerDisabler) update(k8s *flipopv1alpha1.NodeDNSRecordSet, provs *provider.Registry, metricRecords *prometheus.GaugeVec) {
	d.lock.Lock()
	defer d.lock.Unlock()
	if d.k8s != nil {
		metricRecords.Delete(d.metricLabels())
	}
	if d.matchController != nil &&
		(!d.matchController.IsCriteriaEqual(&k8s.Spec.Match) ||
			d.provider != provs.Get(k8s.Spec.DNSRecordSet.Provider) ||
			!reflect.DeepEqual(d.k8s.Spec.DNSRecordSet, k8s.Spec.DNSRecordSet)) {
		d.log.Info("NodeDNSRecordSet updated; restarting controller.")
		d.matchController.Stop()
		d.matchController = nil
		d.activeNodes = make(map[string]*corev1.Node)
	}
	d.provider = provs.Get(k8s.Spec.DNSRecordSet.Provider).(provider.DNSProvider)
	if d.matchController == nil {
		d.matchController = nodematch.NewController(d.log, d.kubeCS, d)
		d.matchController.SetCriteria(&k8s.Spec.Match)
		if d.retryTimer != nil {
			d.retryTimer.Stop()
		}
	}
	d.k8s = k8s.DeepCopy()
	d.metricRecords = metricRecords.With(d.metricLabels())
}

func (d *dnsEnablerDisabler) metricLabels() prometheus.Labels {
	return prometheus.Labels{
		"namespace": d.k8s.GetNamespace(),
		"name":      d.k8s.GetName(),
		"provider":  d.provider.GetProviderName(),
		"dns": d.provider.RecordNameAndZoneToFQDN(
			d.k8s.Spec.DNSRecordSet.Zone, d.k8s.Spec.DNSRecordSet.RecordName),
	}
}

func (d *dnsEnablerDisabler) EnableNodes(ctx context.Context, nodes ...*corev1.Node) {
	d.lock.Lock()
	defer d.lock.Unlock()
	for _, node := range nodes {
		d.activeNodes[node.Name] = node.DeepCopy()
	}
	d.applyDNS()
}

func (d *dnsEnablerDisabler) DisableNodes(ctx context.Context, nodes ...*corev1.Node) {
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
	ll := d.log.WithFields(logrus.Fields{
		"address_type": string(addressType),
		"zone":         d.k8s.Spec.DNSRecordSet.Zone,
		"record_name":  d.k8s.Spec.DNSRecordSet.RecordName,
	})
	ll.Debug("ensuring provider dnsRecordSet records")
	var ips []string
	// NOTE, we don't watch nodes for updates to their address list. Since these are typically
	// added only a node bootstrap, and the match controller waits for nodes to enter the Ready
	// condition, this should be fine.
	for _, node := range d.activeNodes {
		var found bool
		ll := ll.WithField("node", node.Name)

		if addressType == flipopv1alpha1.IPv4ReservedIPAnnotation {
			reservedIP, ok := node.Annotations[flipopv1alpha1.IPv4ReservedIPAnnotation]
			if ok {
				ips = append(ips, reservedIP)
				found = true
			}
		} else {
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
		}
		if !found {
			ll.Warn("matching node had no IPs of the expected type")
		}
	}
	status := &flipopv1alpha1.NodeDNSRecordSetStatus{
		State: flipopv1alpha1.NodeDNSRecordInProgress,
	}
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
		ll.WithField("ips", ips).Debug("DNS record updated")
		status.State = flipopv1alpha1.NodeDNSRecordActive
	}
	err = updateStatus(d.ctx, d.flipopCS, d.k8s.Name, d.k8s.Namespace, status)
	if err != nil {
		ll.WithError(err).Error("updating status")
		return
	}
	d.metricRecords.Set(float64(len(ips)))
	d.retries = 0
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

func updateStatus(ctx context.Context, cs flipopCS.Interface, name, namespace string, status *flipopv1alpha1.NodeDNSRecordSetStatus) error {
	k8s, err := cs.FlipopV1alpha1().NodeDNSRecordSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("loading NodeDNSRecordSet: %w", err)
	}
	if reflect.DeepEqual(*status, k8s.Status) {
		return nil
	}
	k8s.Status = *status
	_, err = cs.FlipopV1alpha1().NodeDNSRecordSets(namespace).UpdateStatus(ctx, k8s, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating NodeDNSRecordSet status: %w", err)
	}
	return nil
}
