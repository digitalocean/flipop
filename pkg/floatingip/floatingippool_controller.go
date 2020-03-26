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

package floatingip

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/digitalocean/flipop/pkg/nodematch"
	"github.com/digitalocean/flipop/pkg/provider"
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

	providers map[string]provider.Provider

	pools    map[string]floatingIPPool
	poolLock sync.Mutex

	log logrus.FieldLogger
	ctx context.Context
}

type floatingIPPool struct {
	matchController *nodematch.Controller
	ipController    *ipController
}

// NewController creates a new Controller.
func NewController(kubeConfig clientcmd.ClientConfig, providers map[string]provider.Provider, log logrus.FieldLogger) (*Controller, error) {
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
	if !isKnownPool {
		if !isValid {
			return // c.validate logs & updates the FloatingIPPool's status to indicate the error.
		}
		ipc := newIPController(log,
			c.ipUpdater(log, k8sPool.Name, k8sPool.Namespace),
			c.statusUpdater(log, k8sPool.Name, k8sPool.Namespace))
		pool = floatingIPPool{
			matchController: nodematch.NewController(log, c.kubeCS, ipc),
			ipController:    ipc,
		}
		pool.matchController.SetCriteria(&k8sPool.Spec.Match)
		pool.matchController.Start(c.ctx)
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

	prov := c.providers[k8sPool.Spec.Provider]
	ipChange := pool.ipController.updateProvider(prov, k8sPool.Spec.Region)
	pool.ipController.updateIPs(k8sPool.Spec.IPs, k8sPool.Spec.DesiredIPs)
	pool.ipController.updateDNSSpec(k8sPool.Spec.DNSRecordSet)
	if ipChange {
		pool.ipController.start(c.ctx)
	}
}

func (c *Controller) validate(log logrus.FieldLogger, k8sPool *flipopv1alpha1.FloatingIPPool) bool {
	if _, ok := c.providers[k8sPool.Spec.Provider]; !ok {
		c.updateStatus(k8sPool, fmt.Sprintf("unknown provider %q", k8sPool.Spec.Provider))
		log.Warn("FloatingIPPool referenced unknown provider")
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
	_, err := c.flipopCS.FlipopV1alpha1().FloatingIPPools(k8sPool.Namespace).UpdateStatus(k8sPool)
	if err != nil {
		c.log.WithError(err).Error("updating FloatingIPPool status")
	}
}

func (c *Controller) statusUpdater(log logrus.FieldLogger, name, namespace string) statusUpdateFunc {
	return func(ctx context.Context, status flipopv1alpha1.FloatingIPPoolStatus) error {
		// This GET doesn't seem strictly necessary as the status subresource should update even
		// if our local resource id is stale. Nevertheless, tests using the fake client fail
		// without it. Err on the side of caution until we get this resolved.
		k8s, err := c.flipopCS.FlipopV1alpha1().FloatingIPPools(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			log.WithError(err).Error("loading FloatingIPPool status")
			return fmt.Errorf("loading FloatingIPPool: %w", err)
		}
		if reflect.DeepEqual(status, k8s.Status) {
			return nil
		}
		k8s.Status = status
		_, err = c.flipopCS.FlipopV1alpha1().FloatingIPPools(k8s.Namespace).UpdateStatus(k8s)
		if err != nil {
			log.WithError(err).Error("updating FloatingIPPool status")
			return fmt.Errorf("updating FloatingIPPool status: %w", err)
		}
		return nil
	}
}

func (c *Controller) ipUpdater(log logrus.FieldLogger, name, namespace string) newIPFunc {
	return func(ctx context.Context, ips []string) error {
		k8s, err := c.flipopCS.FlipopV1alpha1().FloatingIPPools(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			c.log.WithError(err).Error("loading FloatingIPPool status")
			return fmt.Errorf("loading FloatingIPPool: %w", err)
		}
		k8s.Spec.IPs = ips
		_, err = c.flipopCS.FlipopV1alpha1().FloatingIPPools(namespace).Update(k8s)
		if err != nil {
			log.WithError(err).Error("updating FloatingIPPool status")
			return fmt.Errorf("updating FloatingIPPool: %w", err)
		}
		return nil
	}
}
