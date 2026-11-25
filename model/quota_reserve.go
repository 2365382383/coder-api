package model

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type cacheQuotaResult int

const (
	cacheQuotaInsufficient cacheQuotaResult = iota
	cacheQuotaOK
	cacheQuotaMiss
)

const userQuotaReserveScript = `
if redis.call('HGET', KEYS[1], 'Id') ~= ARGV[2]
  or redis.call('HGET', KEYS[1], 'CacheSchema') ~= ARGV[3]
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
local function compare(a, b)
  local function normalize(value)
    value = value or '0'
    local sign = ''
    local first = string.sub(value, 1, 1)
    if first == '-' then
      sign = '-'
      value = string.sub(value, 2)
    elseif first == '+' then
      value = string.sub(value, 2)
    end
    value = string.gsub(value, '^0+', '')
    if value == '' then value = '0' end
    if value == '0' then return '0' end
    return sign .. value
  end
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
local current = redis.call('HGET', KEYS[1], 'Quota')
if compare(current, ARGV[1]) < 0 then return 0 end
local result = redis.pcall('HINCRBY', KEYS[1], 'Quota', '-' .. ARGV[1])
if type(result) == 'table' and result.err then return -2 end
return 1`

const userQuotaDeltaScript = `
if redis.call('HGET', KEYS[1], 'Id') ~= ARGV[2]
  or redis.call('HGET', KEYS[1], 'CacheSchema') ~= ARGV[3]
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
local result = redis.pcall('HINCRBY', KEYS[1], 'Quota', ARGV[1])
if type(result) == 'table' and result.err then return -2 end
return 1`

const tokenQuotaReserveScript = `
if redis.call('HGET', KEYS[1], 'Id') ~= ARGV[2]
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
local function compare(a, b)
  local function normalize(value)
    value = value or '0'
    local sign = ''
    local first = string.sub(value, 1, 1)
    if first == '-' then
      sign = '-'
      value = string.sub(value, 2)
    elseif first == '+' then
      value = string.sub(value, 2)
    end
    value = string.gsub(value, '^0+', '')
    if value == '' then value = '0' end
    if value == '0' then return '0' end
    return sign .. value
  end
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
if redis.call('HGET', KEYS[1], 'Id') ~= ARGV[2] then return -1 end
local remain = redis.call('HGET', KEYS[1], 'RemainQuota')
if not remain then return -1 end
if compare(remain, ARGV[1]) < 0 then return 0 end
local remainResult = redis.pcall('HINCRBY', KEYS[1], 'RemainQuota', '-' .. ARGV[1])
if type(remainResult) == 'table' and remainResult.err then return -2 end
local usedResult = redis.pcall('HINCRBY', KEYS[1], 'UsedQuota', ARGV[1])
if type(usedResult) == 'table' and usedResult.err then
  redis.call('HINCRBY', KEYS[1], 'RemainQuota', ARGV[1])
  return -2
end
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

const tokenQuotaDeltaScript = `
local function opposite(value)
  if string.sub(value, 1, 1) == '-' then return string.sub(value, 2) end
  return '-' .. value
end
if redis.call('HGET', KEYS[1], 'Id') ~= ARGV[2] then return -1 end
local remainResult = redis.pcall('HINCRBY', KEYS[1], 'RemainQuota', ARGV[1])
if type(remainResult) == 'table' and remainResult.err then return -2 end
local usedResult = redis.pcall('HINCRBY', KEYS[1], 'UsedQuota', opposite(ARGV[1]))
if type(usedResult) == 'table' and usedResult.err then
  redis.call('HINCRBY', KEYS[1], 'RemainQuota', opposite(ARGV[1]))
  return -2
end
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

func quotaResultFromLua(result int, err error) (cacheQuotaResult, error) {
	if err != nil {
		return cacheQuotaMiss, err
	}
	switch result {
	case 1:
		return cacheQuotaOK, nil
	case 0:
		return cacheQuotaInsufficient, nil
	default:
		return cacheQuotaMiss, nil
	}
}

func quotaCacheContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 750*time.Millisecond)
}

func cacheTryReserveUserQuota(userID int, amount int64) (cacheQuotaResult, error) {
	ctx, cancel := quotaCacheContext()
	defer cancel()
	if err := ensureRedisBillingUserQuota(ctx, userID); err != nil {
		return cacheQuotaMiss, err
	}
	result, err := common.RDB.Eval(ctx, userQuotaReserveScript, []string{redisBillingUserKey(userID)}, amount, userID, userCacheSchemaVersion).Int()
	return quotaResultFromLua(result, err)
}

func cacheApplyUserQuotaDelta(userID int, delta int64) (cacheQuotaResult, error) {
	ctx, cancel := quotaCacheContext()
	defer cancel()
	result, err := common.RDB.Eval(ctx, userQuotaDeltaScript, []string{redisBillingUserKey(userID)}, delta, userID, userCacheSchemaVersion).Int()
	return quotaResultFromLua(result, err)
}

func cacheTryReserveTokenQuota(id int, key string, amount int64) (cacheQuotaResult, error) {
	ctx, cancel := quotaCacheContext()
	defer cancel()
	cacheKey, err := ensureRedisBillingTokenQuota(ctx, id, key)
	if err != nil {
		return cacheQuotaMiss, err
	}
	result, err := common.RDB.Eval(ctx, tokenQuotaReserveScript, []string{cacheKey}, amount, id, common.GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

func cacheApplyTokenQuotaDelta(id int, key string, delta int64) (cacheQuotaResult, error) {
	ctx, cancel := quotaCacheContext()
	defer cancel()
	result, err := common.RDB.Eval(ctx, tokenQuotaDeltaScript, []string{redisBillingTokenKey(key)}, delta, id, common.GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

func persistUserQuotaDelta(id int, delta int64) error {
	if delta == 0 {
		return nil
	}
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUserQuota, id, delta)
		return nil
	}
	return updateUserQuotaWithDeltaTx(DB, id, delta, nil)
}

func persistTokenQuotaDelta(id int, delta int64) error {
	if delta == 0 {
		return nil
	}
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeTokenQuota, id, delta)
		return nil
	}
	return updateTokenQuotaDeltaTx(DB, id, delta)
}

func reserveUserQuotaDB(id int, quota int64) (bool, error) {
	return reserveUserQuotaTx(DB, id, quota)
}

func reserveUserQuotaTx(tx *gorm.DB, id int, quota int64) (bool, error) {
	result := tx.Model(&User{}).
		Where("id = ? AND quota >= ?", id, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return true, nil
	}
	var user User
	if err := tx.Select("id", "quota").Where("id = ?", id).First(&user).Error; err != nil {
		return false, err
	}
	return false, nil
}

func reserveTokenQuotaDB(id int, quota int64) (bool, error) {
	return reserveTokenQuotaTx(DB, id, quota)
}

func reserveUserQuotaDBAndInvalidateCache(id int, quota int64) (bool, error) {
	reserved, err := reserveUserQuotaDB(id, quota)
	if err == nil && reserved {
		if cacheErr := InvalidateRedisBillingUserQuota(id); cacheErr != nil {
			common.SysLog(common.LogText("failed to invalidate user quota cache after database fallback: %s", cacheErr.Error()))
		}
	}
	return reserved, err
}

func reserveTokenQuotaDBAndInvalidateCache(id int, key string, quota int64) (bool, error) {
	reserved, err := reserveTokenQuotaDB(id, quota)
	if err == nil && reserved {
		if cacheErr := InvalidateRedisBillingTokenQuota(key); cacheErr != nil {
			common.SysLog(common.LogText("failed to invalidate token quota cache after database fallback: %s", cacheErr.Error()))
		}
	}
	return reserved, err
}

