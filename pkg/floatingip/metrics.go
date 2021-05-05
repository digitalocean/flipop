package floatingip

import (
	"math"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricNamespace = "flipop"
	metricSubsystem = "floatingippoolcontroller"
)

var (
	nodeStatusDesc = prometheus.NewDesc(
		prometheus.BuildFQName(metricNamespace, metricSubsystem, "node_status"),
		`The total count of available nodes by status.`,
		[]string{
			"namespace",
			"name",
			"provider",
			"dns",
			"status",
		},
		nil,
	)
	ipAssignmentErrorsDesc = prometheus.NewDesc(
		prometheus.BuildFQName(metricNamespace, metricSubsystem, "ip_assignment_errors"),
		`The total count of errors encountered while assigning an IP.`,
		[]string{
			"namespace",
			"name",
			"ip",
			"provider",
			"dns",
		}, nil,
	)
	ipAssignmentsDesc = prometheus.NewDesc(
		prometheus.BuildFQName(metricNamespace, metricSubsystem, "ip_assignments"),
		`The total count of assignments by IP.`,
		[]string{
			"namespace",
			"name",
			"ip",
			"provider",
			"dns",
		}, nil,
	)
	ipNodeDesc = prometheus.NewDesc(
		prometheus.BuildFQName(metricNamespace, metricSubsystem, "ip_node"),
		`Mapping of IPs to Nodes.`,
		[]string{
			"namespace",
			"name",
			"ip",
			"provider",
			"dns",
			"provider_id",
			"node",
		}, nil,
	)
	ipStateDesc = prometheus.NewDesc(
		prometheus.BuildFQName(metricNamespace, metricSubsystem, "ip_state"),
		`The state ("active", "in-progress", "error", "no-match", "unassigned", or "disabled") of a managed IP.`,
		[]string{
			"namespace",
			"name",
			"ip",
			"provider",
			"dns",
			"state",
		}, nil,
	)
	unfulfilledIPsDesc = prometheus.NewDesc(
		prometheus.BuildFQName(metricNamespace, metricSubsystem, "unfulfilled_ips"),
		`The number of desired IP addresses which have not yet been acquired.`,
		[]string{
			"namespace",
			"name",
			"provider",
			"dns",
		}, nil,
	)
)

// Describe implements prometheus.Collector.
func (c *Controller) Describe(ch chan<- *prometheus.Desc) {
	ch <- nodeStatusDesc
	ch <- ipAssignmentErrorsDesc
	ch <- ipAssignmentsDesc
	ch <- ipNodeDesc
	ch <- ipStateDesc
	ch <- unfulfilledIPsDesc
}

// Collect implements prometheus.Collector.
func (c *Controller) Collect(ch chan<- prometheus.Metric) {
	c.poolLock.Lock()
	defer c.poolLock.Unlock()
	for _, p := range c.pools {
		p.collect(ch)
	}
}

func (p *floatingIPPool) collect(ch chan<- prometheus.Metric) {
	p.ipController.lock.Lock()
	defer p.ipController.lock.Unlock()
	var dnsName string
	if p.ipController.dns != nil && p.ipController.dnsProvider != nil {
		dnsName = p.ipController.dnsProvider.RecordNameAndZoneToFQDN(
			p.ipController.dns.Zone, p.ipController.dns.RecordName)
	}
	var assignedNodes int
	for ip, s := range p.ipController.ipToStatus {
		labels := []string{
			p.namespace,
			p.name,
			ip,
			p.ipController.provider.GetProviderName(),
			dnsName,
		}
		ch <- prometheus.MustNewConstMetric(
			ipAssignmentErrorsDesc,
			prometheus.CounterValue,
			float64(s.assignmentErrors),
			labels...,
		)
		ch <- prometheus.MustNewConstMetric(
			ipAssignmentsDesc,
			prometheus.CounterValue,
			float64(s.assignments),
			labels...,
		)
		if s.nodeProviderID != "" {
			assignedNodes++
			nodeLabels := append(
				append([]string{}, labels...),
				s.nodeProviderID,
				p.ipController.providerIDToNodeName[s.nodeProviderID],
			)
			ch <- prometheus.MustNewConstMetric(
				ipNodeDesc,
				prometheus.GaugeValue,
				float64(1),
				nodeLabels...,
			)
		}
		ch <- prometheus.MustNewConstMetric(
			ipStateDesc,
			prometheus.GaugeValue,
			float64(1),
			append(labels, string(s.state))...,
		)
	}
	unfulfilledIPs := math.Max(0, float64(p.ipController.desiredIPs-len(p.ipController.ips)))
	ch <- prometheus.MustNewConstMetric(
		unfulfilledIPsDesc,
		prometheus.GaugeValue,
		unfulfilledIPs,
		p.namespace,
		p.name,
		p.ipController.provider.GetProviderName(),
		dnsName,
	)

	ch <- prometheus.MustNewConstMetric(
		nodeStatusDesc,
		prometheus.GaugeValue,
		float64(p.ipController.assignableNodes.Len()),
		p.namespace,
		p.name,
		p.ipController.provider.GetProviderName(),
		dnsName,
		"available",
	)
	ch <- prometheus.MustNewConstMetric(
		nodeStatusDesc,
		prometheus.GaugeValue,
		float64(assignedNodes),
		p.namespace,
		p.name,
		p.ipController.provider.GetProviderName(),
		dnsName,
		"assigned",
	)
}
