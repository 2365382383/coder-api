package model

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	billingmetrics "github.com/QuantumNous/new-api/pkg/billing_metrics"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const billingTransactionAttempts = 5

const billingLockWaitTimeoutEnv = "BILLING_LOCK_WAIT_TIMEOUT_SECONDS"

type billingQueueKey struct {
	kind string
	id   int
}

type billingQueueEntry struct {
	slot chan struct{}
	refs int
}

type billingOwnerEntry struct {
	userID    int
	channelID int
	expiresAt time.Time
}

var billingQueues = struct {
	sync.Mutex
	entries map[billingQueueKey]*billingQueueEntry
}{entries: make(map[billingQueueKey]*billingQueueEntry)}

var billingOwners = struct {
	sync.Mutex
	entries map[string]billingOwnerEntry
}{entries: make(map[string]billingOwnerEntry)}

const (
	billingOwnerCacheTTL        = 10 * time.Minute
	billingOwnerCacheMaxEntries = 10_000
)

func rememberBillingOwner(requestID string, userID, channelID int) {
	if requestID == "" || userID <= 0 {
		return
	}
	billingOwners.Lock()
	if len(billingOwners.entries) >= billingOwnerCacheMaxEntries {
		now := time.Now()
		for requestID, entry := range billingOwners.entries {
			if !now.Before(entry.expiresAt) {
				delete(billingOwners.entries, requestID)
			}
		}
		if len(billingOwners.entries) >= billingOwnerCacheMaxEntries {
			for requestID := range billingOwners.entries {
				delete(billingOwners.entries, requestID)
				break
			}
		}
	}
	billingOwners.entries[requestID] = billingOwnerEntry{
		userID:    userID,
		channelID: channelID,
		expiresAt: time.Now().Add(billingOwnerCacheTTL),
	}
	billingOwners.Unlock()
}

func forgetBillingOwner(requestID string) {
	if requestID == "" {
		return
	}
	billingOwners.Lock()
	delete(billingOwners.entries, requestID)
	billingOwners.Unlock()
}

func cachedBillingOwner(requestID string) (userID, channelID int, found bool) {
	if requestID == "" {
		return 0, 0, false
	}
	now := time.Now()
	billingOwners.Lock()
	entry, found := billingOwners.entries[requestID]
	if found && now.Before(entry.expiresAt) {
		billingOwners.Unlock()
		return entry.userID, entry.channelID, true
	}
	if found {
		delete(billingOwners.entries, requestID)
	}
	billingOwners.Unlock()
	return 0, 0, false
}

// withBillingQueue serializes short database transaction phases that mutate
// the same balance or usage counter. The database row lock remains the
// correctness boundary across processes. The in-process queue keeps requests
// from piling up inside MySQL while waiting for that row and consuming
// connections that could be used by other users.
func withBillingQueue(ctx context.Context, keys []billingQueueKey, fn func() error) error {
	if fn == nil {
		return errors.New("billing queue callback is nil")
	}
	if len(keys) == 0 {
		return fn()
	}
	if ctx == nil {
		ctx = context.Background()
	}

	validKeys := make([]billingQueueKey, 0, len(keys))
	entries := make([]*billingQueueEntry, 0, len(keys))
	billingQueues.Lock()
	for _, key := range keys {
		if key.id <= 0 {
			continue
		}
		entry := billingQueues.entries[key]
		if entry == nil {
			entry = &billingQueueEntry{slot: make(chan struct{}, 1)}
			entry.slot <- struct{}{}
			billingQueues.entries[key] = entry
		}
		entry.refs++
		validKeys = append(validKeys, key)
		entries = append(entries, entry)
	}
	billingQueues.Unlock()
	if len(entries) == 0 {
		return fn()
	}

	releaseReferences := func() {
		billingQueues.Lock()
		for i, key := range validKeys {
			entry := entries[i]
			entry.refs--
			if entry.refs == 0 {
				delete(billingQueues.entries, key)
			}
		}
		billingQueues.Unlock()
	}

	for i, entry := range entries {
		select {
		case <-ctx.Done():
			for j := i - 1; j >= 0; j-- {
				entries[j].slot <- struct{}{}
			}
			releaseReferences()
			return ctx.Err()
		case <-entry.slot:
		}
	}

	defer func() {
		for i := len(entries) - 1; i >= 0; i-- {
			entries[i].slot <- struct{}{}
		}
		releaseReferences()
	}()
	return fn()
}

func withBillingUserQueue(ctx context.Context, userID int, fn func() error) error {
	return withBillingQueue(ctx, []billingQueueKey{{kind: "user", id: userID}}, fn)
}