func reserveTokenQuotaTx(tx *gorm.DB, id int, quota int64) (bool, error) {
	result := tx.Model(&Token{}).
		Where("id = ? AND remain_quota >= ? AND remain_quota >= ? AND used_quota <= ?", id, quota, common.MinWalletQuota+quota, common.MaxWalletQuota-quota).
		Updates(map[string]any{
			"remain_quota":  gorm.Expr("remain_quota - ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": common.GetTimestamp(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return true, nil
	}
	var token Token
	if err := tx.Select("id", "remain_quota").Where("id = ?", id).First(&token).Error; err != nil {
		return false, err
	}
	if token.RemainQuota < quota {
		return false, nil
	}
	return false, common.ErrWalletQuotaOverflow
}

func TryReserveUserQuotaTx(tx *gorm.DB, id int, quota int64) (bool, error) {
	if tx == nil || quota < 0 {
		return false, errors.New("invalid quota reservation")
	}
	if quota == 0 {
		return true, nil
	}
	return reserveUserQuotaTx(tx, id, quota)
}

func TryReserveTokenQuotaTx(tx *gorm.DB, id int, quota int64, unlimited bool) (bool, error) {
	if tx == nil || quota < 0 {
		return false, errors.New("invalid token reservation")
	}
	if quota == 0 {
		return true, nil
	}
	if unlimited {
		return true, updateTokenQuotaDeltaTx(tx, id, -quota)
	}
	return reserveTokenQuotaTx(tx, id, quota)
}

// TryReserveUserQuota uses the upstream Redis-cache reservation path.
func TryReserveUserQuota(id int, quota int) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota cannot be negative")
	}
	if quota == 0 {
		return true, nil
	}
	if !RedisBillingFastPathEnabled() {
		return reserveUserQuotaDBAndInvalidateCache(id, int64(quota))
	}
	result, err := cacheTryReserveUserQuota(id, int64(quota))
	if err != nil || result == cacheQuotaMiss {
		if err != nil {
			common.SysLog(common.LogText("user quota cache reserve unavailable, falling back to database: %s", err.Error()))
		}
		return reserveUserQuotaDBAndInvalidateCache(id, int64(quota))
	}
	if result == cacheQuotaInsufficient {
		return false, nil
	}
	if err := persistUserQuotaDelta(id, -int64(quota)); err != nil {
		compensated, compensateErr := cacheApplyUserQuotaDelta(id, int64(quota))
		if compensateErr != nil || compensated != cacheQuotaOK {
			common.SysError(common.LogText("failed to compensate reserved user quota: result=%d error=%v", compensated, compensateErr))
		}
		return false, err
	}
	return true, nil
}

// TryReserveTokenQuota uses the same Redis cache plus batch persistence model.
func TryReserveTokenQuota(id int, key string, quota int, unlimited bool) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota cannot be negative")
	}
	if quota == 0 {
		return true, nil
	}
	if unlimited {
		err := DecreaseTokenQuota(id, key, quota)
		return err == nil, err
	}
	if !RedisBillingFastPathEnabled() {
		return reserveTokenQuotaDBAndInvalidateCache(id, key, int64(quota))
	}
	result, err := cacheTryReserveTokenQuota(id, key, int64(quota))
	if err != nil || result == cacheQuotaMiss {
		if err != nil {
			common.SysLog(common.LogText("token quota cache reserve unavailable, falling back to database: %s", err.Error()))
		}
		return reserveTokenQuotaDBAndInvalidateCache(id, key, int64(quota))
	}
	if result == cacheQuotaInsufficient {
		return false, nil
	}
	if err := persistTokenQuotaDelta(id, -int64(quota)); err != nil {
		compensated, compensateErr := cacheApplyTokenQuotaDelta(id, key, int64(quota))
		if compensateErr != nil || compensated != cacheQuotaOK {
			common.SysError(common.LogText("failed to compensate reserved token quota: result=%d error=%v", compensated, compensateErr))
		}
		return false, err
	}
	return true, nil
}
