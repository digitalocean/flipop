package floatingip

import (
	"container/list"
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"

	flipopv1alpha1 "github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
	"github.com/digitalocean/flipop/pkg/provider"
)

const (
	reconcilePeriod = time.Minute
)

var (
	healthyRetrySchedule = provider.RetrySchedule{5 * time.Minute}
)

// newIPFunc describes a callback used when the list of IPs is updated.
type newIPFunc func(ctx context.Context, ips []string) error

// statusUpdateFunc describes a callback when the ip/node assignment status should be updated.
type statusUpdateFunc func(ctx context.Context, status flipopv1alpha1.FloatingIPPoolStatus) error

type ipController struct {
	provider provider.Provider
	region   string

	desiredIPs  int
	disabledIPs []string
	ips         []string
	pendingIPs  []string

	onNewIPs newIPFunc

	log      logrus.FieldLogger
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	pokeChan chan struct{}
	lock     sync.Mutex

	nextRetry time.Time

	createRetrySchedule provider.RetrySchedule
	createAttempts      int
	createNextRetry     time.Time
	createError         string

	// ipToStatus tracks each IP address, including its current assignment, errors, and retries.
	ipToStatus map[string]*ipStatus
	// providerIDToIP maps a node providerID to an IP address. It retains references for nodes
	// which are not currently active, but may become active again.
	providerIDToIP map[string]string
	// providerIDToNodeName contains ONLY active nodes. It is the source of truth for which
	// node providerIDs are active.
	providerIDToNodeName map[string]string

	providerIDToRetry map[string]*retry

	assignableIPs   *orderedSet
	assignableNodes *orderedSet

	updateStatus   bool
	onStatusUpdate statusUpdateFunc
}

type ipStatus struct {
	retry
	message        string
	nodeProviderID string
	state          flipopv1alpha1.IPState
}

type retry struct {
	attempts      int
	nextRetry     time.Time
	retrySchedule provider.RetrySchedule
}

// newIPController initializes an ipController.
func newIPController(log logrus.FieldLogger, onNewIPs newIPFunc, onStatusUpdate statusUpdateFunc) *ipController {
	i := &ipController{
		log:            log,
		onNewIPs:       onNewIPs,
		onStatusUpdate: onStatusUpdate,
		pokeChan:       make(chan struct{}, 1),
	}
	i.reset()
	return i
}

func (i *ipController) reset() {
	i.ipToStatus = make(map[string]*ipStatus)
	i.providerIDToRetry = make(map[string]*retry)
	i.providerIDToIP = make(map[string]string)
	i.providerIDToNodeName = make(map[string]string)
	i.assignableIPs = newOrderedSet()
	i.assignableNodes = newOrderedSet()
}

func (i *ipController) start(ctx context.Context) {
	if i.cancel != nil {
		return
	}
	i.wg.Add(1)
	ctx, i.cancel = context.WithCancel(ctx)
	go func() {
		defer i.wg.Done()
		i.run(ctx)
	}()
}

func (i *ipController) stop() {
	if i.cancel != nil {
		i.cancel()
	}
	i.wg.Wait()
	i.cancel = nil
}

func (i *ipController) updateProvider(prov provider.Provider, region string) bool {
	if i.provider != prov || i.region != region {
		i.stop()
		i.reset()
		i.region = region
		i.provider = prov
		return true
	}
	return false
}

