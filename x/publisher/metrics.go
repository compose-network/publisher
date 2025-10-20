package publisher

import (
	metrics2 "github.com/compose-network/publisher/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all publisher-level metrics
type Metrics struct {
	registry *metrics2.ComponentRegistry

	ConnectionsActive      prometheus.Gauge
	ConnectionsTotal       *prometheus.CounterVec
	CrossChainTransactions prometheus.Counter
	UniqueChains           *prometheus.GaugeVec
	TransactionBatchSize   prometheus.Histogram
	ErrorsTotal            *prometheus.CounterVec
	RuntimeMemoryAlloc     prometheus.Gauge
	RuntimeMemorySys       prometheus.Gauge
	RuntimeGoroutines      prometheus.Gauge
	RuntimeGCPause         prometheus.Gauge
}

// NewMetrics creates publisher metrics
func NewMetrics() *Metrics {
	reg := metrics2.NewComponentRegistry("publisher", "")

	return &Metrics{
		registry: reg,

		ConnectionsActive: reg.NewGauge(prometheus.GaugeOpts{
			Name: "connections_active",
			Help: "Number of active connections",
		}),

		ConnectionsTotal: reg.NewCounterVec(prometheus.CounterOpts{
			Name: "connections_total",
			Help: "Total number of connections",
		}, []string{"status"}),

		CrossChainTransactions: reg.NewCounter(prometheus.CounterOpts{
			Name: "cross_chain_transactions_total",
			Help: "Total number of cross-chain transactions",
		}),

		UniqueChains: reg.NewGaugeVec(prometheus.GaugeOpts{
			Name: "unique_chains_total",
			Help: "Number of unique chains seen",
		}, []string{"chain_id"}),

		TransactionBatchSize: reg.NewHistogram(prometheus.HistogramOpts{
			Name:    "transaction_batch_size",
			Help:    "Number of transactions in a batch",
			Buckets: metrics2.CountBuckets,
		}),

		ErrorsTotal: reg.NewCounterVec(prometheus.CounterOpts{
			Name: "errors_total",
			Help: "Total number of errors",
		}, []string{"type", "operation"}),

		RuntimeMemoryAlloc: reg.NewGauge(prometheus.GaugeOpts{
			Name: "runtime_memory_alloc_bytes",
			Help: "Currently allocated memory in bytes",
		}),

		RuntimeMemorySys: reg.NewGauge(prometheus.GaugeOpts{
			Name: "runtime_memory_sys_bytes",
			Help: "Total system memory in bytes",
		}),

		RuntimeGoroutines: reg.NewGauge(prometheus.GaugeOpts{
			Name: "runtime_goroutines_total",
			Help: "Number of goroutines currently running",
		}),

		RuntimeGCPause: reg.NewGauge(prometheus.GaugeOpts{
			Name: "runtime_gc_pause_duration_seconds",
			Help: "GC pause duration in seconds",
		}),
	}
}

// RecordCrossChainTransaction records a cross-chain transaction
func (m *Metrics) RecordCrossChainTransaction(batchSize int) {
	m.CrossChainTransactions.Inc()
	m.TransactionBatchSize.Observe(float64(batchSize))
}

// RecordUniqueChain records a unique chain being seen
func (m *Metrics) RecordUniqueChain(chainID string) {
	m.UniqueChains.WithLabelValues(chainID).Set(1)
}

// RecordError records an error
func (m *Metrics) RecordError(errType, operation string) {
	m.ErrorsTotal.WithLabelValues(errType, operation).Inc()
}
