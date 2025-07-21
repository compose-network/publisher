package network

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/ssvlabs/rollup-shared-publisher/pkg/metrics"
)

// Metrics holds all network-level metrics
type Metrics struct {
	registry *metrics.ComponentRegistry

	// Connection management
	ConnectionsTotal   *prometheus.CounterVec
	ConnectionsActive  prometheus.Gauge
	ConnectionDuration prometheus.Histogram

	// Message I/O
	MessagesTotal             *prometheus.CounterVec
	MessageSizeBytes          *prometheus.HistogramVec
	MessageProcessingDuration *prometheus.HistogramVec

	// Broadcast performance
	BroadcastsTotal     prometheus.Counter
	BroadcastRecipients prometheus.Histogram
	BroadcastDuration   prometheus.Histogram

	// Network errors
	ErrorsTotal *prometheus.CounterVec
}

// NewMetrics creates network metrics
func NewMetrics() *Metrics {
	reg := metrics.NewComponentRegistry("publisher", "network")

	return &Metrics{
		registry: reg,

		ConnectionsTotal: reg.NewCounterVec(prometheus.CounterOpts{
			Name: "connections_total",
			Help: "Total number of network connections",
		}, []string{"state"}),

		ConnectionsActive: reg.NewGauge(prometheus.GaugeOpts{
			Name: "connections_active",
			Help: "Number of active network connections",
		}),

		ConnectionDuration: reg.NewHistogram(prometheus.HistogramOpts{
			Name:    "connection_duration_seconds",
			Help:    "Duration of network connections",
			Buckets: metrics.NetworkBuckets,
		}),

		MessagesTotal: reg.NewCounterVec(prometheus.CounterOpts{
			Name: "messages_total",
			Help: "Total number of messages by type and direction",
		}, []string{"type", "direction"}),

		MessageSizeBytes: reg.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "message_size_bytes",
			Help:    "Size of messages in bytes",
			Buckets: metrics.SizeBuckets,
		}, []string{"type", "direction"}),

		MessageProcessingDuration: reg.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "message_processing_duration_seconds",
			Help:    "Duration of message processing",
			Buckets: metrics.DurationBuckets,
		}, []string{"type"}),

		BroadcastsTotal: reg.NewCounter(prometheus.CounterOpts{
			Name: "broadcasts_total",
			Help: "Total number of broadcast operations",
		}),

		BroadcastRecipients: reg.NewHistogram(prometheus.HistogramOpts{
			Name:    "broadcast_recipients_total",
			Help:    "Number of recipients per broadcast operation",
			Buckets: metrics.CountBuckets,
		}),

		BroadcastDuration: reg.NewHistogram(prometheus.HistogramOpts{
			Name:    "broadcast_duration_seconds",
			Help:    "Duration of broadcast operations",
			Buckets: metrics.DurationBuckets,
		}),

		ErrorsTotal: reg.NewCounterVec(prometheus.CounterOpts{
			Name: "errors_total",
			Help: "Total number of network errors",
		}, []string{"type", "operation"}),
	}
}

// RecordConnection records a network connection event
func (m *Metrics) RecordConnection(state string) {
	m.ConnectionsTotal.WithLabelValues(state).Inc()

	switch state {
	case "accepted":
		m.ConnectionsActive.Inc()
	case "closed":
		m.ConnectionsActive.Dec()
	default:
	}
}

// RecordConnectionDuration records the duration of a network connection
func (m *Metrics) RecordConnectionDuration(duration time.Duration) {
	m.ConnectionDuration.Observe(duration.Seconds())
}

// RecordMessageReceived records a received message
func (m *Metrics) RecordMessageReceived(msgType string, sizeBytes int) {
	m.MessagesTotal.WithLabelValues(msgType, "received").Inc()
	m.MessageSizeBytes.WithLabelValues(msgType, "received").Observe(float64(sizeBytes))
}

// RecordMessageSent records a sent message
func (m *Metrics) RecordMessageSent(msgType string, sizeBytes int) {
	m.MessagesTotal.WithLabelValues(msgType, "sent").Inc()
	m.MessageSizeBytes.WithLabelValues(msgType, "sent").Observe(float64(sizeBytes))
}

// RecordMessageProcessing records message processing duration
func (m *Metrics) RecordMessageProcessing(msgType string, duration time.Duration) {
	m.MessageProcessingDuration.WithLabelValues(msgType).Observe(duration.Seconds())
}

// RecordBroadcast records a broadcast operation
func (m *Metrics) RecordBroadcast(recipientCount int, duration time.Duration) {
	m.BroadcastsTotal.Inc()
	m.BroadcastRecipients.Observe(float64(recipientCount))
	m.BroadcastDuration.Observe(duration.Seconds())
}

// RecordError records a network error
func (m *Metrics) RecordError(errorType, operation string) {
	m.ErrorsTotal.WithLabelValues(errorType, operation).Inc()
}
