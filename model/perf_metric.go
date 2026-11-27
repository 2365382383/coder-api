package model

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PerfMetric stores aggregated relay performance metrics for the model square.
type PerfMetric struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	ModelName      string `json:"model_name" gorm:"size:128;uniqueIndex:idx_perf_model_group_bucket,priority:1"`
	Group          string `json:"group" gorm:"column:group;size:64;uniqueIndex:idx_perf_model_group_bucket,priority:2"`
	BucketTs       int64  `json:"bucket_ts" gorm:"uniqueIndex:idx_perf_model_group_bucket,priority:3;index:idx_perf_bucket_ts"`
	RequestCount   int64  `json:"-" gorm:"default:0"`
	SuccessCount   int64  `json:"-" gorm:"default:0"`
	TotalLatencyMs int64  `json:"-" gorm:"default:0"`
	TtftSumMs      int64  `json:"-" gorm:"default:0"`
	TtftCount      int64  `json:"-" gorm:"default:0"`
	OutputTokens   int64  `json:"-" gorm:"default:0"`
	GenerationMs   int64  `json:"-" gorm:"default:0"`
}

func (PerfMetric) TableName() string {
	return "perf_metrics"
}

const PerfMetricWriteBatchSize = 200

func perfMetricUpsertClause() clause.OnConflict {
	add := func(column string) any {
		if DB != nil && DB.Dialector.Name() == "postgres" {
			return gorm.Expr(fmt.Sprintf("perf_metrics.%s + EXCLUDED.%s", column, column))
		}
		if DB != nil && DB.Dialector.Name() == "sqlite" {
			return gorm.Expr(fmt.Sprintf("perf_metrics.%s + excluded.%s", column, column))
		}
		return gorm.Expr(fmt.Sprintf("perf_metrics.%s + VALUES(%s)", column, column))
	}
	return clause.OnConflict{
		Columns: []clause.Column{
			{Name: "model_name"},
			{Name: "group"},
			{Name: "bucket_ts"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"request_count":    add("request_count"),
			"success_count":    add("success_count"),
			"total_latency_ms": add("total_latency_ms"),
			"ttft_sum_ms":      add("ttft_sum_ms"),
			"ttft_count":       add("ttft_count"),
			"output_tokens":    add("output_tokens"),
			"generation_ms":    add("generation_ms"),
		}),
	}
}

func UpsertPerfMetric(metric *PerfMetric) error {
	if metric == nil || metric.RequestCount == 0 {
		return nil
	}
	return UpsertPerfMetricBatch([]PerfMetric{*metric})
}

// UpsertPerfMetricBatch writes one bounded batch in one transaction. Callers
// should retry failed batches independently so a successful batch is never
// applied twice when a later batch fails.
func UpsertPerfMetricBatch(metrics []PerfMetric) error {
	if len(metrics) == 0 {
		return nil
	}
	if len(metrics) > PerfMetricWriteBatchSize {
		return errors.New("perf metric batch exceeds the write limit")
	}
	return DB.Clauses(perfMetricUpsertClause()).Create(&metrics).Error
}

func GetPerfMetrics(modelName string, group string, allowedGroups []string, startTs int64, endTs int64) ([]PerfMetric, error) {
	var metrics []PerfMetric
	query := DB.Model(&PerfMetric{}).
		Where("model_name = ? AND bucket_ts >= ? AND bucket_ts <= ?", modelName, startTs, endTs)
	if group != "" {
		query = query.Where(commonGroupCol+" = ?", group)
	} else if allowedGroups != nil {
		if len(allowedGroups) == 0 {
			return metrics, nil
		}
		query = query.Where(commonGroupCol+" IN ?", allowedGroups)
	}
	err := query.Order("bucket_ts ASC").Find(&metrics).Error
	return metrics, err
}

type PerfMetricSummary struct {
	ModelName      string `json:"model_name"`
	RequestCount   int64  `json:"request_count"`
	SuccessCount   int64  `json:"success_count"`
	TotalLatencyMs int64  `json:"total_latency_ms"`
	OutputTokens   int64  `json:"output_tokens"`
	GenerationMs   int64  `json:"generation_ms"`
}

type PerfMetricSummaryBucket struct {
	ModelName      string `json:"model_name"`
	BucketTs       int64  `json:"bucket_ts"`
	RequestCount   int64  `json:"request_count"`
	SuccessCount   int64  `json:"success_count"`
	TotalLatencyMs int64  `json:"total_latency_ms"`
	OutputTokens   int64  `json:"output_tokens"`
	GenerationMs   int64  `json:"generation_ms"`
}