func (i *ipController) updateIPs(ips []string, desiredIPs int) {
	i.lock.Lock()
	defer i.lock.Unlock()
	i.disabledIPs = nil
	if desiredIPs != 0 && len(ips) > desiredIPs {
		i.disabledIPs = ips[desiredIPs:]
		ips = ips[0:desiredIPs]
	}
	if reflect.DeepEqual(ips, i.ips) &&
		(i.desiredIPs == desiredIPs || desiredIPs == 0 || (i.desiredIPs == 0 && desiredIPs == len(ips))) {
		i.desiredIPs = desiredIPs
		return
	}
	i.updateStatus = true
	if len(i.disabledIPs) != 0 { // only log if the spec changed.
		i.log.WithField("ips", i.disabledIPs).Warn("update desiredIPs < len(ips); some IPs will be disabled")
	}

	// We really only care about removed IPs. The reconciler will take care of adding new ones.
	knownIPs := make(map[string]struct{})
	for _, ip := range ips {
		if _, ok := i.ipToStatus[ip]; ok {
			knownIPs[ip] = struct{}{}
		}
	}
	for ip, status := range i.ipToStatus {
		if _, ok := knownIPs[ip]; ok {
			continue
		}
		log := i.log.WithField("ip", ip)
		if status.nodeProviderID == "" {
			log.Info("update removes ip without node assignment")
		} else {
			if nodeName, ok := i.providerIDToNodeName[status.nodeProviderID]; ok {
				log.WithField("node", nodeName).Warn("update removes ip assigned to active node")
				i.assignableNodes.Add(status.nodeProviderID, true) // This node needs reassigned ASAP.
			} else {
				// We don't unassign IPs when DisableNodes is called, we just mark the ip as assignable.
				log.Info("update removes ip assigned to inactive node")
			}

			i.providerIDToIP[status.nodeProviderID] = ""
			delete(i.providerIDToRetry, status.nodeProviderID)
		}
		i.assignableIPs.Delete(ip)
		delete(i.ipToStatus, ip)
	}
	i.ips = ips
	i.desiredIPs = desiredIPs
	i.poke()
	i.log.Info("ip configuration updated")
	return
}

// Run will start reconciliation of floating IPs until the context is canceled.
func (i *ipController) run(ctx context.Context) {
	i.log.Info("ipController reconciler started")
	i.reconcile(ctx)
	retryTimer := time.NewTimer(i.retryTimerDuration())
	for {
		select {
		case <-ctx.Done():
			return
		case <-i.pokeChan:
			retryTimer.Stop() // need to drain the timer
			i.reconcile(ctx)
		case <-retryTimer.C:
			i.reconcile(ctx)
		}
		retryTimer.Reset(i.retryTimerDuration())
	}
}

// retryTimerDuration converts our nextRetry timestamp to a duration from now.
func (i *ipController) retryTimerDuration() time.Duration {
	dur := time.Until(i.nextRetry)
	if dur < 0 {
		i.log.Debug("ipController reconciliation will retry immediately")
		return 0
	}
	i.log.WithField("pause", dur.String()).Debug("ipController reconciliation scheduled")
	return dur
}

func (i *ipController) reconcile(ctx context.Context) {
	i.lock.Lock()
	defer i.lock.Unlock()
	i.log.Debug("ipController beginning reconciliation")
	defer i.log.Debug("ipController finished reconciliation")

	i.nextRetry = time.Now().Add(reconcilePeriod)

	i.reconcileDesiredIPs(ctx)
	i.reconcilePendingIPs(ctx)
	i.reconcileIPStatus(ctx)
	i.reconcileAssignment(ctx)

	if i.updateStatus && i.onStatusUpdate != nil {
		err := i.onStatusUpdate(ctx, i.buildStatusUpdate())
		if err != nil {
			i.log.WithError(err).Error("updating status")
			return
		}
		i.updateStatus = false
	}
}

func (i *ipController) retry(next time.Time) {
	if i.nextRetry.IsZero() || i.nextRetry.After(next) {
		i.nextRetry = next
	}
}

