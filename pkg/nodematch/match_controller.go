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

package nodematch

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"

	corev1Informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	flipopv1alpha1 "github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
)

const (
	podResyncPeriod        = 5 * time.Minute
	nodeResyncPeriod       = 5 * time.Minute
	defaultRequeueInterval = 5 * time.Minute
	podNodeNameIndexerName = "podNodeName"

	eventTypeCreate = "create"
	eventTypeUpdate = "update"
	eventTypeDelete = "delete"
)

// NodeEnableDisabler describes a controller which can enable or disable sets of nodes, based
// upon decisions reached by the node match controller.
type NodeEnableDisabler interface {
	EnableNodes(...*corev1.Node)
	DisableNodes(...*corev1.Node)
}

type event struct {
	key          string
	eventType    string
	resourceType string
	object       interface{}
	requeued     bool
	requeuedAt   time.Time
}

type requeueError struct {
	// requeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	requeueAfter time.Duration
}

func (e *requeueError) Error() string {
	return "requeue error. requeue after: " + e.requeueAfter.String()
}

// Controller watches Kubernetes nodes and pods and enables or disables them with the provided
// action based upon the provided criteria.
type Controller struct {
	match *flipopv1alpha1.Match
	// cache the parsed selectors
	nodeSelector labels.Selector
	podSelector  labels.Selector

	nodeNameToNode map[string]*node
	queue          workqueue.RateLimitingInterface

	log          logrus.FieldLogger
	kubeCS       kubernetes.Interface
	nodeInformer cache.SharedIndexInformer

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	primed bool

	podIndexer cache.Indexer

	sync.Mutex

	action NodeEnableDisabler
}

// NewController builds a new node match Controller.
func NewController(log logrus.FieldLogger, kubeCS kubernetes.Interface, action NodeEnableDisabler) *Controller {
	m := &Controller{
		queue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "nodednsrecordset_controller"),
		log:            log,
		kubeCS:         kubeCS,
		action:         action,
		nodeNameToNode: make(map[string]*node),
	}
	return m
}

// Start begins asynchronous execution of the node match controller.
func (m *Controller) Start(ctx context.Context) {
	if m.cancel != nil {
		return // already running.
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.wg.Add(1)
	go m.run()
}

// Stop terminates execution of the node match controller and waits for it to finish.
func (m *Controller) Stop() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
		m.queue.ShutDown()
	}
	m.wg.Wait()
}

// IsCriteriaEqual returns true if the specified criteria match the current criteria.
func (m *Controller) IsCriteriaEqual(match *flipopv1alpha1.Match) bool {
	return reflect.DeepEqual(match, m.match)
}

// SetCriteria sets the match criteria based on the match spec.
func (m *Controller) SetCriteria(match *flipopv1alpha1.Match) {
	m.match = match.DeepCopy()

	var err error
	m.nodeSelector = nil
	if m.match.NodeLabel != "" {
		m.nodeSelector, err = labels.Parse(m.match.NodeLabel)
		if err != nil { // This shouldn't happen if the caller used validateMatch
			m.log.WithError(err).Error("parsing node selector")
			m.match = nil
			return
		}
	}

	m.podSelector = nil
	if m.match.PodLabel != "" {
		m.podSelector, err = labels.Parse(m.match.PodLabel)
		if err != nil {
			m.log.WithError(err).Error("parsing pod selector")
			m.match = nil
			return
		}
	}
	m.log.Info("match criteria updated")
}

// podNodeNameIndexer implements k8s.io/client-go/tools/cache.Indexer.
func podNodeNameIndexer(obj interface{}) ([]string, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod == nil {
		return nil, errors.New("expected pod type")
	}
	return []string{pod.Spec.NodeName}, nil
}

