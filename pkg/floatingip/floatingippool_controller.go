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

package floatingip

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/digitalocean/flipop/pkg/metacontext"
	"github.com/digitalocean/flipop/pkg/nodematch"
	"github.com/digitalocean/flipop/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	flipopCS "github.com/digitalocean/flipop/pkg/apis/flipop/generated/clientset/versioned"
	flipopInformers "github.com/digitalocean/flipop/pkg/apis/flipop/generated/informers/externalversions/flipop/v1alpha1"
	flipopv1alpha1 "github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
)

const (
	floatingIPPoolResyncPeriod = 5 * time.Minute
)

// Controller watches for FloatingIPPool resources and reconciles the described state into reality.
type Controller struct {
	kubeCS   kubernetes.Interface
	flipopCS flipopCS.Interface

	providers *provider.Registry

	pools    map[string]floatingIPPool
	poolLock sync.Mutex

	log logrus.FieldLogger
	ctx context.Context
}

type floatingIPPool struct {
	namespace       string
	name            string
	matchController *nodematch.Controller
	ipController    *ipController
}

// NewController creates a new Controller.
func NewController(
	kubeConfig clientcmd.ClientConfig,
	providers *provider.Registry,
	log logrus.FieldLogger,
	promRegistry *prometheus.Registry,
) (*Controller, error) {
	c := &Controller{
		providers: providers,
		pools:     make(map[string]floatingIPPool),
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

// Run watches for FloatingIPPools and reconciles their state into reality.
func (c *Controller) Run(ctx context.Context) {
	informer := flipopInformers.NewFloatingIPPoolInformer(c.flipopCS, "", floatingIPPoolResyncPeriod, cache.Indexers{})
	informer.AddEventHandler(c)
	c.ctx = ctx
	c.log.Info("FloatingIPPool controller starting")
	informer.Run(ctx.Done())
	c.log.Info("FloatingIPPool controller shutting down")
	c.poolLock.Lock()
	defer c.poolLock.Unlock()
	for k, m := range c.pools {
		// Our parent's canceling of the context should stop all of the children concurrently.
		// This loop just verifies all children have completed.
		c.log.WithField("pool", k).Debug("stopping match controller")
		m.matchController.Stop()
		c.log.WithField("pool", k).Debug("stopping ip controller")
		m.ipController.stop()
		c.log.WithField("pool", k).Debug("FloatingIPPool shutdown complete")
		delete(c.pools, k)
	}
}

// OnAdd implements the shared informer ResourceEventHandler for FloatingIPPools.
func (c *Controller) OnAdd(obj interface{}) {
	k8sPool, ok := obj.(*flipopv1alpha1.FloatingIPPool)
	if !ok {
		c.log.WithField("unexpected_type", fmt.Sprintf("%T", obj)).Warn("unexpected type")
	}
	c.updateOrAdd(k8sPool)
}

// OnUpdate implements the shared informer ResourceEventHandler for FloatingIPPools.
func (c *Controller) OnUpdate(_, newObj interface{}) {
	k8sPool, ok := newObj.(*flipopv1alpha1.FloatingIPPool)
	if !ok {
		c.log.WithField("unexpected_type", fmt.Sprintf("%T", newObj)).Warn("unexpected type")
	}
	c.updateOrAdd(k8sPool)
}

// OnDelete implements the shared informer ResourceEventHandler for FloatingIPPools.
func (c *Controller) OnDelete(obj interface{}) {
	k8sPool, ok := obj.(*flipopv1alpha1.FloatingIPPool)
	if !ok {
		c.log.WithField("unexpected_type", fmt.Sprintf("%T", obj)).Warn("unexpected type")
	}
	c.poolLock.Lock()
	defer c.poolLock.Unlock()
	pool, ok := c.pools[k8sPool.GetSelfLink()]
	if !ok {
		return
	}
	c.log.WithField("floating_ip_pool", fmt.Sprintf("%s/%s", k8sPool.Namespace, k8sPool.Name)).Info("pool deleted")
	pool.matchController.Stop()
	pool.ipController.stop()
	delete(c.pools, k8sPool.GetSelfLink())
}

func (c *Controller) updateOrAdd(k8sPool *flipopv1alpha1.FloatingIPPool) {
	c.poolLock.Lock()
	defer c.poolLock.Unlock()
	log := c.log.WithField("floating_ip_pool", fmt.Sprintf("%s/%s", k8sPool.Namespace, k8sPool.Name))
	isValid := c.validate(log, k8sPool)

	pool, isKnownPool := c.pools[k8sPool.GetSelfLink()]
	if isKnownPool && !pool.matchController.IsCriteriaEqual(&k8sPool.Spec.Match) {
		isKnownPool = false
		log.Info("match criteria changed, resetting")
		pool.matchController.Stop()
		pool.ipController.stop()
		delete(c.pools, k8sPool.GetSelfLink())
	}
	ctx := metacontext.WithKubeObject(c.ctx, k8sPool)
	if !isKnownPool {
		if !isValid {
			return // c.validate logs & updates the FloatingIPPool's status to indicate the error.
		}
		ipc := newIPController(log,
			c.ipUpdater(log, k8sPool.Name, k8sPool.Namespace),
			c.statusUpdater(log, k8sPool.Name, k8sPool.Namespace))
		pool = floatingIPPool{
			namespace:       k8sPool.Namespace,
			name:            k8sPool.Name,
			matchController: nodematch.NewController(log, c.kubeCS, ipc),
			ipController:    ipc,
		}
		pool.matchController.SetCriteria(&k8sPool.Spec.Match)
		pool.matchController.Start(ctx)
		log.Info("FloatingIPPool added; beginning reconciliation")
		c.pools[k8sPool.GetSelfLink()] = pool
	}
	if !isValid {
		log.Info("updated FloatingIPPool spec is invalid")
		pool.matchController.Stop()
		pool.ipController.stop()
		delete(c.pools, k8sPool.GetSelfLink())
		return
	}

	prov := c.providers.Get(k8sPool.Spec.Provider).(provider.IPProvider)
	dnsProv, _ := prov.(provider.DNSProvider)
	if k8sPool.Spec.DNSRecordSet != nil && k8sPool.Spec.DNSRecordSet.Provider != "" {
		dnsProv, _ = c.providers.Get(k8sPool.Spec.DNSRecordSet.Provider).(provider.DNSProvider)
	}
	coolOff := time.Duration(k8sPool.Spec.AssignmentCoolOffSeconds * float64(time.Second))
	ipChange := pool.ipController.updateProviders(prov, dnsProv, k8sPool.Spec.Region, coolOff)
	pool.ipController.updateIPs(k8sPool.Spec.IPs, k8sPool.Spec.DesiredIPs)
	pool.ipController.updateDNSSpec(k8sPool.Spec.DNSRecordSet)
	if ipChange {
		pool.ipController.start(ctx)
	}
}

func (c *Controller) validate(log logrus.FieldLogger, k8sPool *flipopv1alpha1.FloatingIPPool) bool {
	prov := c.providers.Get(k8sPool.Spec.Provider)
	if prov == nil {
		c.updateStatus(k8sPool, fmt.Sprintf("unknown provider %q", k8sPool.Spec.Provider))
		log.Warn("FloatingIPPool referenced unknown provider")
		return false
	}
	if _, ok := prov.(provider.IPProvider); !ok {
		c.updateStatus(k8sPool, fmt.Sprintf("provider %q does not provide floating IPs", k8sPool.Spec.Provider))
		log.WithField("provider", k8sPool.Spec.Provider).
			Warn("FloatingIPPool referenced provider that does not support floating IPs")
		return false
	}
	if len(k8sPool.Spec.IPs) == 0 && k8sPool.Spec.DesiredIPs == 0 {
		c.updateStatus(k8sPool, "ips or desiredIPs must be provided")
		log.Warn("FloatingIPPool had neither ips nor desiredIPs")
		return false
	}
	err := nodematch.ValidateMatch(&k8sPool.Spec.Match)
	if err != nil {
		c.updateStatus(k8sPool, "Error "+err.Error())
		log.WithError(err).Warn("FloatingIPPool had invalid match criteria")
		return false
	}
	if k8sPool.Spec.DNSRecordSet != nil {
		dnsProv := c.providers.Get(k8sPool.Spec.DNSRecordSet.Provider)
		if dnsProv == nil {
			dnsProv = prov
		}
		if _, ok := dnsProv.(provider.DNSProvider); !ok {
			c.updateStatus(k8sPool, "FloatingIPPool dns referenced provider without dns capability")
			log.WithError(err).WithField("provider", dnsProv.GetProviderName()).
				Warn("FloatingIPPool dns referenced provider without dns capability")
			return false
		}
	}
	return true
}

func (c *Controller) updateStatus(k8sPool *flipopv1alpha1.FloatingIPPool, errMsg string) {
	s := flipopv1alpha1.FloatingIPPoolStatus{
		Error: errMsg,
	}
	if reflect.DeepEqual(s, k8sPool.Status) {
		return
	}
	k8sPool.Status = s
	_, err := c.flipopCS.FlipopV1alpha1().FloatingIPPools(k8sPool.Namespace).UpdateStatus(c.ctx, k8sPool, metav1.UpdateOptions{})
	if err != nil {
		c.log.WithError(err).Error("updating FloatingIPPool status")
	}
}

func (c *Controller) statusUpdater(log logrus.FieldLogger, name, namespace string) statusUpdateFunc {
	return func(ctx context.Context, status flipopv1alpha1.FloatingIPPoolStatus) error {
		// This GET doesn't seem strictly necessary as the status subresource should update even
		// if our local resource id is stale. Nevertheless, tests using the fake client fail
		// without it. Err on the side of caution until we get this resolved.
		k8s, err := c.flipopCS.FlipopV1alpha1().FloatingIPPools(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			log.WithError(err).Error("loading FloatingIPPool status")
			return fmt.Errorf("loading FloatingIPPool: %w", err)
		}
		if reflect.DeepEqual(status, k8s.Status) {
			return nil
		}
		k8s.Status = status
		_, err = c.flipopCS.FlipopV1alpha1().FloatingIPPools(k8s.Namespace).UpdateStatus(ctx, k8s, metav1.UpdateOptions{})
		if err != nil {
			log.WithError(err).Error("updating FloatingIPPool status")
			return fmt.Errorf("updating FloatingIPPool status: %w", err)
		}
		return nil
	}
}

func (c *Controller) ipUpdater(log logrus.FieldLogger, name, namespace string) newIPFunc {
	return func(ctx context.Context, ips []string) error {
		k8s, err := c.flipopCS.FlipopV1alpha1().FloatingIPPools(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			c.log.WithError(err).Error("loading FloatingIPPool status")
			return fmt.Errorf("loading FloatingIPPool: %w", err)
		}
		k8s.Spec.IPs = ips
		_, err = c.flipopCS.FlipopV1alpha1().FloatingIPPools(namespace).Update(ctx, k8s, metav1.UpdateOptions{})
		if err != nil {
			log.WithError(err).Error("updating FloatingIPPool status")
			return fmt.Errorf("updating FloatingIPPool: %w", err)
		}
		return nil
	}
}