func (i *ipController) reconcileDesiredIPs(ctx context.Context) {
	if ctx.Err() != nil {
		return // short-circuit on context cancel.
	}
	if i.createNextRetry.After(time.Now()) {
		i.retry(i.createNextRetry)
		return
	}
	// Acquire new IPs if needed. If this fails, we can try again next reconcile.
	for j := len(i.ips); j < i.desiredIPs; j++ {
		i.log.Info("requesting ip from provider")
		i.updateStatus = true
		ip, err := i.provider.CreateIP(ctx, i.region)
		if err != nil {
			i.createRetrySchedule = provider.ErrorToRetrySchedule(err)
			i.createAttempts, i.createNextRetry = i.createRetrySchedule.Next(i.createAttempts)
			i.retry(i.createNextRetry)
			i.log.WithError(err).Error("requesting new IP from provider")
			i.createError = fmt.Sprintf("creating new ip with provider: %s", err)
			return
		}
		i.log.WithField("ip", ip).Info("created new ip with provider")
		i.pendingIPs = append(i.pendingIPs, ip)
		i.createAttempts = 0
		i.createError = ""
		i.updateStatus = true
	}
}

func (i *ipController) reconcilePendingIPs(ctx context.Context) {
	if ctx.Err() != nil {
		return // short-circuit on context cancel.
	}
	if len(i.pendingIPs) == 0 {
		return
	}
	allIPs := make([]string, 0, len(i.ips)+len(i.pendingIPs))
	copy(allIPs, i.ips)
	allIPs = append(allIPs, i.pendingIPs...)
	if i.onNewIPs != nil {
		log := i.log.WithField("ips", i.pendingIPs)
		log.Info("updating IPs with caller")
		err := i.onNewIPs(ctx, allIPs)
		if err != nil {
			log.WithError(err).Error("updating IPs with caller")
			return
		}
	}
	i.updateStatus = true
	for _, ip := range i.pendingIPs {
		// shortcut lookup for ip provider
		i.ipToStatus[ip] = &ipStatus{
			retry: retry{retrySchedule: healthyRetrySchedule},
			state: flipopv1alpha1.IPStateUnassigned,
		}
		// This IP is empty and should be a priority for assignment, put it at the front.
		i.assignableIPs.Add(ip, true)
	}
	i.ips = allIPs
	i.pendingIPs = nil
}