func (m *Controller) run() {
	defer m.wg.Done()
	if m.match == nil {
		// The only way this should happen is if SetCriteria was never called, or a match criteria
		// passed validation with ValidateMatch, but then failed SetCriteria.
		m.log.Warn("no match criteria set; cannot reconcile")
		return
	}

	// This does NOT use shared informers which CAN consume more memory and Kubernetes API
	// connections, IF there are other consumers which need the same subscription. Since we filter
	// on labels (and namespace for pod), we would need a shared-informer for each label-set/ns
	// combo, or an unfiltered shared informer. Since it seems likely we're only concerned about a
	// very small subset of pods, it seems better to filter these on the server. If this pattern
	// turns out to be expensive for some use cases, we could add logic/flags to enable better
	// decisions.
	var syncFuncs []cache.InformerSynced
	if m.match.PodNamespace != "" || m.podSelector != nil {
		podInformer := corev1Informers.NewFilteredPodInformer(
			m.kubeCS,
			m.match.PodNamespace,
			podResyncPeriod,
			cache.Indexers{
				podNodeNameIndexerName: podNodeNameIndexer,
			},
			func(opts *v1.ListOptions) {
				if m.podSelector != nil {
					opts.LabelSelector = m.podSelector.String()
				}
			},
		)
		m.podIndexer = podInformer.GetIndexer()
		podInformer.AddEventHandler(m)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			podInformer.Run(m.ctx.Done())
		}()
		syncFuncs = append(syncFuncs, podInformer.HasSynced)
	} else {
		m.podIndexer = nil
	}

	m.nodeInformer = corev1Informers.NewFilteredNodeInformer(
		m.kubeCS, nodeResyncPeriod, cache.Indexers{},
		func(opts *v1.ListOptions) {
			if m.nodeSelector != nil {
				opts.LabelSelector = m.nodeSelector.String()
			}
		},
	)
	m.nodeInformer.AddEventHandler(m)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.nodeInformer.Run(m.ctx.Done())
	}()
	syncFuncs = append(syncFuncs, m.nodeInformer.HasSynced)

	if !cache.WaitForCacheSync(m.ctx.Done(), syncFuncs...) {
		if m.ctx.Err() != nil {
			// We don't know why the context was canceled, but this can be a normal error if the
			// FloatingIPPool spec changed during initialization.
			m.log.WithError(m.ctx.Err()).Error("failed to sync dependencies; maybe spec changed")
		} else {
			m.log.Error("failed to sync dependencies")
		}
		return
	}

	// After the caches are sync'ed we need to loop through nodes again, otherwise pods which were
	// added before the node was known may be missing.
	for _, o := range m.nodeInformer.GetStore().List() {
		k8sNode, ok := o.(*corev1.Node)
		if !ok {
			m.log.Error("node informer store produced non-node")
			continue
		}
		err := m.updateNode(m.ctx, k8sNode)
		if err != nil {
			m.log.WithError(err).Error("updating node")
		}
	}
	m.Lock()
	defer m.Unlock()
	// We enable the initial set of nodes in bulk. This helps minimize chaos as the action can
	// apply these changes as a single update, and hopefully avoid disabling or moving nodes
	// which are active but not yet seen.
	var enable []*corev1.Node

	for _, n := range m.nodeNameToNode {
		if n.enabled {
			enable = append(enable, n.k8sNode)
		}
	}
	sort.Sort(byNodeName(enable)) // make this list reproducable
	m.action.EnableNodes(enable...)

	m.primed = true

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		wait.Until(m.runWorker, time.Second, m.ctx.Done())
	}()
}

func (m *Controller) runWorker() {
	for m.processNextItem() {
	}
}

func (m *Controller) processNextItem() bool {
	m.Lock()
	defer m.Unlock()

	e, term := m.queue.Get()

	if term {
		return false
	}

	defer m.queue.Done(e)

	var re *requeueError

	err := m.processItem(e.(event))
	if err == nil {
		m.queue.Forget(e)
		return true
	} else if errors.As(err, &re) {
		m.queue.Forget(e)

		if ev, ok := e.(event); ok {
			ev.requeued = true
			ev.requeuedAt = time.Now()
			m.queue.AddAfter(e, re.requeueAfter)
		}
	} else {
		m.log.Error(err.Error())
	}

	return true
}

