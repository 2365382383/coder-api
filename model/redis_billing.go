package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

var ErrRedisBillingUnavailable = errors.New("redis billing fast path unavailable")

// RedisBillingFastPathEnabled is the single switch for the ordinary wallet
// and token quota cache path. Durable subscription/task accounting does not
// use this path.
func RedisBillingFastPathEnabled() bool {
	return common.RedisEnabled && common.RDB != nil && common.GetEnvOrDefaultBool("BILLING_REDIS_FAST_PATH", true)
}

// Keep these helpers as the billing-facing names while using the same hashes
// as authentication. A successful quota operation therefore updates the exact
// balance that TokenAuth and the trust-quota check read.
func redisBillingUserKey(userID int) string {
	return getUserCacheKey(userID)
}

func redisBillingTokenKey(tokenKey string) string {
	return getTokenCacheKey(tokenKey)
}

func readRedisBillingUserQuota(ctx context.Context, userID int) (int64, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	values, err := common.RDB.HMGet(ctx, redisBillingUserKey(userID), "Id", "CacheSchema", "Quota").Result()
	if err != nil {
		return 0, false, err
	}
	if len(values) != 3 || values[0] == nil || values[1] == nil || values[2] == nil {
		return 0, false, nil
	}
	if fmt.Sprint(values[0]) != strconv.Itoa(userID) || fmt.Sprint(values[1]) != strconv.Itoa(userCacheSchemaVersion) {
		return 0, false, nil
	}
	quota, err := strconv.ParseInt(fmt.Sprint(values[2]), 10, 64)
	if err != nil {
		return 0, false, nil
	}
	return quota, true, nil
}

func hydrateRedisBillingUserQuota(ctx context.Context, userID int) (int64, error) {
	user, err := GetUserById(userID, false)
	if err != nil {
		return 0, err
	}
	if err := populateUserCache(*user); err != nil {
		return 0, fmt.Errorf("hydrate Redis user cache: %w", err)
	}
	return user.Quota, nil
}

func RedisBillingUserQuota(userID int) (int64, error) {
	if !RedisBillingFastPathEnabled() || userID <= 0 {
		return 0, ErrRedisBillingUnavailable
	}
	ctx, cancel := quotaCacheContext()
	defer cancel()
	if quota, found, err := readRedisBillingUserQuota(ctx, userID); err != nil {
		return 0, err
	} else if found {
		return quota, nil
	}
	return hydrateRedisBillingUserQuota(ctx, userID)
}

func InvalidateRedisBillingUserQuota(userID int) error {
	if !common.RedisEnabled || common.RDB == nil || userID <= 0 {
		return nil
	}
	return invalidateUserCache(userID)
}

func AdjustRedisBillingUserQuota(userID int, delta int64) error {
	if !common.RedisEnabled || common.RDB == nil || userID <= 0 || delta == 0 {
		return nil
	}
	ctx, cancel := quotaCacheContext()
	defer cancel()
	exists, err := common.RDB.Exists(ctx, redisBillingUserKey(userID)).Result()
	if err != nil || exists == 0 {
		return err
	}
	result, err := cacheApplyUserQuotaDelta(userID, delta)
	if err != nil {
		return err
	}
	if result != cacheQuotaOK {
		return ErrRedisBillingUnavailable
	}
	return nil
}

func invalidateRedisBillingTokenQuota(tokenKey string) error {
	if !common.RedisEnabled || common.RDB == nil || tokenKey == "" {
		return nil
	}
	return common.RDB.Del(context.Background(), redisBillingTokenKey(tokenKey)).Err()
}

func InvalidateRedisBillingTokenQuota(tokenKey string) error {
	return invalidateRedisBillingTokenQuota(tokenKey)
}

func adjustRedisBillingTokenQuota(tokenID int, tokenKey string, delta int64) error {
	if !common.RedisEnabled || common.RDB == nil || tokenID <= 0 || tokenKey == "" || delta == 0 {
		return nil
	}
	ctx, cancel := quotaCacheContext()
	defer cancel()
	cachedID, err := common.RDB.HGet(ctx, redisBillingTokenKey(tokenKey), "Id").Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil || cachedID != strconv.Itoa(tokenID) {
		return err
	}
	result, err := cacheApplyTokenQuotaDelta(tokenID, tokenKey, delta)
	if err != nil {
		return err
	}
	if result != cacheQuotaOK {
		return ErrRedisBillingUnavailable
	}
	return nil
}

// AdjustRedisBillingTokenQuota applies a committed database delta to the
// shared Token cache when it is warm. A cold cache is intentionally untouched;
// the next authentication read hydrates it from the database.
func AdjustRedisBillingTokenQuota(tokenID int, tokenKey string, delta int64) error {
	return adjustRedisBillingTokenQuota(tokenID, tokenKey, delta)
}

// AdjustRedisBillingTokenQuotaByID is used by durable database paths that keep
// only a token ID in their operation record. The extra lookup is limited to
// those non-hot paths and avoids introducing the secret token key into the
// billing operation table.
func AdjustRedisBillingTokenQuotaByID(tokenID int, delta int64) error {
	if tokenID <= 0 || delta == 0 {
		return nil
	}
	var token Token
	if err := DB.Select("id", commonKeyCol).Where("id = ?", tokenID).First(&token).Error; err != nil {
		return err
	}
	return adjustRedisBillingTokenQuota(token.Id, token.Key, delta)
}

func ensureRedisBillingUserQuota(ctx context.Context, userID int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, found, err := readRedisBillingUserQuota(ctx, userID); err != nil {
		return err
	} else if found {
		return nil
	}
	_, err := hydrateRedisBillingUserQuota(ctx, userID)
	return err
}

func ensureRedisBillingTokenQuota(ctx context.Context, tokenID int, tokenKey string) (string, error) {
	if tokenID <= 0 || tokenKey == "" {
		return "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := redisBillingTokenKey(tokenKey)
	cachedID, err := common.RDB.HGet(ctx, key, "Id").Result()
	if err == nil && cachedID == strconv.Itoa(tokenID) {
		remainExists, remainErr := common.RDB.HExists(ctx, key, "RemainQuota").Result()
		usedExists, usedErr := common.RDB.HExists(ctx, key, "UsedQuota").Result()
		if remainErr != nil {
			return "", remainErr
		}
		if usedErr != nil {
			return "", usedErr
		}
		if remainExists && usedExists {
			return key, nil
		}
	} else if err != nil && !errors.Is(err, redis.Nil) {
		return "", err
	}

	var token Token
	if err := DB.WithContext(ctx).Where("id = ?", tokenID).First(&token).Error; err != nil {
		return "", err
	}
	if _, err := cacheInitToken(token); err != nil {
		return "", fmt.Errorf("hydrate Redis token cache: %w", err)
	}
	return key, nil
}
