package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestUserTopUpHistoryIncludesRecordsOlderThanThirtyDays(t *testing.T) {
	truncateTables(t)
	userID := 7301
	tradeNo := "TOPUP-HISTORY-OLD-7301"
	oldCreateTime := common.GetTimestamp() - 31*24*60*60

	require.NoError(t, (&TopUp{
		UserId:        userID,
		Amount:        10,
		Money:         10,
		TradeNo:       tradeNo,
		PaymentMethod: PaymentMethodBalance,
		Status:        common.TopUpStatusSuccess,
		CreateTime:    oldCreateTime,
	}).Insert())

	pageInfo := &common.PageInfo{Page: 1, PageSize: 10}
	records, total, err := GetUserTopUps(userID, pageInfo)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, records, 1)
	require.Equal(t, tradeNo, records[0].TradeNo)

	pageInfo = &common.PageInfo{Page: 1, PageSize: 10}
	records, total, err = SearchUserTopUps(userID, tradeNo, pageInfo)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, records, 1)
	require.Equal(t, tradeNo, records[0].TradeNo)
}