func (m *Controller) processItem(e event) error {
	var obj interface{}
	var err error

	switch e.resourceType {
	case "node":

		if e.requeued {
			// TODO(bdjurkovic) is this correct?

			// if requeued we really should make sure we have the latest most current version of the object
			// unfortunately there is no way to check when resyc was last done to check if we resynced since
			// we got requeued
			// so we are forced to, ahem, force a cache sync
			err := m.nodeInformer.GetStore().Resync()
			if err != nil {
				return err
			}

			if !cache.WaitForCacheSync(m.ctx.Done(), m.nodeInformer.HasSynced) {
				if m.ctx.Err() != nil {
					// We don't know why the context was canceled, but this can be a normal error if the
					// FloatingIPPool spec changed during initialization.
					m.log.WithError(m.ctx.Err()).Error("failed to sync dependencies; maybe spec changed")
				} else {
					m.log.Error("failed to sync dependencies")
				}
				return m.ctx.Err()
			}
		}

		obj, _, err = m.nodeInformer.GetIndexer().GetByKey(e.key)
		if err != nil {
			return fmt.Errorf("error fetching object with key %s from store: %v", e.key, err)
		}

		n, _ := obj.(*corev1.Node)

		// we only want the stored event object if it's missing and not requeued
		// usually this should be a deleted object that's been removed from the index
		// so lets handle it properly
		// if it was requeued and it's missing we probably should not handle it
		// example we requeued toleration check but in the mean time the node has been removed
		// we will handle the delete event properly so we really should not handle the requeued event
		if n == nil && !e.requeued {
			n, _ = e.object.(*corev1.Node)
		}

		if n != nil {
			if e.eventType == eventTypeUpdate || e.eventType == eventTypeCreate {
				err = m.updateNode(m.ctx, n)
			} else if e.eventType == eventTypeDelete {
				err = m.deleteNode(n)
			}
		}
	case "pod":
		var p *corev1.Pod
		if m.podIndexer != nil {
			obj, _, err = m.podIndexer.GetByKey(e.key)
			if err != nil {
				return fmt.Errorf("error fetching object with key %s from store: %v", e.key, err)
			}

			p, _ = obj.(*corev1.Pod)
		}

		// see comment above
		if p == nil && !e.requeued {
			p, _ = e.object.(*corev1.Pod)
		}

		if p != nil {
			if e.eventType == eventTypeUpdate || e.eventType == eventTypeCreate {
				err = m.updatePod(p)
			} else if e.eventType == eventTypeDelete {
				err = m.deletePod(p)
			}
		}

	default:
		m.log.Errorf("informer emitted unexpected type: %T", e.resourceType)
	}

	return err
}

func (m *Controller) getNodePods(nodeName string) ([]*corev1.Pod, error) {
	var out []*corev1.Pod
	indexer := m.podIndexer
	items, err := indexer.ByIndex(podNodeNameIndexerName, nodeName)
	if err != nil {
		return nil, fmt.Errorf("retrieving pods: %w", err)
	}
	for _, o := range items {
		pod, ok := o.(*corev1.Pod)
		if !ok {
			return nil, fmt.Errorf("pod indexer return non-pod type %T", o)
		}
		out = append(out, pod)
	}
	return out, nil
}

func (m *Controller) deleteNode(k8sNode *corev1.Node) error {
	m.action.DisableNodes(k8sNode)
	delete(m.nodeNameToNode, k8sNode.Name)
	return nil
}

func (m *Controller) updateNode(ctx context.Context, k8sNode *corev1.Node) error {
	if !k8sNode.ObjectMeta.DeletionTimestamp.IsZero() {
		m.deleteNode(k8sNode)
		return nil
	}
	providerID := k8sNode.Spec.ProviderID
	log := m.log.WithFields(logrus.Fields{"node": k8sNode.Name, "node_provider_id": providerID})
	n, ok := m.nodeNameToNode[k8sNode.Name]
	if !ok {
		if providerID == "" {
			log.Info("node has no provider id, ignoring")
			return nil
		}
		n = newNode(k8sNode)
		m.nodeNameToNode[n.getName()] = n
		log.Info("new node")
	} else {
		n.k8sNode = k8sNode
		log.Debug("node updated")
	}

	var oldNodeMatch = n.isNodeMatch
	n.isNodeMatch = m.isNodeMatch(n)

	if oldNodeMatch == n.isNodeMatch {
		log.Debug("node match unchanged")
		return nil
	}

	if n.isNodeMatch && len(n.matchingPods) > 0 {
		// We stop tracking pods when the node doesn't match.
		n.matchingPods = make(map[string]*corev1.Pod)
	}

	if n.isNodeMatch {
		if m.match.PodNamespace != "" || m.podSelector != nil {
			podList, err := m.getNodePods(n.getName())
			if err != nil {
				return fmt.Errorf("listing node pods: %w", err)
			}
			for _, pod := range podList {
				m.updatePod(pod)
			}
			return nil // updatePod will enable the node if appropriate
		}
		log.Info("enabling node")
		n.enabled = true
		if m.primed {
			m.action.EnableNodes(n.k8sNode)
		}

		if shouldRecheck, dur := m.shouldRecheck(n); shouldRecheck && dur > 0 {
			log.Infof("requeue node %s for %+v s", n.getName(), dur.Seconds())
			return &requeueError{requeueAfter: dur}
		}
	} else {
		log.Info("disabling node")
		n.enabled = false
		// This should be idempotent, so we don't need to care if we're primed yet.
		m.action.DisableNodes(n.k8sNode)
	}

	return nil
}