func (i *ipController) reconcileIPStatus(ctx context.Context) {
	for _, ip := range i.ips {
		if ctx.Err() != nil {
			return // short-circuit on context cancel.
		}

		status, ipInitialized := i.ipToStatus[ip]
		if !ipInitialized {
			status = &ipStatus{
				retry: retry{retrySchedule: provider.RetryFast},
			}
			i.ipToStatus[ip] = status
		}

		if status.nextRetry.After(time.Now()) {
			i.retry(status.nextRetry)
			continue
		}

		originalState := status.state
		originalMessage := status.message

		expectedProviderID := status.nodeProviderID
		log := i.log.WithField("ip", ip)
		log.Debug("retrieving IP current provider ID")
		providerID, err := i.provider.IPToProviderID(ctx, ip)
		if err != nil {
			if err == provider.ErrNotFound {
				// If the IP's not found, try to do the best we can. We'log continue to check its
				// status according to the retry schedule. If it recovers, it should be added back.
				oldProviderID := status.nodeProviderID
				status.nodeProviderID = ""
				delete(i.providerIDToIP, oldProviderID)
				if nodeName, ok := i.providerIDToNodeName[oldProviderID]; ok {
					i.assignableNodes.Add(oldProviderID, true)
					log.WithField("node", nodeName).Error("ip not found; node will be reassigned")
				} else {
					log.Error("ip not found; ip will be removed from assignable")
					i.assignableIPs.Delete(ip)
				}
			}
			if err == provider.ErrInProgress {
				status.state = flipopv1alpha1.IPStateInProgress
				status.message = ""
			} else {
				status.state = flipopv1alpha1.IPStateError
				status.message = fmt.Sprintf("retrieving IPs current provider ID: %s", err)
			}
			status.retrySchedule = provider.ErrorToRetrySchedule(err)
			status.attempts, status.nextRetry = status.retrySchedule.Next(status.attempts)
			i.retry(status.nextRetry)
			log.WithError(err).Error("retrieving IPs current provider ID")
			if originalState != status.state || originalMessage != status.message {
				i.updateStatus = true
			}
			continue
		}
		log = log.WithField("provider_id", providerID)

		var isProviderIDActiveNode bool
		if providerID == "" {
			// This IP isn't pointed anywhere, mark it as available for assignment.
			i.assignableIPs.Add(ip, true)
			log.Info("ip address is available for assignment")
		} else {
			var nodeName string
			nodeName, isProviderIDActiveNode = i.providerIDToNodeName[providerID]
			if isProviderIDActiveNode {
				log = log.WithField("node", nodeName)
			}
		}

		if expectedProviderID != providerID {
			// Update our records to reflect reality.
			status.nodeProviderID = providerID

			if !ipInitialized {
				if isProviderIDActiveNode {
					i.assignableNodes.Delete(providerID)
					log.Info("ip address has existing assignment, reusing")
				} else {
					// The IP references a node we don't know about yet.
					log.Info("ip address has existing assignment, but is available")
					i.assignableIPs.Add(ip, false)
				}
			}

			expectedIP := i.providerIDToIP[providerID]
			if expectedIP != "" && expectedIP != ip {
				log.WithField("expected_ip", expectedIP).
					Warn("node assignment mismatch; updating cache to reflect provider")
				i.assignableIPs.Add(expectedIP, false)
				// mark the node's old IP for immediate retry.
				i.ipToStatus[expectedIP] = &ipStatus{
					state:          flipopv1alpha1.IPStateError,
					retry:          retry{retrySchedule: provider.RetryFast},
					message:        "state unknown; cache / provider mismatch",
					nodeProviderID: "", // reset
				}
			}
			i.providerIDToIP[providerID] = ip

			delete(i.providerIDToIP, expectedProviderID)
			if evictedNodeName, ok := i.providerIDToNodeName[providerID]; ok {
				log.WithFields(logrus.Fields{
					"node": evictedNodeName,
					"ip":   expectedIP,
				}).Info("nodes ip was claimed by other node; marking for reassignment")
				i.assignableNodes.Add(expectedProviderID, true)
			}
		}

		switch {
		case status.nodeProviderID != "" && i.providerIDToNodeName[status.nodeProviderID] != "":
			status.state = flipopv1alpha1.IPStateActive
		case status.nodeProviderID != "":
			status.state = flipopv1alpha1.IPStateNoMatch
		default:
			status.state = flipopv1alpha1.IPStateUnassigned
		}
		status.message = ""
		status.attempts = 0
		status.retrySchedule = healthyRetrySchedule
		_, status.nextRetry = status.retrySchedule.Next(status.attempts)
		if originalState != status.state || originalMessage != status.message {

			i.updateStatus = true
		}
		log.Debug("provider ip mapping verified")
	}
}

