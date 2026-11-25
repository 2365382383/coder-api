package service

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// BillingSession — 统一计费会话
// ---------------------------------------------------------------------------

// BillingSession 封装单次请求的预扣费/结算/退款生命周期。
// 实现 relaycommon.BillingSettler 接口。
type BillingSession struct {
	relayInfo        *relaycommon.RelayInfo
	funding          FundingSource
	preConsumedQuota int
	tokenConsumed    int
	extraReserved    int
	trusted          bool
	fundingSettled   bool
	settled          bool
	refunded         bool
	durable          bool
	reserveCount     int
	lang             string
	mu               sync.Mutex

	settlementAllocations       []relaycommon.BillingAllocation
	settlementSubscriptionDelta int64
	settlementUnbilledQuota     int64
}

func (s *BillingSession) Settle(actualQuota int) error {
	return s.settle(actualQuota, nil)
}

func (s *BillingSession) SettleTask(actualQuota int, task *model.Task, fromStatus model.TaskStatus) error {
	if task == nil {
		return errors.New("task is required for task billing settlement")
	}
	return s.settle(actualQuota, &model.BillingTaskCommit{Task: task, FromStatus: fromStatus})
}

func (s *BillingSession) settle(actualQuota int, taskCommit *model.BillingTaskCommit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if actualQuota < 0 {
		return errors.New("actual billing quota cannot be negative")
	}
	if s.settled {
		return nil
	}
	if taskCommit != nil || s.durable {
		return s.settleDurableLocked(actualQuota, taskCommit)
	}
	return s.settleCachedLocked(actualQuota)
}

// settleCachedLocked keeps the upstream split settlement. The Redis quota
// cache has already authorized pre-consume; only the final difference is
// written through the ordinary quota functions and batch updater.
func (s *BillingSession) settleCachedLocked(actualQuota int) error {
	delta := actualQuota - s.preConsumedQuota
	if delta == 0 {
		s.settled = true
		return nil
	}
	if !s.fundingSettled {
		if err := s.funding.Settle(delta); err != nil {
			return err
		}
		s.fundingSettled = true
	}
	var tokenErr error
	if !s.relayInfo.IsPlayground {
		if delta > 0 {
			tokenErr = model.DecreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, delta)
		} else {
			tokenErr = model.IncreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, -delta)
		}
		if tokenErr != nil {
			common.SysLog(common.LogText("error adjusting token quota after funding settled (userId=%d, tokenId=%d, delta=%d): %s",
				s.relayInfo.UserId, s.relayInfo.TokenId, delta, tokenErr.Error()))
		}
	}
	if s.funding.Source() == BillingSourceSubscription {
		s.relayInfo.SubscriptionPostDelta += int64(delta)
	} else if s.funding.Source() == BillingSourceMixed {
		s.syncRelayInfo()
	}
	s.settled = true
	return tokenErr
}

// settleDurableLocked is only used by asynchronous task settlement, where the
// task state and final financial result must commit together.
func (s *BillingSession) settleDurableLocked(actualQuota int, taskCommit *model.BillingTaskCommit) error {
	delta := actualQuota - s.preConsumedQuota
	walletDelta, subDelta := s.fundingDeltas(delta)
	tokenDelta := int64(delta)
	if s.relayInfo.IsPlayground {
		tokenDelta = 0
	}
	var commits []*model.BillingTaskCommit
	countRequest := actualQuota > 0 || s.relayInfo.BillableUsageObserved
	settlementPolicy := s.settlementPolicy(delta, taskCommit)
	if taskCommit != nil {
		commits = append(commits, taskCommit)
	} else {
		if settlementPolicy != "" {
			if err := model.RequestBillingSettlementWithPolicy(s.relayInfo.RequestId, walletDelta, subDelta, tokenDelta, int64(actualQuota), countRequest, settlementPolicy); err != nil {
				return err
			}
		} else if err := model.RequestBillingSettlement(s.relayInfo.RequestId, walletDelta, subDelta, tokenDelta, int64(actualQuota), countRequest); err != nil {
			return err
		}
	}
	result, err := model.ApplyBillingAdjustmentWithResult(s.relayInfo.RequestId, "settle", walletDelta, subDelta, tokenDelta, false, true, int64(actualQuota), countRequest, commits...)
	if err != nil {
		return err
	}
	s.applyFundingDeltas(result.WalletDelta, result.SubscriptionDelta)
	s.settlementSubscriptionDelta = result.SubscriptionDelta
	s.settlementUnbilledQuota = result.UnbilledAmount
	if sub, ok := s.funding.(*SubscriptionFunding); ok && result.WalletDelta > 0 {
		s.settlementAllocations = s.subscriptionFallbackAllocations(sub, result)
	}
	s.syncRelayInfo()
	s.settled = true
	return nil
}