func (m *Controller) updatePod(pod *corev1.Pod) error {
	log := m.log.WithFields(logrus.Fields{"pod": pod.Name, "pod_namespace": pod.Namespace})
	if pod.Spec.NodeName == "" {
		// This pod hasn't been assigned to a node. Once a pod is assigned to a node, it cannot be
		// unassigned.
		log.Debug("ignoring unscheduled pod")
		return nil
	}
	if !pod.ObjectMeta.DeletionTimestamp.IsZero() {
		m.deletePod(pod)
		return nil
	}
	log = log.WithField("node", pod.Spec.NodeName)
	n, ok := m.nodeNameToNode[pod.Spec.NodeName]
	if !ok {
		// We don't know about this node.  If primed, we should, otherwise we'log catch it
		// when the node is added.
		if m.primed {
			log.Info("pod referenced unknown node")
		}
		return nil
	}

	if !n.isNodeMatch {
		log.Debug("ignoring pod on unmatching node")
		return nil
	}
	// Pods spec & metadata (labels+namespace) are immutable. If it doesn't match now it never did.
	if m.match.PodNamespace != "" && pod.Namespace != m.match.PodNamespace {
		// This is a warning because the informer should only deliver pods in the specified namespace.
		log.Warn("unexpected pod namespace")
		return nil
	}
	if m.podSelector != nil && !m.podSelector.Matches(labels.Set(pod.Labels)) {
		// This is a warning because pod labels should be immutable, and the informer should only
		// give us matching pods.
		log.Warn("pod labels did not match; informer should not have delivered")
		return nil
	}

	podKey := podNamespacedName(pod)
	_, active := n.matchingPods[podKey]

	running := pod.Status.Phase == corev1.PodRunning
	var ready bool
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			ready = (cond.Status == corev1.ConditionTrue)
		}
	}
	log = log.WithFields(logrus.Fields{"pod_ready": ready, "pod_phase": pod.Status.Phase})
	if (ready && running) == active {
		log.Debug("pod matching state unchanged")
		return nil // no change
	}
	if ready && running {
		n.matchingPods[podKey] = pod.DeepCopy()
		if len(n.matchingPods) == 1 {
			log.Debug("enabling node; pod update met node match criteria")
			n.enabled = true
			if m.primed {
				m.action.EnableNodes(n.k8sNode)
			}
		}
	} else {
		delete(n.matchingPods, podKey)
		if len(n.matchingPods) == 0 {
			log.Debug("disabling node; updated pod no longer meets node match criteria")
			m.action.DisableNodes(n.k8sNode)
		}
	}
	return nil
}

func (m *Controller) deletePod(pod *corev1.Pod) error {
	if pod.Spec.NodeName == "" {
		return nil
	}
	n, ok := m.nodeNameToNode[pod.Spec.NodeName]
	if !ok {
		return nil
	}
	podKey := podNamespacedName(pod)
	delete(n.matchingPods, podKey)
	if len(n.matchingPods) == 0 {
		m.action.DisableNodes(n.k8sNode)
	}

	return nil
}