func (i *ipController) reconcileAssignment(ctx context.Context) {
	var retryIPs, retryProviders []string
	defer func() {
		// Requeue anything we skipped or errored on.
		for _, ip := range retryIPs {
			i.assignableIPs.Add(ip, false)
		}
		for _, providerID := range retryProviders {
			i.assignableNodes.Add(providerID, false)
		}
	}()
	for i.assignableIPs.Len() != 0 && i.assignableNodes.Len() != 0 {
		if ctx.Err() != nil {
			return // short-circuit on context cancel.
		}

		ip := i.assignableIPs.Front()

		// If this IP was previously involved in an error we shouldn't attempt to try again before
		// its retry timestamp.
		status := i.ipToStatus[ip]
		if !status.nextRetry.IsZero() && !status.nextRetry.After(time.Now()) {
			retryIPs = append(retryIPs, ip)
			i.retry(status.nextRetry)
			continue
		}

		providerID := i.assignableNodes.Front()

		// Similarly, if this node was involved in an error we should wait until after its retry
		// timestamp has elapsed.
		nRetry, ok := i.providerIDToRetry[providerID]
		if ok && !nRetry.nextRetry.IsZero() && !nRetry.nextRetry.After(time.Now()) {
			retryIPs = append(retryIPs, ip)
			retryProviders = append(retryProviders, providerID)
			i.retry(nRetry.nextRetry)
			continue
		}

		originalState := status.state
		originalMessage := status.message

		oldProviderID := status.nodeProviderID
		// This IP may have been released by a different node. We gave the old node a chance to
		// recover, but this new node needs an IP. Remove the old node's claim it one exists.
		delete(i.providerIDToIP, oldProviderID)
		status.nodeProviderID = providerID

		// record the assignment now, but also record it as pending
		i.providerIDToIP[providerID] = ip

		log := i.log.WithFields(logrus.Fields{
			"ip":         ip,
			"providerID": providerID,
		})
		log.Info("assigning IP to node")

		err := i.provider.AssignIP(ctx, ip, providerID)
		if err == nil || err == provider.ErrInProgress {
			status.message = ""
			status.state = flipopv1alpha1.IPStateInProgress
			status.retrySchedule = provider.RetryFast
			status.attempts = 0
			delete(i.providerIDToRetry, providerID)
			_, status.nextRetry = status.retrySchedule.Next(status.attempts)
		} else {
			status.state = flipopv1alpha1.IPStateError
			status.retrySchedule = provider.ErrorToRetrySchedule(err)
			status.message = fmt.Sprintf("assigning IP to node: %s", err)
			log.WithError(err).Error("assigning IP to node")
			if nRetry == nil {
				nRetry = &retry{}
			}
			nRetry.attempts, nRetry.nextRetry = nRetry.retrySchedule.Next(nRetry.attempts)
			i.providerIDToRetry[providerID] = nRetry
			i.retry(nRetry.nextRetry)
		}
		i.retry(status.nextRetry)
		if originalState != status.state || originalMessage != status.message {
			i.updateStatus = true
		}
	}
}

func (i *ipController) DisableNodes(nodes ...*corev1.Node) {
	i.lock.Lock()
	defer i.lock.Unlock()
	for _, node := range nodes {
		providerID := node.Spec.ProviderID
		if providerID == "" {
			continue
		}

		if _, ok := i.providerIDToNodeName[providerID]; !ok {
			continue // Wasn't enabled
		}
		log := i.log.WithFields(logrus.Fields{
			"node":        node.Name,
			"provider_id": providerID,
		})
		i.updateStatus = true
		delete(i.providerIDToNodeName, providerID)
		if ip := i.providerIDToIP[providerID]; ip != "" {
			// Add this IP to the back of the list. This increases the chances that the IP mapping
			// can be retained if the node recovers.
			i.assignableIPs.Add(ip, false)
			// cancel any pending retries
			status := i.ipToStatus[ip]
			status.state = flipopv1alpha1.IPStateNoMatch
			status.attempts = 0
			status.message = ""
			status.retrySchedule = provider.RetrySlow
			_, status.nextRetry = status.retrySchedule.Next(status.attempts)
			log.WithField("ip", ip).Info("node disabled; ip added to assignable list")
		} else {
			log.Info("node disabled")
		}
		i.assignableNodes.Delete(providerID)
		// We leave the providerID<->IP mappings in providerIDToIP/ipStatus.nodeProviderID so we can
		// reuse the IP mapping, if it's not immediately recovered.
		i.poke()
	}
}

