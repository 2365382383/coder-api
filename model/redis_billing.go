package model

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	billingmetrics "github.com/QuantumNous/new-api/pkg/billing_metrics"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

const (
	redisBillingStream         = "new_api:billing:events:v1"
	redisBillingConsumerGroup  = "new-api-billing-materializer"
	redisBillingCacheTTL       = 15 * time.Minute
	redisBillingIdempotencyTTL = 7 * 24 * time.Hour
	redisBillingBatchSize      = 100
	redisBillingPendingMinIdle = 30 * time.Second
)

var (
	ErrRedisBillingUnavailable       = errors.New("redis billing fast path unavailable")
	ErrRedisBillingInsufficient      = errors.New("redis billing quota insufficient")
	ErrRedisBillingTokenInsufficient = errors.New("redis billing token quota insufficient")
	ErrRedisBillingUnknown           = errors.New("redis billing result is unknown")
	errRedisBillingEventRetry        = errors.New("redis billing event is waiting for its reservation")
)

// RedisBillingFastPathEnabled reports whether wallet and token billing may use
// the Redis atomic path. The default is enabled when Redis is configured; the
// environment switch is retained for a safe rollback to the database path.
func RedisBillingFastPathEnabled() bool {
	return common.RedisEnabled && common.RDB != nil && common.GetEnvOrDefaultBool("BILLING_REDIS_FAST_PATH", true)
}

// RedisBillingEventRequest is one idempotent financial phase. Positive wallet
// and token deltas charge the account; negative deltas refund it.
type RedisBillingEventRequest struct {
	RequestID      string
	Phase          string
	Kind           string
	UserID         int
	ChannelID      int
	TokenID        int
	TokenKey       string
	WalletDelta    int64
	TokenDelta     int64
	Actual         int64
	CountRequest   bool
	PreConsumed    int64
	TokenUnlimited bool
	FundingSource  string
}