func (m *Controller) isNodeMatch(n *node) bool {
	var ready bool
	for _, c := range n.k8sNode.Status.Conditions {
		if c.Type == corev1.NodeReady {
			ready = (c.Status == corev1.ConditionTrue)
		}
	}
	if !ready {
		return false
	}

	if m.nodeSelector != nil && !m.nodeSelector.Matches(labels.Set(n.k8sNode.Labels)) {
		return false
	}

taintLoop:
	for _, taint := range n.k8sNode.Spec.Taints {
		for _, tol := range m.match.Tolerations {
			if tol.ToleratesTaint(&taint) {
				if (tol.TolerationSeconds != nil && *tol.TolerationSeconds != 0) && taint.TimeAdded != nil {
					// time the taint was added + toleration seconds = t2
					// if t2 is after now then we are still in tolartion window
					// otherwise we should not tolerate this taint any more
					if !taint.TimeAdded.Add(time.Duration(*tol.TolerationSeconds * int64(time.Second))).After(time.Now()) {
						return false
					}
				}
				continue taintLoop
			}
		}
		return false
	}
	return true
}

func (m *Controller) shouldRecheck(n *node) (bool, time.Duration) {
	minDuration := defaultRequeueInterval
	recheck := false
	now := time.Now()

	for _, taint := range n.k8sNode.Spec.Taints {
		for _, tol := range m.match.Tolerations {
			if tol.ToleratesTaint(&taint) {
				if (tol.TolerationSeconds != nil && *tol.TolerationSeconds != 0) && taint.TimeAdded != nil {

					ta := taint.TimeAdded
					// tilaration duration + time when taint is added is the window when we tolarete taint
					// if that window ends after now we need to recheck
					te := ta.Add(time.Duration(*tol.TolerationSeconds * int64(time.Second)))
					if te.After(now) {
						recheck = true

						// if the window ends before now + minDuration
						// means its a new min duration
						if te.Before(now.Add(minDuration)) {
							minDuration = te.Sub(now)
						}
					}
				}
			}
		}
	}

	return recheck, minDuration
}

// OnAdd implements the shared informer ResourceEventHandler for corev1.Pod & corev1.Node.
func (m *Controller) OnAdd(obj interface{}) {
	m.enqueue(obj, eventTypeCreate)
}

// OnUpdate implements the shared informer ResourceEventHandler for corev1.Pod & corev1.Node.
func (m *Controller) OnUpdate(_, obj interface{}) {
	m.enqueue(obj, eventTypeUpdate)
}

// OnDelete implements the shared informer ResourceEventHandler for corev1.Pod & corev1.Node.
func (m *Controller) OnDelete(obj interface{}) {
	m.enqueue(obj, eventTypeDelete)
}

func (m *Controller) enqueue(obj interface{}, eventType string) {
	var event event
	var err error
	var rType string

	switch obj.(type) {
	case *corev1.Node:
		rType = "node"
	case *corev1.Pod:
		rType = "pod"
	default:
		return
	}

	event.key, err = cache.MetaNamespaceKeyFunc(obj)
	event.eventType = eventType
	event.resourceType = rType
	event.object = obj

	if err == nil {
		m.queue.Add(event)
	}
}

type node struct {
	k8sNode      *corev1.Node
	isNodeMatch  bool
	matchingPods map[string]*corev1.Pod
	enabled      bool
}

func newNode(k8sNode *corev1.Node) *node {
	return &node{
		k8sNode:      k8sNode.DeepCopy(),
		matchingPods: make(map[string]*corev1.Pod),
	}
}

func (n *node) getName() string {
	return n.k8sNode.Name
}

func (n *node) getProviderID() string {
	return n.k8sNode.Spec.ProviderID
}

func podNamespacedName(pod *corev1.Pod) string {
	return fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
}

// ValidateMatch returns an error if the provided match spec is invalid.
func ValidateMatch(match *flipopv1alpha1.Match) error {
	if match.NodeLabel != "" {
		_, err := labels.Parse(match.NodeLabel)
		if err != nil {
			return fmt.Errorf("parsing node selector: %w", err)
		}
	}

	if match.PodLabel != "" {
		_, err := labels.Parse(match.PodLabel)
		if err != nil {
			return fmt.Errorf("parsing pod selector: %w", err)
		}
	}
	return nil
}

type byNodeName []*corev1.Node

func (a byNodeName) Len() int           { return len(a) }
func (a byNodeName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byNodeName) Less(i, j int) bool { return a[i].GetName() < a[j].GetName() }