func GetPerfMetricsSummaryAll(startTs int64, endTs int64, groups []string) ([]PerfMetricSummary, error) {
	var summaries []PerfMetricSummary
	query := DB.Model(&PerfMetric{}).
		Select("model_name, SUM(request_count) as request_count, SUM(success_count) as success_count, SUM(total_latency_ms) as total_latency_ms, SUM(output_tokens) as output_tokens, SUM(generation_ms) as generation_ms").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if groups != nil {
		if len(groups) == 0 {
			return summaries, nil
		}
		query = query.Where(commonGroupCol+" IN ?", groups)
	}
	err := query.
		Group("model_name").
		Having("SUM(request_count) > 0").
		Find(&summaries).Error
	return summaries, err
}

func GetPerfMetricsSummaryBucketsAll(startTs int64, endTs int64, groups []string) ([]PerfMetricSummaryBucket, error) {
	var summaries []PerfMetricSummaryBucket
	query := DB.Model(&PerfMetric{}).
		Select("model_name, bucket_ts, SUM(request_count) as request_count, SUM(success_count) as success_count, SUM(total_latency_ms) as total_latency_ms, SUM(output_tokens) as output_tokens, SUM(generation_ms) as generation_ms").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if groups != nil {
		if len(groups) == 0 {
			return summaries, nil
		}
		query = query.Where(commonGroupCol+" IN ?", groups)
	}
	err := query.
		Group("model_name, bucket_ts").
		Having("SUM(request_count) > 0").
		Find(&summaries).Error
	return summaries, err
}

// GetRecentPerfMetricSuccessBuckets reads the latest nonempty hours within
// the supplied history window, independently of the summary's reporting window.
func GetRecentPerfMetricSuccessBuckets(modelName string, startTs int64, endTs int64, groups []string, limit int) ([]PerfMetricSummaryBucket, error) {
	if modelName == "" || limit <= 0 || (groups != nil && len(groups) == 0) {
		return nil, nil
	}
	const hourBucket = "bucket_ts - (bucket_ts % 3600)"
	query := DB.Model(&PerfMetric{}).
		Select("model_name, "+hourBucket+" AS bucket_ts, SUM(request_count) AS request_count, SUM(success_count) AS success_count").
		Where("model_name = ? AND bucket_ts >= ? AND bucket_ts <= ? AND request_count > 0", modelName, startTs, endTs)
	if groups != nil {
		query = query.Where(commonGroupCol+" IN ?", groups)
	}
	var buckets []PerfMetricSummaryBucket
	err := query.Group("model_name, " + hourBucket).
		Order("bucket_ts DESC").Limit(limit).Find(&buckets).Error
	return buckets, err
}

// GetRecentPerfMetricSuccessBucketsBatch loads the recent success history for
// all requested models in one query. The caller applies the per-model sample
// limit after aggregation, avoiding one database round trip per model.
func GetRecentPerfMetricSuccessBucketsBatch(modelNames []string, startTs int64, endTs int64, groups []string) ([]PerfMetricSummaryBucket, error) {
	if len(modelNames) == 0 || (groups != nil && len(groups) == 0) {
		return nil, nil
	}
	const hourBucket = "bucket_ts - (bucket_ts % 3600)"
	query := DB.Model(&PerfMetric{}).
		Select("model_name, "+hourBucket+" AS bucket_ts, SUM(request_count) AS request_count, SUM(success_count) AS success_count").
		Where("model_name IN ? AND bucket_ts >= ? AND bucket_ts <= ? AND request_count > 0", modelNames, startTs, endTs)
	if groups != nil {
		query = query.Where(commonGroupCol+" IN ?", groups)
	}
	var buckets []PerfMetricSummaryBucket
	err := query.Group("model_name, " + hourBucket).
		Order("model_name ASC, bucket_ts DESC").Find(&buckets).Error
	return buckets, err
}

func DeletePerfMetricsBefore(cutoffTs int64) error {
	if cutoffTs <= 0 {
		return nil
	}
	return DB.Where("bucket_ts < ?", cutoffTs).Delete(&PerfMetric{}).Error
}

func PerfMetricStartTime(hours int) int64 {
	if hours <= 0 {
		hours = 24
	}
	return time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
}
