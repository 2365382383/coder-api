package model

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	BatchUpdateTypeUserQuota = iota
	BatchUpdateTypeTokenQuota
	BatchUpdateTypeUsedQuota
	BatchUpdateTypeChannelUsedQuota
	BatchUpdateTypeRequestCount
	BatchUpdateTypeCount
)

var batchUpdateStores []map[int]int64
var batchUpdateLocks []sync.Mutex

func init() {
	for range BatchUpdateTypeCount {
		batchUpdateStores = append(batchUpdateStores, make(map[int]int64))
		batchUpdateLocks = append(batchUpdateLocks, sync.Mutex{})
	}
}

func InitBatchUpdater() {
	gopool.Go(func() {
		for {
			time.Sleep(time.Duration(common.BatchUpdateInterval) * time.Second)
			batchUpdate()
		}
	})
}

func addNewRecord(recordType int, id int, value int64) {
	batchUpdateLocks[recordType].Lock()
	defer batchUpdateLocks[recordType].Unlock()

	sum, err := common.AddWalletQuota(batchUpdateStores[recordType][id], value)
	if err != nil {
		common.SysError(common.LogText("batch update overflow: type=%d id=%d value=%d", recordType, id, value))
		return
	}
	batchUpdateStores[recordType][id] = sum
}

func batchUpdate() {
	hasData := false
	for i := range BatchUpdateTypeCount {
		batchUpdateLocks[i].Lock()
		if len(batchUpdateStores[i]) > 0 {
			hasData = true
		}
		batchUpdateLocks[i].Unlock()
		if hasData {
			break
		}
	}
	if !hasData {
		return
	}

	stores := make([]map[int]int64, BatchUpdateTypeCount)
	for i := range BatchUpdateTypeCount {
		batchUpdateLocks[i].Lock()
		stores[i] = batchUpdateStores[i]
		batchUpdateStores[i] = make(map[int]int64)
		batchUpdateLocks[i].Unlock()
	}

	for id, delta := range stores[BatchUpdateTypeTokenQuota] {
		err := withBillingTransaction(context.Background(), func(tx *gorm.DB) error {
			return updateTokenQuotaDeltaTx(tx, id, delta)
		})
		if err != nil {
			common.SysLog(common.LogText("failed to batch update token quota: %s", err.Error()))
			requeueBatchRecord(BatchUpdateTypeTokenQuota, id, delta)
		}
	}
	for id, delta := range stores[BatchUpdateTypeChannelUsedQuota] {
		err := withBillingTransaction(context.Background(), func(tx *gorm.DB) error {
			return updateChannelUsedQuotaTx(tx, id, delta)
		})
		if err != nil {
			common.SysLog(common.LogText("failed to batch update channel used quota: %s", err.Error()))
			requeueBatchRecord(BatchUpdateTypeChannelUsedQuota, id, delta)
		}
	}

	userQuotaStore := stores[BatchUpdateTypeUserQuota]
	usedQuotaStore := stores[BatchUpdateTypeUsedQuota]
	requestCountStore := stores[BatchUpdateTypeRequestCount]
	userIDs := make(map[int]struct{}, len(userQuotaStore)+len(usedQuotaStore)+len(requestCountStore))
	for id := range userQuotaStore {
		userIDs[id] = struct{}{}
	}
	for id := range usedQuotaStore {
		userIDs[id] = struct{}{}
	}
	for id := range requestCountStore {
		userIDs[id] = struct{}{}
	}
	for id := range userIDs {
		quota, usedQuota, requestCount := userQuotaStore[id], usedQuotaStore[id], requestCountStore[id]
		err := withBillingTransactionForUser(context.Background(), id, func(tx *gorm.DB) error {
			return updateUserQuotaAndUsageTx(tx, id, quota, usedQuota, requestCount)
		})
		if err != nil {
			common.SysLog(common.LogText("failed to batch update user quota, used quota and request count: %s", err.Error()))
			requeueBatchRecord(BatchUpdateTypeUserQuota, id, quota)
			requeueBatchRecord(BatchUpdateTypeUsedQuota, id, usedQuota)
			requeueBatchRecord(BatchUpdateTypeRequestCount, id, requestCount)
		}
	}
}

func requeueBatchRecord(recordType, id int, value int64) {
	if value != 0 {
		addNewRecord(recordType, id, value)
	}
}

func RecordExist(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

func shouldUpdateRedis(fromDB bool, err error) bool {
	return common.RedisEnabled && fromDB && err == nil
}
