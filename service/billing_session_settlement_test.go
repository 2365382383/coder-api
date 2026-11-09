package service

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

type billingSettlementFixture struct {
	userID    int
	tokenID   int
	subID     int
	planID    int
	channelID int
	requestID string
	info      *relaycommon.RelayInfo
}

func seedBillingSettlementFixture(t *testing.T, preference string, walletQuota int64) billingSettlementFixture {
	t.Helper()
	truncate(t)

	fixture := billingSettlementFixture{
		userID:    4101,
		tokenID:   4102,
		subID:     4103,
		planID:    4104,
		channelID: 4105,
		requestID: "settlement-overflow-" + preference,
	}
	seedUser(t, fixture.userID, walletQuota)
	setting, err := json.Marshal(dto.UserSetting{BillingPreference: preference})
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", fixture.userID).Update("setting", string(setting)).Error)
	seedToken(t, fixture.tokenID, fixture.userID, "sk-"+fixture.requestID, 1_000)
	seedChannel(t, fixture.channelID)
	seedSubscriptionPlan(t, fixture.planID, true)
	seedSubscriptionWithPlan(t, fixture.subID, fixture.userID, fixture.planID, 1_000, 950, true)

	fixture.info = &relaycommon.RelayInfo{
		UserId:          fixture.userID,
		TokenId:         fixture.tokenID,
		TokenKey:        "sk-" + fixture.requestID,
		RequestId:       fixture.requestID,
		OriginModelName: "test-model",
		UsingGroup:      "default",
		UserSetting:     dto.UserSetting{BillingPreference: preference},
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: fixture.channelID},
	}
	return fixture
}

func TestBillingSettlementConsumesSubscriptionRemainderThenWallet(t *testing.T) {
	fixture := seedBillingSettlementFixture(t, "subscription_first", 1_000)
	session, apiErr := NewBillingSession(newBillingTestContext(1_000), fixture.info, 40)
	require.Nil(t, apiErr)
	require.Equal(t, BillingSourceSubscription, fixture.info.BillingSource)
	require.EqualValues(t, 990, getSubscriptionUsed(t, fixture.subID))

	require.NoError(t, session.Settle(70))

	require.EqualValues(t, 1_000, getSubscriptionUsed(t, fixture.subID))
	require.EqualValues(t, 980, getUserQuota(t, fixture.userID))
	require.EqualValues(t, 930, getTokenRemainQuota(t, fixture.tokenID))
	require.Equal(t, BillingSourceMixed, fixture.info.BillingSource)
	require.Len(t, fixture.info.BillingAllocations, 2)
	require.Equal(t, BillingSourceSubscription, fixture.info.BillingAllocations[0].Source)
	require.Equal(t, 50, fixture.info.BillingAllocations[0].Quota)
	require.EqualValues(t, 1_000, fixture.info.BillingAllocations[0].SubscriptionAmountUsedAfterConsume)
	require.Equal(t, BillingSourceWallet, fixture.info.BillingAllocations[1].Source)
	require.Equal(t, 20, fixture.info.BillingAllocations[1].Quota)

	var root model.BillingOperation
	require.NoError(t, model.DB.Where("request_id = ? AND phase = ?", fixture.requestID, "initial").First(&root).Error)
	require.Equal(t, model.BillingSettled, root.State)
	require.Equal(t, "mixed", root.FundingSource)
	require.EqualValues(t, 20, root.WalletAmount)
	require.EqualValues(t, 50, root.SubscriptionAmount)
	require.EqualValues(t, 70, root.Actual)

	var intent model.BillingOperation
	require.NoError(t, model.DB.Where("request_id = ? AND phase = ?", fixture.requestID, "settlement_request").First(&intent).Error)
	require.Equal(t, model.BillingSettlementWalletFallback, intent.FundingSource)

	var settled model.BillingOperation
	require.NoError(t, model.DB.Where("request_id = ? AND phase = ?", fixture.requestID, "settle").First(&settled).Error)
	require.EqualValues(t, 20, settled.WalletAmount)
	require.EqualValues(t, 10, settled.SubscriptionAmount)
	require.EqualValues(t, 30, settled.TokenAmount)

	var user model.User
	require.NoError(t, model.DB.First(&user, fixture.userID).Error)
	require.EqualValues(t, 70, user.UsedQuota)
	require.EqualValues(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, fixture.channelID).Error)
	require.EqualValues(t, 70, channel.UsedQuota)

	require.NoError(t, model.RecoverBillingSettlement(fixture.requestID))
	require.EqualValues(t, 980, getUserQuota(t, fixture.userID))
}

func TestBillingSettlementWalletFallbackMayCreateDebt(t *testing.T) {
	fixture := seedBillingSettlementFixture(t, "subscription_first", 0)
	session, apiErr := NewBillingSession(newBillingTestContext(1_000), fixture.info, 40)
	require.Nil(t, apiErr)

	require.NoError(t, session.Settle(70))
	require.EqualValues(t, 1_000, getSubscriptionUsed(t, fixture.subID))
	require.EqualValues(t, -20, getUserQuota(t, fixture.userID))
	require.EqualValues(t, 930, getTokenRemainQuota(t, fixture.tokenID))
}

func TestBillingSettlementWalletFirstFallbackMayUseWalletDebt(t *testing.T) {
	fixture := seedBillingSettlementFixture(t, "wallet_first", 0)
	session, apiErr := NewBillingSession(newBillingTestContext(1_000), fixture.info, 40)
	require.Nil(t, apiErr)
	require.Equal(t, BillingSourceSubscription, fixture.info.BillingSource)

	require.NoError(t, session.Settle(70))
	require.EqualValues(t, 1_000, getSubscriptionUsed(t, fixture.subID))
	require.EqualValues(t, -20, getUserQuota(t, fixture.userID))
	require.EqualValues(t, 930, getTokenRemainQuota(t, fixture.tokenID))
}