func (s *BillingSession) billingChannelID() int {
	if s.relayInfo != nil && s.relayInfo.ChannelMeta != nil {
		return s.relayInfo.ChannelId
	}
	return 0
}

func (s *BillingSession) settlementPolicy(delta int, taskCommit *model.BillingTaskCommit) string {
	if taskCommit != nil || delta <= 0 {
		return ""
	}
	preference := common.NormalizeBillingPreference(s.relayInfo.UserSetting.BillingPreference)
	switch preference {
	case "subscription_only":
		if s.funding.Source() == BillingSourceSubscription || s.funding.Source() == BillingSourceMixed {
			return model.BillingSettlementSubscriptionClamp
		}
	case "subscription_first", "wallet_first":
		if s.funding.Source() == BillingSourceSubscription || s.funding.Source() == BillingSourceMixed {
			return model.BillingSettlementWalletFallback
		}
	}
	return ""
}

func (s *BillingSession) subscriptionFallbackAllocations(sub *SubscriptionFunding, result model.BillingAdjustmentResult) []relaycommon.BillingAllocation {
	allocations := make([]relaycommon.BillingAllocation, 0, 2)
	subscriptionQuota := s.preConsumedQuota + int(result.SubscriptionDelta)
	if subscriptionQuota > 0 {
		allocations = append(allocations, relaycommon.BillingAllocation{
			Source:                             BillingSourceSubscription,
			Quota:                              subscriptionQuota,
			SubscriptionId:                     sub.subscriptionId,
			SubscriptionPlanId:                 sub.PlanId,
			SubscriptionPlanTitle:              sub.PlanTitle,
			SubscriptionAmountTotal:            sub.AmountTotal,
			SubscriptionAmountUsedAfterConsume: sub.AmountUsedAfter + int64(s.extraReserved) + result.SubscriptionDelta,
		})
	}
	if result.WalletDelta > 0 {
		allocations = append(allocations, relaycommon.BillingAllocation{Source: BillingSourceWallet, Quota: int(result.WalletDelta)})
	}
	return allocations
}