func (i *ipController) EnableNodes(nodes ...*corev1.Node) {
	i.lock.Lock()
	defer i.lock.Unlock()

	for _, node := range nodes {
		providerID := node.Spec.ProviderID
		if providerID == "" {
			continue
		}
		if _, ok := i.providerIDToNodeName[providerID]; ok {
			continue // Already enabled.
		}
		i.poke()
		i.updateStatus = true
		i.providerIDToNodeName[providerID] = node.Name
		log := i.log.WithFields(logrus.Fields{
			"node":        node.Name,
			"provider_id": providerID,
		})
		if ip := i.providerIDToIP[providerID]; ip != "" {
			log.WithField("ip", ip).Info("enabling node; already assigned to ip")
			status := i.ipToStatus[ip]
			status.nodeProviderID = providerID // should already be set.
			status.state = flipopv1alpha1.IPStateInProgress
			status.message = ""
			status.retrySchedule = provider.RetryFast
			status.attempts = 0
			_, status.nextRetry = status.retrySchedule.Next(status.attempts)
			i.assignableIPs.Delete(ip)
			continue // Already has an IP.
		}
		log.Info("enabling node; submitted to assignable node queue")
		i.assignableNodes.Add(providerID, false)
	}
}

func (i *ipController) poke() {
	select {
	case i.pokeChan <- struct{}{}:
	default: // if there's already a poke in queued, we don't need another.
	}
}

func (i *ipController) buildStatusUpdate() flipopv1alpha1.FloatingIPPoolStatus {
	status := flipopv1alpha1.FloatingIPPoolStatus{
		IPs:   make(map[string]flipopv1alpha1.IPStatus),
		Error: i.createError,
	}
	for ip, ipStatus := range i.ipToStatus {
		status.IPs[ip] = flipopv1alpha1.IPStatus{
			ProviderID: ipStatus.nodeProviderID,
			NodeName:   i.providerIDToNodeName[ipStatus.nodeProviderID],
			State:      ipStatus.state,
			Error:      ipStatus.message,
		}
	}
	for _, ip := range i.disabledIPs {
		status.IPs[ip] = flipopv1alpha1.IPStatus{
			State: flipopv1alpha1.IPStateDisabled,
		}
	}
	for providerID := range i.providerIDToRetry {
		nodeName, ok := i.providerIDToNodeName[providerID]
		if !ok {
			continue
		}
		status.NodeErrors = append(status.NodeErrors, nodeName)
	}
	for _, providerID := range i.assignableNodes.AsList() {
		status.AssignableNodes = append(status.AssignableNodes, i.providerIDToNodeName[providerID])
	}
	return status
}

type orderedSet struct {
	l *list.List
	m map[string]*list.Element
}

func newOrderedSet() *orderedSet {
	return &orderedSet{
		l: list.New(),
		m: make(map[string]*list.Element),
	}
}

// Add v to the s, if it doesn't already exist. If front is true it will be
// added/moved to the front, otherwise its added to the end.
func (o *orderedSet) Add(v string, front bool) {
	e, ok := o.m[v]
	if ok {
		if front {
			o.l.MoveToFront(e)
		}
		return
	}
	if front {
		o.m[v] = o.l.PushFront(v)
	} else {
		o.m[v] = o.l.PushBack(v)
	}
}

// Front returns the first item in the set, or "" if the set is empty.
func (o *orderedSet) Front() string {
	e := o.l.Front()
	if e == nil {
		return ""
	}
	v := e.Value.(string)
	delete(o.m, v)
	o.l.Remove(e)
	return v
}

// Len returns the length of the set.
func (o *orderedSet) Len() int {
	return o.l.Len()
}

// Delete removes v from the set.
func (o *orderedSet) Delete(v string) bool {
	e, ok := o.m[v]
	if !ok {
		return false
	}
	delete(o.m, v)
	o.l.Remove(e)
	return true
}

// IsSet returns true if v is in the set.
func (o *orderedSet) IsSet(v string) bool {
	_, ok := o.m[v]
	if !ok {
		return false
	}
	return true
}

// AsList returns all items in the set.
func (o *orderedSet) AsList() []string {
	var out []string
	for _, e := range o.m {
		out = append(out, e.Value.(string))
	}
	return out
}
