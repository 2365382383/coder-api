package model

import (
	"context"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRedisBillingEventIsAtomicAndIdempotent(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	token := createReserveTestToken(t, 100)

	reserve := RedisBillingEventRequest{
		RequestID: "redis-billing-test", Phase: "initial", Kind: "reserve",
		UserID: user.Id, TokenID: token.Id, TokenKey: token.Key,
		WalletDelta: 30, TokenDelta: 30, PreConsumed: 30,
		FundingSource: "wallet",
	}
	require.NoError(t, ApplyRedisBillingEvent(reserve))
	require.NoError(t, ApplyRedisBillingEvent(reserve))

	settle := reserve
	settle.Phase = "settle"
	settle.Kind = "settle"
	settle.WalletDelta = 5
	settle.TokenDelta = 5
	settle.Actual = 35
	settle.CountRequest = true
	require.NoError(t, ApplyRedisBillingEvent(settle))

	refundReserve := reserve
	refundReserve.RequestID = "redis-billing-refund"
	refundReserve.WalletDelta = 10
	refundReserve.TokenDelta = 10
	refundReserve.PreConsumed = 10
	require.NoError(t, ApplyRedisBillingEvent(refundReserve))
	refund := refundReserve
	refund.Phase = "refund"
	refund.Kind = "refund"
	refund.WalletDelta = -10
	refund.TokenDelta = -10
	require.NoError(t, ApplyRedisBillingEvent(refund))

	ctx := context.Background()
	messages, err := common.RDB.XRange(ctx, redisBillingStream, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, messages, 4)
	require.NoError(t, materializeRedisBillingBatch(ctx, messages))
	require.NoError(t, materializeRedisBillingBatch(ctx, messages))

	var persistedUser User
	require.NoError(t, DB.First(&persistedUser, user.Id).Error)
	require.EqualValues(t, 65, persistedUser.Quota)
	require.EqualValues(t, 35, persistedUser.UsedQuota)
	require.EqualValues(t, 1, persistedUser.RequestCount)

	var persistedToken Token
	require.NoError(t, DB.First(&persistedToken, token.Id).Error)
	require.EqualValues(t, 65, persistedToken.RemainQuota)
	require.EqualValues(t, 35, persistedToken.UsedQuota)
	require.Equal(t, "1", server.HGet(redisBillingIdempotencyKey(reserve.RequestID, reserve.Phase), "result"))
}

func TestRedisBillingMaterializerDatabaseMatrix(t *testing.T) {
	for _, driver := range []struct {
		name    string
		dialect common.DatabaseType
		dsn     string
	}{
		{"sqlite", common.DatabaseTypeSQLite, ""},
		{"mysql", common.DatabaseTypeMySQL, os.Getenv("TEST_MYSQL_DSN")},
		{"postgres", common.DatabaseTypePostgreSQL, os.Getenv("TEST_POSTGRES_DSN")},
	} {
		t.Run(driver.name, func(t *testing.T) {
			if driver.name != "sqlite" && driver.dsn == "" {
				t.Skip("isolated TEST_" + driver.name + "_DSN is not configured")
			}
			db := openSensitiveWordMatrixDB(t, driver.name, driver.dsn)
			previous, previousLog := DB, LOG_DB
			oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(driver.dialect, driver.dialect)
			initCol()
			t.Cleanup(func() {
				DB, LOG_DB = previous, previousLog
				common.SetDatabaseTypes(oldMain, oldLog)
				initCol()
			})
			require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Channel{}, &BillingOperation{}))
			useUserCacheMiniRedis(t)
			user := User{Username: "redis-billing-matrix-" + driver.name, Quota: 100, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			token := Token{UserId: user.Id, Key: "redis-billing-matrix-key-" + driver.name,
				Status: common.TokenStatusEnabled, RemainQuota: 100, ExpiredTime: -1}
			require.NoError(t, db.Create(&token).Error)
			require.NoError(t, ApplyRedisBillingEvent(RedisBillingEventRequest{
				RequestID: "redis-billing-matrix-" + driver.name, Phase: "initial", Kind: "reserve",
				UserID: user.Id, TokenID: token.Id, TokenKey: token.Key,
				WalletDelta: 30, TokenDelta: 30, PreConsumed: 30, FundingSource: "wallet",
			}))
			require.NoError(t, ApplyRedisBillingEvent(RedisBillingEventRequest{
				RequestID: "redis-billing-matrix-" + driver.name, Phase: "settle", Kind: "settle",
				UserID: user.Id, TokenID: token.Id, TokenKey: token.Key,
				WalletDelta: 5, TokenDelta: 5, Actual: 35, CountRequest: true,
				PreConsumed: 30, FundingSource: "wallet",
			}))
			messages, err := common.RDB.XRange(context.Background(), redisBillingStream, "-", "+").Result()
			require.NoError(t, err)
			require.NoError(t, materializeRedisBillingBatch(context.Background(), messages))
			var persisted User
			require.NoError(t, db.First(&persisted, user.Id).Error)
			require.EqualValues(t, 65, persisted.Quota)
			require.EqualValues(t, 35, persisted.UsedQuota)
			require.EqualValues(t, 1, persisted.RequestCount)
			var persistedToken Token
			require.NoError(t, db.First(&persistedToken, token.Id).Error)
			require.EqualValues(t, 65, persistedToken.RemainQuota)
			require.EqualValues(t, 35, persistedToken.UsedQuota)
		})
	}
}