// Refund 退还所有预扣费，幂等安全，异步执行。
func (s *BillingSession) Refund(c *gin.Context) {
	s.mu.Lock()
	if s.settled || s.refunded || !s.needsRefundLocked() {
		s.mu.Unlock()
		return
	}
	s.refunded = true
	if s.durable {
		if err := model.RequestBillingRefund(s.relayInfo.RequestId); err != nil {
			s.refunded = false
			s.mu.Unlock()
			common.SysError(fmt.Sprintf("failed to persist billing refund request %s: %v", s.relayInfo.RequestId, err))
			return
		}
		requestID := s.relayInfo.RequestId
		s.mu.Unlock()
		logger.LogInfo(c, common.LogText("user %d request failed, refunding the pre-consumed quota (token_quota=%s, funding=%s)",
			s.relayInfo.UserId, logger.FormatQuota(s.tokenConsumed), s.funding.Source()))
		gopool.Go(func() {
			if err := model.RefundBillingOperation(requestID); err != nil {
				common.SysError(fmt.Sprintf("billing refund pending for request %s: %v", requestID, err))
			}
		})
		return
	}

	tokenID := s.relayInfo.TokenId
	tokenKey := s.relayInfo.TokenKey
	isPlayground := s.relayInfo.IsPlayground
	tokenConsumed := s.tokenConsumed
	extraReserved := s.extraReserved
	subscriptionID := s.relayInfo.SubscriptionId
	funding := s.funding
	s.mu.Unlock()

	logger.LogInfo(c, common.LogText("user %d request failed, refunding the pre-consumed quota (token_quota=%s, funding=%s)",
		s.relayInfo.UserId, logger.FormatQuota(tokenConsumed), funding.Source()))
	gopool.Go(func() {
		if err := funding.Refund(); err != nil {
			common.SysLog(common.LogText("error refunding billing source: %s", err.Error()))
		}
		if extraReserved > 0 && funding.Source() == BillingSourceSubscription && subscriptionID > 0 {
			if err := model.PostConsumeUserSubscriptionDelta(subscriptionID, -int64(extraReserved)); err != nil {
				common.SysLog(common.LogText("error refunding subscription extra reserved quota: %s", err.Error()))
			}
		}
		if tokenConsumed > 0 && !isPlayground {
			if err := model.IncreaseTokenQuota(tokenID, tokenKey, tokenConsumed); err != nil {
				common.SysLog(common.LogText("error refunding token quota: %s", err.Error()))
			}
		}
	})
}

func (s *BillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsRefundLocked()
}

func (s *BillingSession) needsRefundLocked() bool {
	if s.settled || s.refunded || s.fundingSettled {
		return false
	}
	if s.tokenConsumed > 0 || s.extraReserved > 0 {
		return true
	}
	if wallet, ok := s.funding.(*WalletFunding); ok && wallet.consumed > 0 {
		return true
	}
	if sub, ok := s.funding.(*SubscriptionFunding); ok && sub.preConsumed > 0 {
		return true
	}
	if mixed, ok := s.funding.(*MixedFunding); ok {
		return mixed.needsRefund()
	}
	return false
}

func (s *BillingSession) GetPreConsumedQuota() int {
	return s.preConsumedQuota
}

func (s *BillingSession) UsesDurableAccounting() bool {
	return s.durable
}

func (s *BillingSession) Reserve(targetQuota int) error {
	return s.reserve(targetQuota, false)
}

func (s *BillingSession) ReserveBeforeSubmit(targetQuota int) error {
	return s.reserve(targetQuota, true)
}

func (s *BillingSession) reserve(targetQuota int, requireAvailableQuota bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.relayInfo != nil {
		_, imageRequest := s.relayInfo.Request.(*dto.ImageRequest)
		requireAvailableQuota = requireAvailableQuota || imageRequest || s.relayInfo.ImageRequestCount > 0
	}
	if s.settled || s.refunded || s.trusted && !requireAvailableQuota || targetQuota <= s.preConsumedQuota {
		return nil
	}
	delta := targetQuota - s.preConsumedQuota
	if delta <= 0 {
		return nil
	}
	if s.durable {
		return s.reserveDurableLocked(delta, requireAvailableQuota)
	}
	return s.reserveCachedLocked(delta, requireAvailableQuota)
}

func (s *BillingSession) reserveCachedLocked(delta int, requireAvailableQuota bool) error {
	if err := s.reserveFunding(delta, requireAvailableQuota); err != nil {
		return err
	}
	if err := s.reserveToken(delta); err != nil {
		s.rollbackFundingReserve(delta)
		return err
	}
	s.preConsumedQuota += delta
	s.tokenConsumed += delta
	s.extraReserved += delta
	if requireAvailableQuota {
		s.trusted = false
	}
	s.syncRelayInfo()
	return nil
}