func withBillingUserAndChannelQueue(ctx context.Context, userID, channelID int, fn func() error) error {
	return withBillingQueue(ctx, []billingQueueKey{
		{kind: "user", id: userID},
		{kind: "channel", id: channelID},
	}, fn)
}

// withRetryableDatabaseTransaction retries the complete database transaction
// after a transient deadlock or serialization failure. Retrying the whole
// closure is required so every row read and write observes a fresh snapshot
// and lock set.
func withRetryableDatabaseTransaction(ctx context.Context, fn func(*gorm.DB) error) error {
	return withRetryableDatabaseTransactionObserved(ctx, fn, nil)
}

type billingTransactionAttemptObserver func(attempt int, err error, duration time.Duration, willRetry bool)

func withRetryableDatabaseTransactionObserved(ctx context.Context, fn func(*gorm.DB) error, observer billingTransactionAttemptObserver) error {
	if fn == nil {
		return errors.New("database transaction callback is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var err error
	for attempt := 0; attempt < billingTransactionAttempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		attemptStarted := time.Now()
		err = DB.WithContext(ctx).Transaction(fn)
		retryable := isRetryableDatabaseTransactionError(err)
		willRetry := retryable && attempt < billingTransactionAttempts-1
		if observer != nil {
			observer(attempt, err, time.Since(attemptStarted), willRetry)
		}
		if !willRetry {
			return err
		}
		// A lock can outlive the statement that reported it (especially on
		// SQLite's single-writer fallback), so use a bounded exponential pause
		// instead of immediately re-entering the same contention window.
		delay := time.Duration(50*(1<<attempt)+int(time.Now().UnixNano()%7)) * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

func withBillingTransaction(ctx context.Context, fn func(*gorm.DB) error) error {
	return withBillingTransactionNamed(ctx, billingTransactionCaller(2), fn)
}

func withBillingTransactionForUser(ctx context.Context, userID int, fn func(*gorm.DB) error) error {
	return withBillingTransactionForUserNamed(ctx, userID, billingTransactionCaller(2), fn)
}

func withBillingTransactionForUserNamed(ctx context.Context, userID int, operation string, fn func(*gorm.DB) error) error {
	waitStarted := time.Now()
	return withBillingUserQueue(ctx, userID, func() error {
		billingmetrics.ObserveQueueWait(operation, time.Since(waitStarted))
		return withBillingTransactionNamed(ctx, operation, fn)
	})
}

func withBillingTransactionForUserAndChannel(ctx context.Context, userID, channelID int, fn func(*gorm.DB) error) error {
	return withBillingTransactionForUserAndChannelNamed(ctx, userID, channelID, billingTransactionCaller(2), fn)
}

func withBillingTransactionForUserAndChannelNamed(ctx context.Context, userID, channelID int, operation string, fn func(*gorm.DB) error) error {
	waitStarted := time.Now()
	return withBillingUserAndChannelQueue(ctx, userID, channelID, func() error {
		billingmetrics.ObserveQueueWait(operation, time.Since(waitStarted))
		return withBillingTransactionNamed(ctx, operation, fn)
	})
}

func billingTransactionOwner(ctx context.Context, requestID string) (userID, channelID int) {
	if userID, channelID, found := cachedBillingOwner(requestID); found {
		return userID, channelID
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var root BillingOperation
	if err := DB.WithContext(ctx).Select("user_id", "channel_id").Where("request_id = ? AND phase = ?", requestID, "initial").First(&root).Error; err != nil {
		return 0, 0
	}
	rememberBillingOwner(requestID, root.UserId, root.ChannelId)
	return root.UserId, root.ChannelId
}

func withBillingTransactionForRequest(ctx context.Context, requestID string, fn func(*gorm.DB) error) error {
	return withBillingTransactionForRequestNamed(ctx, requestID, billingTransactionCaller(2), fn)
}

func withBillingTransactionForRequestNamed(ctx context.Context, requestID, operation string, fn func(*gorm.DB) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	userID, _ := billingTransactionOwner(ctx, requestID)
	if userID > 0 {
		waitStarted := time.Now()
		return withBillingUserQueue(ctx, userID, func() error {
			billingmetrics.ObserveQueueWait(operation, time.Since(waitStarted))
			return withBillingTransactionNamed(ctx, operation, fn)
		})
	}
	return withBillingTransactionNamed(ctx, operation, fn)
}

func withBillingTransactionForRequestAndChannel(ctx context.Context, requestID string, fn func(*gorm.DB) error) error {
	return withBillingTransactionForRequestAndChannelNamed(ctx, requestID, billingTransactionCaller(2), fn)
}

func withBillingTransactionForRequestAndChannelNamed(ctx context.Context, requestID, operation string, fn func(*gorm.DB) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	userID, channelID := billingTransactionOwner(ctx, requestID)
	if userID > 0 {
		waitStarted := time.Now()
		return withBillingUserAndChannelQueue(ctx, userID, channelID, func() error {
			billingmetrics.ObserveQueueWait(operation, time.Since(waitStarted))
			return withBillingTransactionNamed(ctx, operation, fn)
		})
	}
	return withBillingTransactionNamed(ctx, operation, fn)
}

func withBillingTransactionForSubscription(ctx context.Context, subscriptionID int, fn func(*gorm.DB) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	operation := billingTransactionCaller(2)
	var subscription UserSubscription
	if err := DB.WithContext(ctx).Select("user_id").Where("id = ?", subscriptionID).First(&subscription).Error; err == nil && subscription.UserId > 0 {
		waitStarted := time.Now()
		return withBillingUserQueue(ctx, subscription.UserId, func() error {
			billingmetrics.ObserveQueueWait(operation, time.Since(waitStarted))
			return withBillingTransactionNamed(ctx, operation, fn)
		})
	}
	return withBillingTransactionNamed(ctx, operation, fn)
}

func withBillingTransactionNamed(ctx context.Context, operation string, fn func(*gorm.DB) error) error {
	started := time.Now()
	retries := 0
	err := withRetryableDatabaseTransactionObserved(ctx, func(tx *gorm.DB) error {
		return withBillingLockWaitTimeout(tx, fn)
	}, func(_ int, attemptErr error, duration time.Duration, willRetry bool) {
		result := "success"
		if attemptErr != nil {
			result = "failure"
			if willRetry {
				result = "retry"
				retries++
			}
		}
		billingmetrics.ObserveAttempt(operation, result, billingErrorCode(attemptErr), duration)
	})
	result := "success"
	if err != nil {
		result = "failure"
	}
	billingmetrics.ObserveFinal(operation, result, billingErrorCode(err), time.Since(started), retries)
	return err
}

func billingTransactionCaller(skip int) string {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return "unknown"
	}
	name := runtime.FuncForPC(pc).Name()
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		name = name[dot+1:]
	}
	if name == "" {
		return "unknown"
	}
	return name
}

func billingErrorCode(err error) string {
	if err == nil {
		return "none"
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return "mysql_" + strconv.Itoa(int(mysqlErr.Number))
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return "postgres_" + pgErr.Code
	}
	if isRetryableDatabaseTransactionError(err) {
		return "retryable_database"
	}
	return "application"
}

// withBillingLockWaitTimeout bounds each MySQL billing attempt before the
// existing whole-transaction retry path runs. The session value is restored
// before the transaction is committed so pooled connections do not leak the
// billing-specific timeout into unrelated queries.
func withBillingLockWaitTimeout(tx *gorm.DB, fn func(*gorm.DB) error) (err error) {
	if fn == nil {
		return errors.New("billing transaction callback is nil")
	}
	if !common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return fn(tx)
	}
	timeoutSeconds := common.GetEnvOrDefault(billingLockWaitTimeoutEnv, 10)
	if timeoutSeconds <= 0 {
		return fn(tx)
	}

	var previousTimeout int
	if err = tx.Raw("SELECT @@SESSION.innodb_lock_wait_timeout").Scan(&previousTimeout).Error; err != nil {
		return err
	}
	if err = tx.Exec("SET SESSION innodb_lock_wait_timeout = ?", timeoutSeconds).Error; err != nil {
		return err
	}
	defer func() {
		if restoreErr := tx.Exec("SET SESSION innodb_lock_wait_timeout = ?", previousTimeout).Error; err == nil && restoreErr != nil {
			err = restoreErr
		}
	}()
	return fn(tx)
}

func isRetryableDatabaseTransactionError(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1205 || mysqlErr.Number == 1213
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40P01" || pgErr.Code == "40001"
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "deadlock found") || strings.Contains(message, "could not serialize access") || strings.Contains(message, "database is locked")
}

func lockBillingUser(tx *gorm.DB, userID int) error {
	if userID <= 0 {
		return errors.New("invalid billing user id")
	}
	var user User
	return lockForUpdate(tx).Select("id").Where("id = ?", userID).First(&user).Error
}

func lockBillingSubscription(tx *gorm.DB, subscriptionID int) error {
	if subscriptionID <= 0 {
		return errors.New("invalid billing subscription id")
	}
	var subscription UserSubscription
	return lockForUpdate(tx).Select("id").Where("id = ?", subscriptionID).First(&subscription).Error
}

func lockBillingToken(tx *gorm.DB, tokenID int) error {
	if tokenID <= 0 {
		return errors.New("invalid billing token id")
	}
	var token Token
	return lockForUpdate(tx).Select("id").Where("id = ?", tokenID).First(&token).Error
}