// redisBillingApplyScript changes all hot-path balances and appends the
// durable event in one Redis script. The user balance is a string key because
// Redis HINCRBY/INCRBY natively preserves the signed int64 domain. Token quota
// uses a hash so remain_quota and used_quota move together.
const redisBillingApplyScript = `
local function normalize(value)
  value = value or '0'
  local sign = ''
  local first = string.sub(value, 1, 1)
  if first == '-' or first == '+' then
    sign = first == '-' and '-' or ''
    value = string.sub(value, 2)
  end
  value = string.gsub(value, '^0+', '')
  if value == '' then value = '0' end
  if value == '0' then return '0' end
  return sign .. value
end
local function compare(a, b)
  a = normalize(a)
  b = normalize(b)
  local an = string.sub(a, 1, 1) == '-'
  local bn = string.sub(b, 1, 1) == '-'
  if an and not bn then return -1 end
  if not an and bn then return 1 end
  local aa = an and string.sub(a, 2) or a
  local bb = bn and string.sub(b, 2) or b
  if string.len(aa) < string.len(bb) then return an and 1 or -1 end
  if string.len(aa) > string.len(bb) then return an and -1 or 1 end
  if aa == bb then return 0 end
  if aa < bb then return an and 1 or -1 end
  return an and -1 or 1
end
local function negate(value)
  value = normalize(value)
  if string.sub(value, 1, 1) == '-' then return string.sub(value, 2) end
  return '-' .. value
end

local existing = redis.call('HGET', KEYS[3], 'result')
if existing then
  return {tonumber(existing), redis.call('HGET', KEYS[3], 'event_id') or ''}
end

local walletDelta = ARGV[1]
local tokenDelta = ARGV[2]
local kind = ARGV[3]
local walletCharge = compare(walletDelta, '0') > 0
local tokenCharge = compare(tokenDelta, '0') > 0

if walletDelta ~= '0' then
  local current = redis.call('GET', KEYS[1])
  if not current then return {-1, ''} end
  if kind == 'reserve' and walletCharge and compare(current, walletDelta) < 0 then
    return {0, ''}
  end
end

if KEYS[2] ~= '' and tokenDelta ~= '0' then
  if redis.call('HEXISTS', KEYS[2], 'RemainQuota') == 0 or redis.call('HEXISTS', KEYS[2], 'UsedQuota') == 0 then
    return {-1, ''}
  end
  if kind == 'reserve' and tokenCharge and ARGV[4] ~= '1' then
    local remain = redis.call('HGET', KEYS[2], 'RemainQuota')
		if compare(remain, tokenDelta) < 0 then return {-3, ''} end
  end
end

local userChanged = false
if walletDelta ~= '0' then
  local userResult = redis.pcall('INCRBY', KEYS[1], negate(walletDelta))
  if type(userResult) == 'table' and userResult.err then return {-2, ''} end
  userChanged = true
end

local tokenChanged = false
if KEYS[2] ~= '' and tokenDelta ~= '0' then
  local remainResult = redis.pcall('HINCRBY', KEYS[2], 'RemainQuota', negate(tokenDelta))
  if type(remainResult) == 'table' and remainResult.err then
    if userChanged then redis.call('INCRBY', KEYS[1], walletDelta) end
    return {-2, ''}
  end
  local usedResult = redis.pcall('HINCRBY', KEYS[2], 'UsedQuota', tokenDelta)
  if type(usedResult) == 'table' and usedResult.err then
    redis.call('HINCRBY', KEYS[2], 'RemainQuota', tokenDelta)
    if userChanged then redis.call('INCRBY', KEYS[1], walletDelta) end
    return {-2, ''}
  end
  tokenChanged = true
end

local eventID = redis.pcall('XADD', KEYS[5], '*',
  'request_id', ARGV[5], 'phase', ARGV[6], 'kind', kind,
  'user_id', ARGV[7], 'channel_id', ARGV[8], 'token_id', ARGV[9],
  'wallet_delta', walletDelta, 'token_delta', tokenDelta,
  'actual', ARGV[10], 'count_request', ARGV[11],
  'pre_consumed', ARGV[12], 'token_unlimited', ARGV[4],
  'funding_source', ARGV[13])
if type(eventID) == 'table' and eventID.err then
  if tokenChanged then
    redis.call('HINCRBY', KEYS[2], 'RemainQuota', tokenDelta)
    redis.call('HINCRBY', KEYS[2], 'UsedQuota', negate(tokenDelta))
  end
  if userChanged then redis.call('INCRBY', KEYS[1], walletDelta) end
  return {-2, ''}
end

redis.call('HSET', KEYS[3], 'result', '1', 'event_id', eventID)
redis.call('EXPIRE', KEYS[3], ARGV[14])
return {1, eventID}`

const redisBillingTokenHydrateScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then
  redis.call('HSET', KEYS[1], 'Id', ARGV[1], 'RemainQuota', ARGV[2], 'UsedQuota', ARGV[3])
  return 1
end
if redis.call('HGET', KEYS[1], 'Id') ~= ARGV[1] then return -1 end
if redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0 then redis.call('HSET', KEYS[1], 'RemainQuota', ARGV[2]) end
if redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then redis.call('HSET', KEYS[1], 'UsedQuota', ARGV[3]) end
return 0`

const redisBillingTokenAdjustScript = `
if redis.call('HGET', KEYS[1], 'Id') ~= ARGV[2] then return -1 end
local remain = redis.pcall('HINCRBY', KEYS[1], 'RemainQuota', ARGV[1])
if type(remain) == 'table' and remain.err then return -2 end
local delta = ARGV[1]
local opposite = delta
if string.sub(delta, 1, 1) == '-' then opposite = string.sub(delta, 2) else opposite = '-' .. delta end
local used = redis.pcall('HINCRBY', KEYS[1], 'UsedQuota', opposite)
if type(used) == 'table' and used.err then
  redis.call('HINCRBY', KEYS[1], 'RemainQuota', opposite)
  return -2
