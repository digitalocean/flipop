package nodedns

import "github.com/prometheus/client_golang/prometheus"

const (
	metricNamespace = "flipop"
	metricSubsystem = "nodednsrecordset"
)

var (
	recordsOpts = prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "records",
		Help:      `The total count of records.`,
	}
	recordsLabels = []string{
		"namespace",
		"name",
		"provider",
		"dns",
	}
)

// Describe implements prometheus.Collector.
func (c *Controller) Describe(ch chan<- *prometheus.Desc) {
	c.metricRecords.Describe(ch)
}

// Collect implements prometheus.Collector.
func (c *Controller) Collect(ch chan<- prometheus.Metric) {
	c.metricRecords.Collect(ch)
}
