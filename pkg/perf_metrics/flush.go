package perfmetrics

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

func flushLoop() {
	for {
		interval := perf_metrics_setting.GetFlushIntervalMinutes()
		time.Sleep(time.Duration(interval) * time.Minute)
		setting := perf_metrics_setting.GetSetting()
		if !setting.Enabled {
			continue
		}
		flushCompletedBuckets()
		cleanupExpiredMetrics(setting.RetentionDays)
	}
}

func flushCompletedBuckets() {
	currentBucket := bucketStart(time.Now().Unix())
	type pendingBucket struct {
		key     bucketKey
		bucket  *atomicBucket
		drained counters
	}
	pending := make([]pendingBucket, 0)
	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if k.bucketTs >= currentBucket {
			return true
		}

		bucket := value.(*atomicBucket)
		drained := bucket.drain()
		if drained.requestCount == 0 {
			deleteOldEmptyBucket(k, key)
			return true
		}

		pending = append(pending, pendingBucket{key: k, bucket: bucket, drained: drained})
		return true
	})
	for start := 0; start < len(pending); start += model.PerfMetricWriteBatchSize {
		end := min(start+model.PerfMetricWriteBatchSize, len(pending))
		rows := make([]model.PerfMetric, 0, end-start)
		for _, item := range pending[start:end] {
			rows = append(rows, model.PerfMetric{
				ModelName:      item.key.model,
				Group:          item.key.group,
				BucketTs:       item.key.bucketTs,
				RequestCount:   item.drained.requestCount,
				SuccessCount:   item.drained.successCount,
				TotalLatencyMs: item.drained.totalLatencyMs,
				TtftSumMs:      item.drained.ttftSumMs,
				TtftCount:      item.drained.ttftCount,
				OutputTokens:   item.drained.outputTokens,
				GenerationMs:   item.drained.generationMs,
			})
		}
		if err := model.UpsertPerfMetricBatch(rows); err != nil {
			for _, item := range pending[start:end] {
				item.bucket.addCounters(item.drained)
			}
			common.SysError(common.LogText("failed to flush perf metric batch size=%d: %s", len(rows), err.Error()))
			continue
		}
		for _, item := range pending[start:end] {
			deleteOldEmptyBucket(item.key, item.key)
		}
	}
}

func deleteOldEmptyBucket(k bucketKey, rawKey any) {
	if k.bucketTs < bucketStart(time.Now().Add(-24*time.Hour).Unix()) {
		hotBuckets.Delete(rawKey)
	}
}

func cleanupExpiredMetrics(retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	if err := model.DeletePerfMetricsBefore(cutoff); err != nil {
		common.SysError(common.LogText("failed to cleanup expired perf metrics: %s", err.Error()))
	}
}