end
return 1`

func redisBillingUserKey(userID int) string {
	return fmt.Sprintf("new_api:billing:user:%d", userID)
}

func redisBillingTokenKey(tokenKey string) string {
	return "new_api:billing:token:" + common.GenerateHMAC(tokenKey)
}

func redisBillingIdempotencyKey(requestID, phase string) string {
	return "new_api:billing:idempotency:" + requestID + ":" + phase
}

func RedisBillingUserQuota(userID int) (int64, error) {
	if !RedisBillingFastPathEnabled() || userID <= 0 {
		return 0, ErrRedisBillingUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	key := redisBillingUserKey(userID)
	raw, err := common.RDB.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		var quota int64
		if err := DB.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Select("quota").Take(&quota).Error; err != nil {
			return 0, err
		}
		ok, setErr := common.RDB.SetNX(ctx, key, quota, 0).Result()
		if setErr != nil {
			return 0, fmt.Errorf("hydrate Redis user quota: %w", setErr)
		}
		if ok {
			return quota, nil
		}
		raw, err = common.RDB.Get(ctx, key).Result()
	}
	if err != nil {
		return 0, fmt.Errorf("read Redis user quota: %w", err)
	}
	quota, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse Redis user quota: %w", err)
	}
	return quota, nil
}

// InvalidateRedisBillingUserQuota is called after an administrator or payment
// path changes the database balance. The next fast-path request hydrates the
// key from the committed value.
func InvalidateRedisBillingUserQuota(userID int) error {
	if !common.RedisEnabled || common.RDB == nil || userID <= 0 {
		return nil
	}
	return common.RDB.Del(context.Background(), redisBillingUserKey(userID)).Err()
}

// AdjustRedisBillingUserQuota applies an already committed additive database
// change to an existing hot-path key. Keeping the key avoids resurrecting a
// pending Redis reservation from an older database snapshot. A cold key is
// left untouched and will hydrate from the committed database on demand.
func AdjustRedisBillingUserQuota(userID int, delta int64) error {
	if !common.RedisEnabled || common.RDB == nil || userID <= 0 || delta == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	exists, err := common.RDB.Exists(ctx, redisBillingUserKey(userID)).Result()
	if err != nil || exists == 0 {
		return err
	}
	_, err = common.RDB.IncrBy(ctx, redisBillingUserKey(userID), delta).Result()
	return err
}

func invalidateRedisBillingTokenQuota(tokenKey string) error {
	if !common.RedisEnabled || common.RDB == nil || tokenKey == "" {
		return nil
	}
	return common.RDB.Del(context.Background(), redisBillingTokenKey(tokenKey)).Err()
}

// InvalidateRedisBillingTokenQuota invalidates the token quota after a
// database fallback has committed a billing phase.
func InvalidateRedisBillingTokenQuota(tokenKey string) error {
	return invalidateRedisBillingTokenQuota(tokenKey)
}

func adjustRedisBillingTokenQuota(tokenID int, tokenKey string, delta int64) error {
	if !common.RedisEnabled || common.RDB == nil || tokenID <= 0 || tokenKey == "" || delta == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	key := redisBillingTokenKey(tokenKey)
	cachedID, err := common.RDB.HGet(ctx, key, "Id").Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil || cachedID != strconv.Itoa(tokenID) {
		return err
	}
	result, err := common.RDB.Eval(ctx, redisBillingTokenAdjustScript, []string{key}, delta, tokenID).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return ErrRedisBillingUnavailable
	}
	return nil
}

func ensureRedisBillingUserQuota(ctx context.Context, userID int) error {
	if _, err := RedisBillingUserQuota(userID); err != nil {
		return err
	}
	return nil
}

func ensureRedisBillingTokenQuota(ctx context.Context, tokenID int, tokenKey string) (string, error) {
	if tokenID <= 0 || tokenKey == "" {
		return "", nil
	}
	key := redisBillingTokenKey(tokenKey)
	if cachedID, err := common.RDB.HGet(ctx, key, "Id").Result(); err == nil && cachedID == strconv.Itoa(tokenID) {
		return key, nil
	} else if err != nil && !errors.Is(err, redis.Nil) {
		return "", err
	}
	var token Token
	if err := DB.WithContext(ctx).Select("id", "remain_quota", "used_quota").Where("id = ?", tokenID).First(&token).Error; err != nil {
		return "", err
	}
	result, err := common.RDB.Eval(ctx, redisBillingTokenHydrateScript, []string{key}, token.Id, token.RemainQuota, token.UsedQuota, int(redisBillingCacheTTL/time.Second)).Int()
	if err != nil {
		return "", fmt.Errorf("hydrate Redis token quota: %w", err)
	}
	if result < 0 {
		return "", ErrRedisBillingUnavailable
	}
	return key, nil
}

func parseRedisBillingResult(value any) (int64, string, error) {
	items, ok := value.([]any)
	if !ok || len(items) != 2 {
		return 0, "", fmt.Errorf("unexpected Redis billing result %T", value)
	}
	code, ok := items[0].(int64)
	if !ok {
		return 0, "", fmt.Errorf("unexpected Redis billing result code %T", items[0])
	}
	eventID := fmt.Sprint(items[1])
	return code, eventID, nil
}

// ApplyRedisBillingEvent performs one fast-path phase. It returns a fallback
// error for Redis misses so callers can use the existing database transaction.
func ApplyRedisBillingEvent(request RedisBillingEventRequest) error {
	started := time.Now()
	if !RedisBillingFastPathEnabled() || request.UserID <= 0 || request.RequestID == "" || request.Phase == "" || request.Kind == "" {
		return ErrRedisBillingUnavailable
	}
	if request.WalletDelta == common.MinWalletQuota || request.TokenDelta == common.MinWalletQuota {
		return common.ErrWalletQuotaOverflow
	}
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	if request.WalletDelta != 0 {
		if err := ensureRedisBillingUserQuota(ctx, request.UserID); err != nil {
			billingmetrics.ObserveRedisEvent(request.Phase, "fallback", time.Since(started))
			return ErrRedisBillingUnavailable
		}
	}
	tokenCacheKey := ""
	if request.TokenDelta != 0 && request.TokenID > 0 && request.TokenKey == "" {
		billingmetrics.ObserveRedisEvent(request.Phase, "fallback", time.Since(started))
		return ErrRedisBillingUnavailable
	}
	if request.TokenDelta != 0 && request.TokenID > 0 {
		var err error
		tokenCacheKey, err = ensureRedisBillingTokenQuota(ctx, request.TokenID, request.TokenKey)
		if err != nil {
			billingmetrics.ObserveRedisEvent(request.Phase, "fallback", time.Since(started))
			return ErrRedisBillingUnavailable
		}
	}
	countRequest := "0"
	if request.CountRequest {
		countRequest = "1"
	}
	unlimited := "0"
	if request.TokenUnlimited {
		unlimited = "1"
	}
	result, err := common.RDB.Eval(ctx, redisBillingApplyScript,
		[]string{
			redisBillingUserKey(request.UserID), tokenCacheKey,
			redisBillingIdempotencyKey(request.RequestID, request.Phase), "", redisBillingStream,
		},
		request.WalletDelta, request.TokenDelta, request.Kind, unlimited,
		request.RequestID, request.Phase, request.UserID, request.ChannelID, request.TokenID,
		request.Actual, countRequest, request.PreConsumed, request.FundingSource,
		int(redisBillingIdempotencyTTL/time.Second), int(redisBillingCacheTTL/time.Second),
	).Result()
	if err != nil {
		if marker, markerErr := common.RDB.HGet(ctx, redisBillingIdempotencyKey(request.RequestID, request.Phase), "result").Result(); markerErr == nil && marker == "1" {
			billingmetrics.ObserveRedisEvent(request.Phase, "success", time.Since(started))
			return nil
		}
		if markerErr := common.RDB.Ping(ctx).Err(); markerErr != nil {
			billingmetrics.ObserveRedisEvent(request.Phase, "unknown", time.Since(started))
			return ErrRedisBillingUnknown
		}
		billingmetrics.ObserveRedisEvent(request.Phase, "fallback", time.Since(started))
		return ErrRedisBillingUnavailable
	}
	code, _, err := parseRedisBillingResult(result)
	if err != nil {
		billingmetrics.ObserveRedisEvent(request.Phase, "fallback", time.Since(started))
		return ErrRedisBillingUnavailable
	}
	switch code {
	case 1:
		billingmetrics.ObserveRedisEvent(request.Phase, "success", time.Since(started))
		return nil
	case 0:
		billingmetrics.ObserveRedisEvent(request.Phase, "insufficient", time.Since(started))
		return ErrRedisBillingInsufficient
	case -3:
		billingmetrics.ObserveRedisEvent(request.Phase, "insufficient", time.Since(started))
		return ErrRedisBillingTokenInsufficient
	default:
		billingmetrics.ObserveRedisEvent(request.Phase, "fallback", time.Since(started))
		return ErrRedisBillingUnavailable
	}
}

// ApplyRedisBillingEventWithRetry retries only transport and availability
// failures. The request phase is idempotent in Redis, so retrying the same
// request/phase is safe even when the first script call completed but its
// response was lost. Business results such as insufficient quota are returned
// immediately and must never be retried as infrastructure failures.
func ApplyRedisBillingEventWithRetry(request RedisBillingEventRequest, attempts int) error {
	if attempts < 1 {
		attempts = 1
	}
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		err = ApplyRedisBillingEvent(request)
		if err == nil || (!errors.Is(err, ErrRedisBillingUnavailable) && !errors.Is(err, ErrRedisBillingUnknown)) {
			return err
		}
		if attempt+1 == attempts {
			break
		}
		delay := 100 * time.Millisecond * time.Duration(1<<attempt)
		if delay > 2*time.Second {
			delay = 2 * time.Second
		}
		time.Sleep(delay)
	}
	return err
}

type redisBillingEvent struct {
	RequestID      string
	Phase          string
	Kind           string
	UserID         int
	ChannelID      int
	TokenID        int
	WalletDelta    int64
	TokenDelta     int64
	Actual         int64
	CountRequest   bool
	PreConsumed    int64
	TokenUnlimited bool
	FundingSource  string
}

func redisMessageString(values map[string]any, key string) string {
	return fmt.Sprint(values[key])
}

func redisMessageInt(values map[string]any, key string) (int64, error) {
	raw := redisMessageString(values, key)
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid Redis billing %s=%q: %w", key, raw, err)
	}
	return value, nil
}

func parseRedisBillingEvent(message redis.XMessage) (redisBillingEvent, error) {
	values := message.Values
	event := redisBillingEvent{
		RequestID:      redisMessageString(values, "request_id"),
		Phase:          redisMessageString(values, "phase"),
		Kind:           redisMessageString(values, "kind"),
		FundingSource:  redisMessageString(values, "funding_source"),
		CountRequest:   redisMessageString(values, "count_request") == "1",
		TokenUnlimited: redisMessageString(values, "token_unlimited") == "1",
	}
	for key, target := range map[string]*int64{
		"wallet_delta": &event.WalletDelta, "token_delta": &event.TokenDelta,
		"actual": &event.Actual, "pre_consumed": &event.PreConsumed,
	} {
		value, err := redisMessageInt(values, key)
		if err != nil {
			return redisBillingEvent{}, err
		}
		*target = value
	}
	for key, target := range map[string]*int{
		"user_id": &event.UserID, "channel_id": &event.ChannelID, "token_id": &event.TokenID,
	} {
		value, err := redisMessageInt(values, key)
		if err != nil || value < 0 || value > int64(^uint(0)>>1) {
			if err == nil {
				err = fmt.Errorf("value out of int range")
			}
			return redisBillingEvent{}, err
		}
		*target = int(value)
	}
	if event.UserID <= 0 || event.RequestID == "" || event.Phase == "" {
		return redisBillingEvent{}, errors.New("invalid Redis billing event identity")
	}
	return event, nil
}

type redisBillingAggregate struct {
	userQuota    map[int]int64
	userUsed     map[int]int64
	requestCount map[int]int64
	tokenQuota   map[int]int64
	channelUsed  map[int]int64
}

func newRedisBillingAggregate() *redisBillingAggregate {
	return &redisBillingAggregate{
		userQuota: make(map[int]int64), userUsed: make(map[int]int64),
		requestCount: make(map[int]int64), tokenQuota: make(map[int]int64),
		channelUsed: make(map[int]int64),
	}
}

func addRedisBillingDelta(store map[int]int64, id int, delta int64) error {
	if id <= 0 || delta == 0 {
		return nil
	}
	current, err := common.AddWalletQuota(store[id], delta)
	if err != nil {
		return err
	}
	store[id] = current
	return nil
}

func addRedisBillingAccountDelta(aggregate *redisBillingAggregate, event redisBillingEvent) error {
	if event.WalletDelta != 0 {
		if err := addRedisBillingDelta(aggregate.userQuota, event.UserID, -event.WalletDelta); err != nil {
			return err
		}
	}
	if event.TokenDelta != 0 && event.TokenID > 0 {
		if err := addRedisBillingDelta(aggregate.tokenQuota, event.TokenID, -event.TokenDelta); err != nil {
			return err
		}
	}
	if event.Kind == "settle" && (event.Actual > 0 || event.CountRequest) {
		if event.Actual > 0 {
			if err := addRedisBillingDelta(aggregate.userUsed, event.UserID, event.Actual); err != nil {
				return err
			}
			if event.ChannelID > 0 {
				if err := addRedisBillingDelta(aggregate.channelUsed, event.ChannelID, event.Actual); err != nil {
					return err
				}
			}
		}
		if event.CountRequest {
			if err := addRedisBillingDelta(aggregate.requestCount, event.UserID, 1); err != nil {
				return err
			}
		}
	}
	return nil
}

func materializeRedisBillingEvent(tx *gorm.DB, event redisBillingEvent, aggregate *redisBillingAggregate) error {
	if event.Kind == "reserve" {
		var root BillingOperation
		err := tx.Where("request_id = ? AND phase = ?", event.RequestID, "initial").First(&root).Error
		if event.Phase == "initial" {
			if err == nil {
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			now := common.GetTimestamp()
			root = BillingOperation{RequestId: event.RequestID, Phase: "initial", State: BillingReserved,
				UserId: event.UserID, ChannelId: event.ChannelID, TokenId: event.TokenID,
				FundingSource: event.FundingSource, WalletAmount: event.WalletDelta,
				TokenAmount: event.TokenDelta, PreConsumed: event.PreConsumed,
				CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&root).Error; err != nil {
				return err
			}
			return addRedisBillingAccountDelta(aggregate, event)
		}
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errRedisBillingEventRetry
			}
			return err
		}
		var existing BillingOperation
		err = tx.Where("request_id = ? AND phase = ?", event.RequestID, event.Phase).First(&existing).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		now := common.GetTimestamp()
		step := BillingOperation{RequestId: event.RequestID, Phase: event.Phase, State: BillingReserved,
			UserId: event.UserID, ChannelId: event.ChannelID, TokenId: event.TokenID,
			FundingSource: event.FundingSource, WalletAmount: event.WalletDelta,
			TokenAmount: event.TokenDelta, PreConsumed: event.PreConsumed,
			CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&step).Error; err != nil {
			return err
		}
		walletTotal, err := common.AddWalletQuota(root.WalletAmount, event.WalletDelta)
		if err != nil {
			return err
		}
		tokenTotal, err := common.AddWalletQuota(root.TokenAmount, event.TokenDelta)
		if err != nil {
			return err
		}
		if err := tx.Model(&root).Updates(map[string]any{
			"wallet_amount": walletTotal, "token_amount": tokenTotal,
			"extra_reserved": gorm.Expr("extra_reserved + ?", event.WalletDelta),
			"pre_consumed":   gorm.Expr("pre_consumed + ?", event.PreConsumed),
			"updated_at":     now,
		}).Error; err != nil {
			return err
		}
		return addRedisBillingAccountDelta(aggregate, event)
	}

	var root BillingOperation
	if err := tx.Where("request_id = ? AND phase = ?", event.RequestID, "initial").First(&root).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errRedisBillingEventRetry
		}
		return err
	}
	var existing BillingOperation
	err := tx.Where("request_id = ? AND phase = ?", event.RequestID, event.Phase).First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	now := common.GetTimestamp()
	state := BillingSettled
	if event.Kind == "refund" {
		state = BillingRefunded
	}
	phase := BillingOperation{RequestId: event.RequestID, Phase: event.Phase, State: state,
		UserId: event.UserID, ChannelId: event.ChannelID, TokenId: event.TokenID,
		FundingSource: event.FundingSource, WalletAmount: event.WalletDelta,
		TokenAmount: event.TokenDelta, Actual: event.Actual, UsageCounted: event.CountRequest,
		CreatedAt: now, UpdatedAt: now}
	if err := tx.Create(&phase).Error; err != nil {
		return err
	}
	if event.Kind == "settle" {
		walletTotal, err := common.AddWalletQuota(root.WalletAmount, event.WalletDelta)
		if err != nil {
			return err
		}
		tokenTotal, err := common.AddWalletQuota(root.TokenAmount, event.TokenDelta)
		if err != nil {
			return err
		}
		if err := tx.Model(&root).Updates(map[string]any{
			"wallet_amount": walletTotal, "token_amount": tokenTotal,
			"state": BillingSettled, "actual": event.Actual, "updated_at": now,
		}).Error; err != nil {
			return err
		}
	} else if event.Kind == "refund" {
		if err := tx.Model(&root).Updates(map[string]any{"state": BillingRefunded, "updated_at": now}).Error; err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unsupported Redis billing event kind %q", event.Kind)
	}
	return addRedisBillingAccountDelta(aggregate, event)
}

func sortedRedisBillingIDs(values map[int]int64) []int {
	ids := make([]int, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func applyRedisBillingAggregate(tx *gorm.DB, aggregate *redisBillingAggregate) error {
	for _, id := range sortedRedisBillingIDs(aggregate.userQuota) {
		if err := updateUserQuotaWithDeltaTx(tx, id, aggregate.userQuota[id], nil); err != nil {
			return err
		}
	}
	for _, id := range sortedRedisBillingIDs(aggregate.tokenQuota) {
		if err := updateTokenQuotaDeltaTx(tx, id, aggregate.tokenQuota[id]); err != nil {
			return err
		}
	}
	for _, id := range sortedRedisBillingIDs(aggregate.userUsed) {
		if err := updateUserQuotaFieldDeltaTx(tx, id, "used_quota", aggregate.userUsed[id]); err != nil {
			return err
		}
	}
	for _, id := range sortedRedisBillingIDs(aggregate.requestCount) {
		count := aggregate.requestCount[id]
		result := tx.Model(&User{}).Where("id = ? AND request_count <= ?", id, int64(2147483647)-count).
			Update("request_count", gorm.Expr("request_count + ?", count))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
	}
	for _, id := range sortedRedisBillingIDs(aggregate.channelUsed) {
		if err := updateChannelUsedQuotaTx(tx, id, aggregate.channelUsed[id]); err != nil {
			return err
		}
	}
	return nil
}

func materializeRedisBillingBatch(ctx context.Context, messages []redis.XMessage) error {
	events := make([]redisBillingEvent, 0, len(messages))
	for _, message := range messages {
		event, err := parseRedisBillingEvent(message)
		if err != nil {
			return err
		}
		events = append(events, event)
	}
	aggregate := newRedisBillingAggregate()
	transactionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := withRetryableDatabaseTransaction(transactionCtx, func(tx *gorm.DB) error {
		for _, event := range events {
			if err := materializeRedisBillingEvent(tx, event, aggregate); err != nil {
				return err
			}
		}
		return applyRedisBillingAggregate(tx, aggregate)
	})
	if err != nil {
		billingmetrics.ObserveRedisMaterialized("failure")
		return err
	}
	for range events {
		billingmetrics.ObserveRedisMaterialized("success")
	}
	return nil
}

var redisBillingMaterializerOnce sync.Once

// StartRedisBillingMaterializer starts the shared Redis Stream consumer. A
// consumer group makes blue-green nodes share work; the database transaction
// and unique request/phase key make redelivery safe.
func StartRedisBillingMaterializer() {
	if !RedisBillingFastPathEnabled() {
		return
	}
	redisBillingMaterializerOnce.Do(func() {
		go redisBillingMaterializerLoop()
	})
}

func claimRedisBillingPendingMessages(ctx context.Context, consumer string) ([]redis.XMessage, error) {
	pending, err := common.RDB.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: redisBillingStream, Group: redisBillingConsumerGroup,
		Idle: redisBillingPendingMinIdle, Start: "-", End: "+", Count: redisBillingBatchSize,
	}).Result()
	if err != nil || len(pending) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(pending))
	for _, message := range pending {
		ids = append(ids, message.ID)
	}
	return common.RDB.XClaim(ctx, &redis.XClaimArgs{
		Stream: redisBillingStream, Group: redisBillingConsumerGroup,
		Consumer: consumer, MinIdle: redisBillingPendingMinIdle, Messages: ids,
	}).Result()
}

func redisBillingMaterializerLoop() {
	ctx := context.Background()
	groupErr := common.RDB.XGroupCreateMkStream(ctx, redisBillingStream, redisBillingConsumerGroup, "0").Err()
	if groupErr != nil && !strings.Contains(strings.ToUpper(groupErr.Error()), "BUSYGROUP") {
		common.SysError("failed to initialize Redis billing stream group: " + groupErr.Error())
		return
	}
	consumer := common.NodeName + "-" + common.GetRandomString(8)
	observeRedisBillingStreamState(ctx)
	for {
		messages, err := claimRedisBillingPendingMessages(ctx, consumer)
		if err != nil && !errors.Is(err, redis.Nil) {
			common.SysError("Redis billing pending claim failed: " + err.Error())
			time.Sleep(time.Second)
			continue
		}
		if len(messages) == 0 {
			read, readErr := common.RDB.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group: redisBillingConsumerGroup, Consumer: consumer,
				Streams: []string{redisBillingStream, ">"}, Count: redisBillingBatchSize, Block: time.Second,
			}).Result()
			if readErr != nil && !errors.Is(readErr, redis.Nil) {
				common.SysError("Redis billing stream read failed: " + readErr.Error())
				time.Sleep(time.Second)
				continue
			}
			if len(read) > 0 {
				messages = read[0].Messages
			}
		}
		if len(messages) == 0 {
			observeRedisBillingStreamState(ctx)
			continue
		}
		if err := materializeRedisBillingBatch(ctx, messages); err != nil {
			common.SysError("Redis billing materialization failed: " + err.Error())
			time.Sleep(time.Second)
			continue
		}
		ids := make([]string, 0, len(messages))
		for _, message := range messages {
			ids = append(ids, message.ID)
		}
		if err := common.RDB.XAck(ctx, redisBillingStream, redisBillingConsumerGroup, ids...).Err(); err != nil {
			common.SysError("Redis billing stream ack failed: " + err.Error())
			continue
		}
		observeRedisBillingStreamState(ctx)
		if pending, pendingErr := common.RDB.XPending(ctx, redisBillingStream, redisBillingConsumerGroup).Result(); pendingErr == nil && pending.Count == 0 {
			if trimErr := common.RDB.XTrimMaxLenApprox(ctx, redisBillingStream, 200_000, 0).Err(); trimErr != nil {
				common.SysError("Redis billing stream trim failed: " + trimErr.Error())
			}
		}
	}
}

func observeRedisBillingStreamState(ctx context.Context) {
	pending, pendingErr := common.RDB.XPending(ctx, redisBillingStream, redisBillingConsumerGroup).Result()
	length, lengthErr := common.RDB.XLen(ctx, redisBillingStream).Result()
	if pendingErr == nil && lengthErr == nil {
		billingmetrics.ObserveRedisStreamState(pending.Count, length)
	}
}