func (s *BillingSession) reserveDurableLocked(delta int, requireAvailableQuota bool) error {
	walletDelta, subDelta := s.fundingDeltas(delta)
	tokenDelta := int64(delta)
	if s.relayInfo.IsPlayground {
		tokenDelta = 0
	}
	phase := fmt.Sprintf("reserve:%d", s.reserveCount+1)
	if err := model.ApplyBillingAdjustment(s.relayInfo.RequestId, phase, walletDelta, subDelta, tokenDelta, requireAvailableQuota, false, 0, false); err != nil {
		if errors.Is(err, model.ErrBillingWalletInsufficient) {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		if errors.Is(err, model.ErrBillingSubscriptionInsufficient) {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	s.reserveCount++
	s.applyFundingDeltas(walletDelta, subDelta)
	s.preConsumedQuota += delta
	s.tokenConsumed += int(tokenDelta)
	s.extraReserved += delta
	if requireAvailableQuota {
		s.trusted = false
	}
	s.syncRelayInfo()
	return nil
}

// preConsume uses the upstream cache path for ordinary requests and the
// durable transaction path only for asynchronous tasks.
func (s *BillingSession) preConsume(c *gin.Context, quota int) *types.NewAPIError {
	effectiveQuota := quota
	if s.shouldTrust(c) {
		s.trusted = true
		effectiveQuota = 0
		logger.LogInfo(c, common.LogText("user %d has enough quota, trusted without pre-consume (funding=%s)", s.relayInfo.UserId, s.funding.Source()))
	} else if effectiveQuota > 0 {
		logger.LogInfo(c, common.LogText("user %d needs pre-consume %s (funding=%s)", s.relayInfo.UserId, logger.FormatQuota(effectiveQuota), s.funding.Source()))
	}
	if s.relayInfo.ForcePreConsume || s.funding.Source() != BillingSourceWallet {
		s.durable = true
		return s.preConsumeDurable(c, effectiveQuota)
	}
	return s.preConsumeCached(c, effectiveQuota)
}

func (s *BillingSession) preConsumeCached(c *gin.Context, effectiveQuota int) *types.NewAPIError {
	if effectiveQuota > 0 {
		if err := PreConsumeTokenQuota(s.relayInfo, effectiveQuota); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		s.tokenConsumed = effectiveQuota
	}
	if err := s.funding.PreConsume(effectiveQuota); err != nil {
		if s.tokenConsumed > 0 && !s.relayInfo.IsPlayground {
			if rollbackErr := model.IncreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, s.tokenConsumed); rollbackErr != nil {
				common.SysLog(common.LogText("error rolling back token quota (userId=%d, tokenId=%d, amount=%d, fundingErr=%s): %s",
					s.relayInfo.UserId, s.relayInfo.TokenId, s.tokenConsumed, err.Error(), rollbackErr.Error()))
			}
			s.tokenConsumed = 0
		}
		if errors.Is(err, ErrInsufficientWalletQuota) {
			userQuota, quotaErr := model.GetUserQuota(s.relayInfo.UserId, false)
			if quotaErr != nil {
				userQuota = 0
			}
			return types.NewErrorWithStatusCode(
				errors.New(i18n.T(c, i18n.MsgQuotaUserInsufficient, map[string]any{"Remaining": logger.FormatQuota64(userQuota)})),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		errMsg := err.Error()
		if strings.Contains(errMsg, "no active subscription") || strings.Contains(errMsg, "subscription quota insufficient") {
			return types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgQuotaSubscriptionInsufficient, map[string]any{"Error": errMsg})), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	s.preConsumedQuota = effectiveQuota
	s.syncRelayInfo()
	return nil
}

func (s *BillingSession) preConsumeDurable(c *gin.Context, effectiveQuota int) *types.NewAPIError {
	op := &model.BillingOperation{RequestId: s.relayInfo.RequestId, UserId: s.relayInfo.UserId,
		ChannelId: s.billingChannelID(), FundingSource: s.funding.Source(), PreConsumed: int64(effectiveQuota)}
	if !s.relayInfo.IsPlayground {
		op.TokenId = s.relayInfo.TokenId
	}
	err := model.CreateBillingReservation(op, func(tx *gorm.DB) error {
		if err := s.preConsumeFundingTx(tx, op, effectiveQuota); err != nil {
			return err
		}
		if effectiveQuota > 0 && !s.relayInfo.IsPlayground {
			reserved, err := model.TryReserveTokenQuotaTx(tx, op.TokenId, int64(effectiveQuota), s.relayInfo.TokenUnlimited)
			if err != nil {
				return err
			}
			if !reserved {
				return model.ErrBillingTokenInsufficient
			}
			op.TokenAmount = int64(effectiveQuota)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, model.ErrBillingTokenInsufficient) {
			return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		if errors.Is(err, ErrInsufficientWalletQuota) {
			userQuota, quotaErr := model.GetUserQuota(s.relayInfo.UserId, false)
			if quotaErr != nil {
				userQuota = 0
			}
			return types.NewErrorWithStatusCode(
				errors.New(i18n.T(c, i18n.MsgQuotaUserInsufficient, map[string]any{"Remaining": logger.FormatQuota64(userQuota)})),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		errMsg := err.Error()
		if strings.Contains(errMsg, "no active subscription") || strings.Contains(errMsg, "subscription quota insufficient") {
			return types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgQuotaSubscriptionInsufficient, map[string]any{"Error": errMsg})), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	if op.WalletAmount != 0 {
		if cacheErr := model.AdjustRedisBillingUserQuota(s.relayInfo.UserId, -op.WalletAmount); cacheErr != nil {
			common.SysLog(common.LogText("failed to sync durable wallet reservation cache: %s", cacheErr.Error()))
		}
	}
	if op.TokenAmount != 0 && !s.relayInfo.IsPlayground {
		if cacheErr := model.AdjustRedisBillingTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, -op.TokenAmount); cacheErr != nil {
			common.SysLog(common.LogText("failed to sync durable token reservation cache: %s", cacheErr.Error()))
		}
	}
	s.tokenConsumed = int(op.TokenAmount)
	s.preConsumedQuota = effectiveQuota
	s.hydrateSubscriptionPlan()
	s.syncRelayInfo()
	return nil
}

func (s *BillingSession) reserveFunding(delta int, requireAvailableQuota bool) error {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		if requireAvailableQuota {
			if err := funding.PreConsume(delta); err != nil {
				if errors.Is(err, ErrInsufficientWalletQuota) {
					return types.NewErrorWithStatusCode(err, types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
				}
				return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
			}
			return nil
		}
		if err := model.DecreaseUserQuota(funding.userId, int64(delta), false); err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		funding.consumed += delta
		return nil
	case *SubscriptionFunding:
		if err := model.PostConsumeUserSubscriptionDelta(funding.subscriptionId, int64(delta)); err != nil {
			return types.NewErrorWithStatusCode(
				errors.New(i18n.Translate(s.lang, i18n.MsgQuotaSubscriptionInsufficient, map[string]any{"Error": err.Error()})),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		return nil
	case *MixedFunding:
		if err := funding.Reserve(delta, requireAvailableQuota); err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		return nil
	default:
		return types.NewError(fmt.Errorf("unsupported funding source: %s", s.funding.Source()), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
}

func (s *BillingSession) rollbackFundingReserve(delta int) {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		if err := model.IncreaseUserQuota(funding.userId, int64(delta), false); err != nil {
			common.SysLog(common.LogText("error rolling back wallet funding reserve: %s", err.Error()))
		} else {
			funding.consumed -= delta
		}
	case *SubscriptionFunding:
		if err := model.PostConsumeUserSubscriptionDelta(funding.subscriptionId, -int64(delta)); err != nil {
			common.SysLog(common.LogText("error rolling back subscription funding reserve: %s", err.Error()))
		}
	case *MixedFunding:
		if err := funding.Settle(-delta); err != nil {
			common.SysLog(common.LogText("error rolling back mixed funding reserve: %s", err.Error()))
		}
	}
}

func (s *BillingSession) reserveToken(delta int) error {
	if delta <= 0 || s.relayInfo.IsPlayground {
		return nil
	}
	if err := PreConsumeTokenQuota(s.relayInfo, delta); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	return nil
}

// fundingDeltas preserves the established subscription-first allocation: an
// upward difference goes to the wallet, a downward difference refunds wallet
// before subscription. A later image reservation always charges atomically.
func (s *BillingSession) fundingDeltas(delta int) (wallet, subscription int64) {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		return int64(delta), 0
	case *SubscriptionFunding:
		return 0, int64(delta)
	case *MixedFunding:
		if delta >= 0 {
			return int64(delta), 0
		}
		walletRefund := min(-delta, funding.walletAmount)
		return -int64(walletRefund), int64(delta + walletRefund)
	}
	return 0, 0
}

func (s *BillingSession) applyFundingDeltas(wallet, subscription int64) {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		funding.consumed += int(wallet)
	case *MixedFunding:
		funding.walletAmount += int(wallet)
		funding.wallet.consumed += int(wallet)
		funding.subscriptionAmount += int(subscription)
	}
}

func (s *BillingSession) preConsumeFundingTx(tx *gorm.DB, op *model.BillingOperation, amount int) error {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		reserved, err := model.TryReserveUserQuotaTx(tx, funding.userId, int64(amount))
		if err != nil {
			return err
		}
		if !reserved {
			return ErrInsufficientWalletQuota
		}
		funding.consumed = amount
		op.WalletAmount = int64(amount)
	case *SubscriptionFunding:
		res, err := model.PreConsumeUserSubscriptionTx(tx, funding.requestId, funding.userId,
			funding.modelName, 0, funding.amount, funding.usingGroup)
		if err != nil {
			return err
		}
		funding.subscriptionId, funding.preConsumed = res.UserSubscriptionId, res.PreConsumed
		funding.AmountTotal, funding.AmountUsedAfter = res.AmountTotal, res.AmountUsedAfter
		op.SubscriptionId, op.SubscriptionAmount = res.UserSubscriptionId, res.PreConsumed
	case *MixedFunding:
		res, err := model.PreConsumeUserSubscriptionPartialTx(tx, funding.subscription.requestId,
			funding.subscription.userId, funding.subscription.modelName, 0, int64(amount), funding.subscription.usingGroup)
		if err != nil {
			return err
		}
		if res == nil || res.SubscriptionPreConsumeResult == nil || res.PreConsumed <= 0 {
			return fmt.Errorf("subscription quota insufficient, need=%d", amount)
		}
		funding.subscription.subscriptionId, funding.subscription.preConsumed = res.UserSubscriptionId, res.PreConsumed
		funding.subscription.AmountTotal, funding.subscription.AmountUsedAfter = res.AmountTotal, res.AmountUsedAfter
		funding.subscriptionAmount = int(res.PreConsumed)
		funding.walletAmount = amount - funding.subscriptionAmount
		if funding.walletAmount > 0 {
			reserved, err := model.TryReserveUserQuotaTx(tx, funding.wallet.userId, int64(funding.walletAmount))
			if err != nil {
				return err
			}
			if !reserved {
				return ErrInsufficientWalletQuota
			}
		}
		funding.wallet.consumed = funding.walletAmount
		op.SubscriptionId, op.SubscriptionAmount = res.UserSubscriptionId, res.PreConsumed
		op.WalletAmount = int64(funding.walletAmount)
	default:
		return fmt.Errorf("unsupported funding source %T", s.funding)
	}
	return nil
}

func (s *BillingSession) hydrateSubscriptionPlan() {
	var funding *SubscriptionFunding
	switch f := s.funding.(type) {
	case *SubscriptionFunding:
		funding = f
	case *MixedFunding:
		funding = f.subscription
	}
	if funding == nil || funding.subscriptionId <= 0 {
		return
	}
	if info, err := model.GetSubscriptionPlanInfoByUserSubscriptionId(funding.subscriptionId); err == nil && info != nil {
		funding.PlanId, funding.PlanTitle = info.PlanId, info.PlanTitle
	}
}

// shouldTrust 统一信任额度检查，适用于钱包和订阅。
func (s *BillingSession) shouldTrust(c *gin.Context) bool {
	// 异步任务（ForcePreConsume=true）必须预扣全额，不允许信任旁路
	if s.relayInfo.ForcePreConsume {
		return false
	}

	trustQuota := operation_setting.GetQuotaSetting().TrustQuotaUSD * common.QuotaPerUnit
	if trustQuota <= 0 || math.IsNaN(trustQuota) || math.IsInf(trustQuota, 0) {
		return false
	}

	// 检查令牌是否充足
	tokenTrusted := s.relayInfo.TokenUnlimited
	if !tokenTrusted && model.RedisBillingFastPathEnabled() {
		tokenQuota := common.GetContextKeyInt64(c, constant.ContextKey("token_quota"))
		tokenTrusted = float64(tokenQuota) > trustQuota
	}
	if !tokenTrusted {
		return false
	}

	switch s.funding.Source() {
	case BillingSourceWallet:
		return float64(s.relayInfo.UserQuota) > trustQuota
	case BillingSourceSubscription:
		// 订阅不能启用信任旁路。原因：
		// 1. PreConsumeUserSubscription 要求 amount>0 来创建预扣记录并锁定订阅
		// 2. SubscriptionFunding.PreConsume 忽略参数，始终用 s.amount 预扣
		// 3. 若信任旁路将 effectiveQuota 设为 0，会导致 preConsumedQuota 与实际订阅预扣不一致
		return false
	default:
		return false
	}
}

// syncRelayInfo 将 BillingSession 的状态同步到 RelayInfo 的兼容字段上。
func (s *BillingSession) syncRelayInfo() {
	info := s.relayInfo
	info.FinalPreConsumedQuota = s.preConsumedQuota
	info.BillingSource = s.funding.Source()
	info.SettlementUnbilledQuota = s.settlementUnbilledQuota

	if len(s.settlementAllocations) > 0 {
		info.BillingSource = BillingSourceMixed
		info.SubscriptionId = 0
		info.SubscriptionPreConsumed = 0
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = 0
		info.SubscriptionAmountUsedAfterPreConsume = 0
		info.SubscriptionPlanId = 0
		info.SubscriptionPlanTitle = ""
		info.BillingAllocations = append([]relaycommon.BillingAllocation(nil), s.settlementAllocations...)
		return
	}

	if sub, ok := s.funding.(*SubscriptionFunding); ok {
		info.SubscriptionId = sub.subscriptionId
		info.SubscriptionPreConsumed = sub.preConsumed + int64(s.extraReserved)
		info.SubscriptionPostDelta = s.settlementSubscriptionDelta
		info.SubscriptionAmountTotal = sub.AmountTotal
		info.SubscriptionAmountUsedAfterPreConsume = sub.AmountUsedAfter + int64(s.extraReserved)
		info.SubscriptionPlanId = sub.PlanId
		info.SubscriptionPlanTitle = sub.PlanTitle
		info.BillingAllocations = nil
	} else if mixed, ok := s.funding.(*MixedFunding); ok {
		info.SubscriptionId = 0
		info.SubscriptionPreConsumed = 0
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = 0
		info.SubscriptionAmountUsedAfterPreConsume = 0
		info.SubscriptionPlanId = 0
		info.SubscriptionPlanTitle = ""
		info.BillingAllocations = mixed.Allocations()
	} else {
		info.SubscriptionId = 0
		info.SubscriptionPreConsumed = 0
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = 0
		info.SubscriptionAmountUsedAfterPreConsume = 0
		info.SubscriptionPlanId = 0
		info.SubscriptionPlanTitle = ""
		info.BillingAllocations = nil
	}
}

// ---------------------------------------------------------------------------
// NewBillingSession 工厂 — 根据计费偏好创建会话并处理回退
// ---------------------------------------------------------------------------

// NewBillingSession 根据用户计费偏好创建 BillingSession，处理 subscription_first / wallet_first 的回退。
func NewBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError) {
	if relayInfo == nil {
		return nil, types.NewError(fmt.Errorf("relayInfo is nil"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if relayInfo.RequestId == "" {
		relayInfo.RequestId = common.NewRequestId()
	}

	pref := common.NormalizeBillingPreference(relayInfo.UserSetting.BillingPreference)

	// 钱包路径需要先检查用户额度
	tryWallet := func() (*BillingSession, *types.NewAPIError) {
		userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if userQuota <= 0 {
			return nil, types.NewErrorWithStatusCode(
				errors.New(i18n.T(c, i18n.MsgQuotaUserInsufficient, map[string]any{"Remaining": logger.FormatQuota64(userQuota)})),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		if userQuota-int64(preConsumedQuota) < 0 {
			return nil, types.NewErrorWithStatusCode(
				errors.New(i18n.T(c, i18n.MsgQuotaPreConsumeInsufficient, map[string]any{"Remaining": logger.FormatQuota64(userQuota), "Required": logger.FormatQuota(preConsumedQuota)})),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		relayInfo.UserQuota = userQuota

		session := &BillingSession{
			relayInfo: relayInfo,
			funding:   &WalletFunding{userId: relayInfo.UserId},
			lang:      i18n.StatedLang(c),
		}
		if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	trySubscription := func() (*BillingSession, *types.NewAPIError) {
		subConsume := int64(preConsumedQuota)
		if subConsume <= 0 {
			subConsume = 1
		}
		session := &BillingSession{
			relayInfo: relayInfo,
			lang:      i18n.StatedLang(c),
			funding: &SubscriptionFunding{
				requestId:  relayInfo.RequestId,
				userId:     relayInfo.UserId,
				modelName:  relayInfo.GetBillingModelName(),
				usingGroup: relayInfo.UsingGroup,
				amount:     subConsume,
			},
		}
		// 必须传 subConsume 而非 preConsumedQuota，保证 SubscriptionFunding.amount、
		// preConsume 参数和 FinalPreConsumedQuota 三者一致，避免订阅多扣费。
		if apiErr := session.preConsume(c, int(subConsume)); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	// Keep the local mixed-funding business contract as a narrow funding
	// source extension. The surrounding session lifecycle, image reservation,
	// plugin task flow, and retry behavior remain upstream rc.39.
	tryMixedSubscriptionWallet := func() (*BillingSession, *types.NewAPIError) {
		if preConsumedQuota <= 0 {
			return trySubscription()
		}
		userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if userQuota <= 0 {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("订阅额度不足且用户余额不足, 剩余额度: %s", logger.FormatQuota64(userQuota)),
				types.ErrorCodeInsufficientUserQuota,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}
		relayInfo.UserQuota = userQuota

		session := &BillingSession{
			relayInfo: relayInfo,
			funding: &MixedFunding{
				subscription: &SubscriptionFunding{
					requestId:  relayInfo.RequestId,
					userId:     relayInfo.UserId,
					modelName:  relayInfo.GetBillingModelName(),
					usingGroup: relayInfo.UsingGroup,
					amount:     int64(preConsumedQuota),
				},
				wallet: &WalletFunding{userId: relayInfo.UserId},
			},
		}
		if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
			if apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				if strings.Contains(apiErr.Error(), "subscription quota insufficient") || strings.Contains(apiErr.Error(), "no active subscription") {
					return tryWallet()
				}
			}
			return nil, apiErr
		}
		return session, nil
	}

	switch pref {
	case "subscription_only":
		return trySubscription()
	case "wallet_only":
		return tryWallet()
	case "wallet_first":
		session, err := tryWallet()
		if err != nil {
			if err.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				return trySubscription()
			}
			return nil, err
		}
		return session, nil
	case "subscription_first":
		fallthrough
	default:
		hasSub, subCheckErr := model.HasActiveUserSubscriptionForGroup(relayInfo.UserId, relayInfo.UsingGroup)
		if subCheckErr != nil {
			return nil, types.NewError(subCheckErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if !hasSub {
			return tryWallet()
		}
		session, apiErr := trySubscription()
		if apiErr != nil {
			if apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				// subscription_first always consumes the remaining subscription quota
				// first, then covers the remainder from the wallet. The plan flag is
				// not a gate for this billing preference.
				return tryMixedSubscriptionWallet()
			}
			return nil, apiErr
		}
		return session, nil
	}
}