func TestBillingSettlementMixedPreconsumeChargesPositiveDeltaToWallet(t *testing.T) {
	fixture := seedBillingSettlementFixture(t, "subscription_first", 1_000)
	session, apiErr := NewBillingSession(newBillingTestContext(1_000), fixture.info, 100)
	require.Nil(t, apiErr)
	require.Equal(t, BillingSourceMixed, fixture.info.BillingSource)
	require.EqualValues(t, 1_000, getSubscriptionUsed(t, fixture.subID))
	require.EqualValues(t, 950, getUserQuota(t, fixture.userID))

	require.NoError(t, session.Settle(130))
	require.EqualValues(t, 1_000, getSubscriptionUsed(t, fixture.subID))
	require.EqualValues(t, 920, getUserQuota(t, fixture.userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, fixture.tokenID))
	require.Len(t, fixture.info.BillingAllocations, 2)
	require.Equal(t, 50, fixture.info.BillingAllocations[0].Quota)
	require.Equal(t, 80, fixture.info.BillingAllocations[1].Quota)
}

func TestBillingSettlementSubscriptionOnlyConsumesRemainderAndRecordsUnbilled(t *testing.T) {
	fixture := seedBillingSettlementFixture(t, "subscription_only", 1_000)
	session, apiErr := NewBillingSession(newBillingTestContext(1_000), fixture.info, 40)
	require.Nil(t, apiErr)

	require.NoError(t, session.Settle(70))
	require.EqualValues(t, 1_000, getSubscriptionUsed(t, fixture.subID))
	require.EqualValues(t, 1_000, getUserQuota(t, fixture.userID))
	require.EqualValues(t, 930, getTokenRemainQuota(t, fixture.tokenID))
	require.Equal(t, BillingSourceSubscription, fixture.info.BillingSource)
	require.EqualValues(t, 10, fixture.info.SubscriptionPostDelta)
	require.EqualValues(t, 20, fixture.info.SettlementUnbilledQuota)

	var root model.BillingOperation
	require.NoError(t, model.DB.Where("request_id = ? AND phase = ?", fixture.requestID, "initial").First(&root).Error)
	require.Equal(t, model.BillingSettled, root.State)
	require.Equal(t, "subscription", root.FundingSource)
	require.EqualValues(t, 0, root.WalletAmount)
	require.EqualValues(t, 50, root.SubscriptionAmount)
	require.EqualValues(t, 70, root.Actual)

	var unbilled model.BillingOperation
	require.NoError(t, model.DB.Where("request_id = ? AND phase = ?", fixture.requestID, "unbilled").First(&unbilled).Error)
	require.Equal(t, model.BillingReviewRequired, unbilled.State)
	require.EqualValues(t, 20, unbilled.Actual)
	require.Equal(t, "unbilled", unbilled.FundingSource)

	var user model.User
	require.NoError(t, model.DB.First(&user, fixture.userID).Error)
	require.EqualValues(t, 70, user.UsedQuota)
	require.EqualValues(t, 1, user.RequestCount)
}

func TestLegacySubscriptionSettlementIntentRemainsPending(t *testing.T) {
	fixture := seedBillingSettlementFixture(t, "subscription_only", 0)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", fixture.subID).Update("amount_used", 990).Error)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", fixture.tokenID).Updates(map[string]any{
		"remain_quota": 960,
		"used_quota":   40,
	}).Error)
	root := &model.BillingOperation{
		RequestId: fixture.requestID, Phase: "initial", State: model.BillingReserved,
		UserId: fixture.userID, ChannelId: fixture.channelID,
		TokenId: fixture.tokenID, SubscriptionId: fixture.subID, FundingSource: "subscription",
		WalletAmount: 0, SubscriptionAmount: 40, TokenAmount: 40, PreConsumed: 40,
	}
	require.NoError(t, model.DB.Create(root).Error)
	require.NoError(t, model.RequestBillingSettlement(fixture.requestID, 0, 30, 30, 70, true))

	require.Error(t, model.RecoverBillingSettlement(fixture.requestID))
	require.EqualValues(t, 990, getSubscriptionUsed(t, fixture.subID))
	var persisted model.BillingOperation
	require.NoError(t, model.DB.Where("request_id = ? AND phase = ?", fixture.requestID, "initial").First(&persisted).Error)
	require.Equal(t, model.BillingSettlementRequested, persisted.State)
}

func TestBillingSettlementResultReplayIsIdempotent(t *testing.T) {
	fixture := seedBillingSettlementFixture(t, "subscription_first", 1_000)
	session, apiErr := NewBillingSession(newBillingTestContext(1_000), fixture.info, 40)
	require.Nil(t, apiErr)
	require.NoError(t, session.Settle(70))

	result, err := model.ApplyBillingAdjustmentWithResult(fixture.requestID, "settle", 0, 30, 30, false, true, 70, true)
	require.NoError(t, err)
	require.EqualValues(t, 20, result.WalletDelta)
	require.EqualValues(t, 10, result.SubscriptionDelta)
	require.EqualValues(t, 30, result.TokenDelta)
	require.EqualValues(t, 980, getUserQuota(t, fixture.userID))
	require.EqualValues(t, 930, getTokenRemainQuota(t, fixture.tokenID))

	var settles int64
	require.NoError(t, model.DB.Model(&model.BillingOperation{}).Where("request_id = ? AND phase = ?", fixture.requestID, "settle").Count(&settles).Error)
	require.EqualValues(t, 1, settles)
}
