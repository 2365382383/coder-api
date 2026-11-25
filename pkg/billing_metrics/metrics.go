package billingmetrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var registry = prometheus.NewRegistry()

var (
	transactionAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "new_api",
			Subsystem: "billing_transaction",
			Name:      "attempts_total",
			Help:      "Billing transaction attempts by outcome.",
		},
		[]string{"operation", "result", "error_code"},
	)
	transactionRetries = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "new_api",
			Subsystem: "billing_transaction",
			Name:      "retries_total",
			Help:      "Billing transaction retries that started a new attempt.",
		},
		[]string{"operation", "error_code"},
	)
	transactionFinal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "new_api",
			Subsystem: "billing_transaction",
			Name:      "final_total",
			Help:      "Billing transactions that reached a final success or failure.",
		},
		[]string{"operation", "result", "error_code"},
	)
	transactionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "new_api",
			Subsystem: "billing_transaction",
			Name:      "duration_seconds",
			Help:      "Total billing transaction duration including retries.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"operation", "result"},
	)
	transactionAttemptDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "new_api",
			Subsystem: "billing_transaction",
			Name:      "attempt_duration_seconds",
			Help:      "Duration of each billing transaction attempt.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"operation", "result"},
	)
	transactionRetriesPerRequest = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "new_api",
			Subsystem: "billing_transaction",
			Name:      "retries_per_transaction",
			Help:      "Number of retries used by each completed billing transaction.",
			Buckets:   []float64{0, 1, 2, 3, 4, 5},
		},
		[]string{"operation", "result"},
	)
	transactionQueueWait = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "new_api",
			Subsystem: "billing_transaction",
			Name:      "queue_wait_seconds",
			Help:      "Time spent waiting for the in-process billing resource queue before a transaction starts.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"operation"},
	)
	redisEvents = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "new_api",
			Subsystem: "billing_redis",
			Name:      "events_total",
			Help:      "Redis billing events accepted by the atomic hot path.",
		},
		[]string{"phase", "result"},
	)
	redisEventDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "new_api",
			Subsystem: "billing_redis",
			Name:      "event_duration_seconds",
			Help:      "Duration of Redis billing hot path operations.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"phase", "result"},
	)
	redisMaterialized = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "new_api",
			Subsystem: "billing_redis",
			Name:      "materialized_total",
			Help:      "Redis billing stream events materialized into MySQL.",
		},
		[]string{"result"},
	)
	redisStreamPending = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "new_api",
			Subsystem: "billing_redis",
			Name:      "stream_pending",
			Help:      "Redis billing Stream entries pending materialization.",
		},
	)
	redisStreamLength = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "new_api",
			Subsystem: "billing_redis",
			Name:      "stream_length",
			Help:      "Redis billing Stream length.",
		},
	)
)

func init() {
	registry.MustRegister(
		transactionAttempts,
		transactionRetries,
		transactionFinal,
		transactionDuration,
		transactionAttemptDuration,
		transactionRetriesPerRequest,
		transactionQueueWait,
		redisEvents,
		redisEventDuration,
		redisMaterialized,
		redisStreamPending,
		redisStreamLength,
	)
}

// ObserveAttempt records one database transaction attempt. result is one of
// success, retry, or failure. A retry result means the transaction was rolled
// back and another attempt will start.
func ObserveAttempt(operation, result, errorCode string, duration time.Duration) {
	transactionAttempts.WithLabelValues(operation, result, errorCode).Inc()
	transactionAttemptDuration.WithLabelValues(operation, result).Observe(duration.Seconds())
	if result == "retry" {
		transactionRetries.WithLabelValues(operation, errorCode).Inc()
	}
}

func ObserveFinal(operation, result, errorCode string, duration time.Duration, retries int) {
	transactionFinal.WithLabelValues(operation, result, errorCode).Inc()
	transactionDuration.WithLabelValues(operation, result).Observe(duration.Seconds())
	transactionRetriesPerRequest.WithLabelValues(operation, result).Observe(float64(retries))
}

func ObserveQueueWait(operation string, duration time.Duration) {
	transactionQueueWait.WithLabelValues(operation).Observe(duration.Seconds())
}

func ObserveRedisEvent(phase, result string, duration time.Duration) {
	redisEvents.WithLabelValues(phase, result).Inc()
	redisEventDuration.WithLabelValues(phase, result).Observe(duration.Seconds())
}

func ObserveRedisMaterialized(result string) {
	redisMaterialized.WithLabelValues(result).Inc()
}

func ObserveRedisStreamState(pending, length int64) {
	redisStreamPending.Set(float64(pending))
	redisStreamLength.Set(float64(length))
}

func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}
