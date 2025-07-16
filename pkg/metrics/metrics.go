package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	ConnectionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "publisher_connections_total",
		Help: "Total number of connections",
	}, []string{"type"}) // type: accepted, closed

	ConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "publisher_connections_active",
		Help: "Number of active connections",
	})

	ConnectionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "publisher_connection_duration_seconds",
		Help:    "Connection duration in seconds",
		Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~17min
	})

	MessagesReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "publisher_messages_received_total",
		Help: "Total number of messages received",
	}, []string{"type"})

	MessagesSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "publisher_messages_sent_total",
		Help: "Total number of messages sent",
	}, []string{"type"})

	MessageSize = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "publisher_message_size_bytes",
		Help:    "Message size in bytes",
		Buckets: prometheus.ExponentialBuckets(100, 10, 7), // 100B to 100MB
	}, []string{"type", "direction"}) // direction: in, out

	MessageProcessingDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "publisher_message_processing_duration_seconds",
		Help:    "Message processing duration",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
	}, []string{"type"})

	TransactionsProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "publisher_transactions_processed_total",
		Help: "Total number of transactions processed",
	}, []string{"chain_id"})

	TransactionBatchSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "publisher_transaction_batch_size",
		Help:    "Number of transactions in a batch",
		Buckets: prometheus.LinearBuckets(1, 5, 20), // 1 to 100
	})

	BroadcastsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "publisher_broadcasts_total",
		Help: "Total number of broadcast operations",
	})

	BroadcastRecipients = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "publisher_broadcast_recipients",
		Help:    "Number of recipients per broadcast",
		Buckets: prometheus.LinearBuckets(0, 5, 20), // 0 to 100
	})

	BroadcastDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "publisher_broadcast_duration_seconds",
		Help:    "Broadcast operation duration",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
	})

	ErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "publisher_errors_total",
		Help: "Total number of errors",
	}, []string{"type", "operation"})

	Uptime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "publisher_uptime_seconds",
		Help: "Uptime in seconds",
	})

	CrossChainTransactionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "publisher_cross_chain_transactions_total",
		Help: "Total number of cross-chain transactions",
	})

	UniqueChains = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "publisher_unique_chains",
		Help: "Number of unique chains seen",
	}, []string{"chain_id"})
)

// RecordMessageReceived records a received message.
func RecordMessageReceived(msgType string, sizeBytes int) {
	MessagesReceived.WithLabelValues(msgType).Inc()
	MessageSize.WithLabelValues(msgType, "in").Observe(float64(sizeBytes))
}

// RecordMessageSent records a sent message.
func RecordMessageSent(msgType string, sizeBytes int) {
	MessagesSent.WithLabelValues(msgType).Inc()
	MessageSize.WithLabelValues(msgType, "out").Observe(float64(sizeBytes))
}

// RecordError records an error.
func RecordError(errType, operation string) {
	ErrorsTotal.WithLabelValues(errType, operation).Inc()
}
