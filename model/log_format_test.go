package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]any{
		"model_price": 0.004,
		"admin_info": map[string]any{
			"quota_saturation": map[string]any{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "model_price")
}

func TestModelDiagnosticsAreVisibleOnlyToOperators(t *testing.T) {
	other := common.MapToJsonStr(map[string]any{
		"model_ratio":         1,
		"is_model_mapped":     true,
		"upstream_model_name": "legacy-upstream",
		"response_model": map[string]any{
			"requested_model": "requested",
			"upstream_model":  "legacy-upstream",
			"returned_model":  "legacy-returned",
		},
		"admin_info": map[string]any{
			"is_model_mapped":     true,
			"upstream_model_name": "scoped-upstream",
			"response_model": map[string]any{
				"requested_model": "requested",
				"upstream_model":  "scoped-upstream",
				"returned_model":  "scoped-returned",
			},
		},
	})

	t.Run("user", func(t *testing.T) {
		logs := []*Log{{Other: other}}
		formatSelfLogs(logs, 0, common.RoleCommonUser)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Equal(t, float64(1), parsed["model_ratio"])
		assert.NotContains(t, parsed, "admin_info")
		for _, key := range modelDiagnosticLogOtherKeys {
			assert.NotContains(t, parsed, key)
		}
	})

	t.Run("admin self view", func(t *testing.T) {
		logs := []*Log{{Other: other}}
		formatSelfLogs(logs, 0, common.RoleAdminUser)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Equal(t, "legacy-upstream", parsed["upstream_model_name"])
		adminInfo, ok := parsed["admin_info"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "scoped-upstream", adminInfo["upstream_model_name"])
	})

	t.Run("root self view", func(t *testing.T) {
		logs := []*Log{{Other: other}}
		formatSelfLogs(logs, 0, common.RoleRootUser)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Contains(t, parsed, "response_model")
		assert.Contains(t, parsed, "admin_info")
	})
}

func TestTaskPluginLogVisibilityIsRoleSeparated(t *testing.T) {
	other := common.MapToJsonStr(map[string]any{
		"model_price": 1.25,
		"admin_info": map[string]any{
			"task_plugin": map[string]any{
				"key":     "document-parser",
				"name":    "Document Parser",
				"version": "1.2.3",
			},
		},
		"root_info": map[string]any{
			"upstream_task_id": "upstream-private",
			"task_plugin": map[string]any{
				"generation": 42,
			},
		},
	})

	t.Run("user", func(t *testing.T) {
		logs := []*Log{{Other: other}}
		formatUserLogs(logs, 0)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.NotContains(t, parsed, "admin_info")
		assert.NotContains(t, parsed, "root_info")
		assert.Equal(t, 1.25, parsed["model_price"])
	})

	t.Run("admin", func(t *testing.T) {
		logs := []*Log{{Other: other}}
		FormatAdminLogs(logs)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Contains(t, parsed, "admin_info")
		assert.NotContains(t, parsed, "root_info")
	})

	t.Run("root", func(t *testing.T) {
		logs := []*Log{{Other: other}}
		FormatRootLogs(logs)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Contains(t, parsed, "admin_info")
		assert.Contains(t, parsed, "root_info")
	})
}

func TestLegacyLogOtherVisibilityIsRoleSeparated(t *testing.T) {
	other := common.MapToJsonStr(map[string]any{
		"request_path":  "/v1/chat/completions",
		"channel_id":    202,
		"channel_name":  "legacy-secret-channel",
		"channel_type":  1,
		"reject_reason": "legacy-policy-rejection",
		"admin_info": map[string]any{
			"existing_admin_field": "preserved",
		},
		"root_info": map[string]any{
			"upstream_request_id": "upstream-private",
		},
		"audit_info": map[string]any{
			"method": "POST",
		},
	})

	t.Run("user", func(t *testing.T) {
		logs := []*Log{{
			Id:          99,
			ChannelId:   77,
			ChannelName: "resolved-secret-channel",
			Other:       other,
		}}

		formatUserLogs(logs, 10)

		assert.Equal(t, 11, logs[0].Id)
		assert.Equal(t, 77, logs[0].ChannelId)
		assert.Empty(t, logs[0].ChannelName)
		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Equal(t, "/v1/chat/completions", parsed["request_path"])
		for _, key := range []string{
			"channel_id",
			"channel_name",
			"channel_type",
			"reject_reason",
			"admin_info",
			"root_info",
			"audit_info",
		} {
			assert.NotContains(t, parsed, key)
		}
	})

	t.Run("admin", func(t *testing.T) {
		logs := []*Log{{Other: other}}

		FormatAdminLogs(logs)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Equal(t, "legacy-secret-channel", parsed["channel_name"])
		assert.NotContains(t, parsed, "reject_reason")
		assert.NotContains(t, parsed, "root_info")
		assert.Contains(t, parsed, "audit_info")
		adminInfo, ok := parsed["admin_info"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "preserved", adminInfo["existing_admin_field"])
		assert.Equal(t, "legacy-policy-rejection", adminInfo["reject_reason"])
	})

	t.Run("root", func(t *testing.T) {
		logs := []*Log{{Other: other}}

		FormatRootLogs(logs)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Equal(t, "legacy-secret-channel", parsed["channel_name"])
		assert.NotContains(t, parsed, "reject_reason")
		assert.Contains(t, parsed, "root_info")
		assert.Contains(t, parsed, "audit_info")
		adminInfo, ok := parsed["admin_info"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "preserved", adminInfo["existing_admin_field"])
		assert.Equal(t, "legacy-policy-rejection", adminInfo["reject_reason"])
	})
}

func TestLegacyRejectReasonDoesNotOverrideScopedValue(t *testing.T) {
	other := common.MapToJsonStr(map[string]any{
		"reject_reason": "legacy-value",
		"admin_info": map[string]any{
			"reject_reason": "scoped-value",
		},
	})
	logs := []*Log{{Other: other}}

	FormatRootLogs(logs)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, parsed, "reject_reason")
	adminInfo, ok := parsed["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "scoped-value", adminInfo["reject_reason"])
}

func TestLegacyRejectReasonHandlesNullAdminInfo(t *testing.T) {
	logs := []*Log{{Other: `{"reject_reason":"legacy-value","admin_info":null}`}}

	FormatAdminLogs(logs)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, parsed, "reject_reason")
	adminInfo, ok := parsed["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "legacy-value", adminInfo["reject_reason"])
}

func TestLogFormattingPreservesLargeIntegerLexemes(t *testing.T) {
	const other = `{"public_id":9007199254740993,"admin_info":{"admin_id":9007199254740995},"root_info":{"generation":18446744073709551615}}`

	t.Run("user", func(t *testing.T) {
		logs := []*Log{{Other: other}}

		formatUserLogs(logs, 0)

		assert.Contains(t, logs[0].Other, `"public_id":9007199254740993`)
		assert.NotContains(t, logs[0].Other, "admin_id")
		assert.NotContains(t, logs[0].Other, "generation")
	})

	t.Run("admin", func(t *testing.T) {
		logs := []*Log{{Other: other}}

		FormatAdminLogs(logs)

		assert.Contains(t, logs[0].Other, `"public_id":9007199254740993`)
		assert.Contains(t, logs[0].Other, `"admin_id":9007199254740995`)
		assert.NotContains(t, logs[0].Other, "generation")
	})

	t.Run("root", func(t *testing.T) {
		logs := []*Log{{Other: other}}

		FormatRootLogs(logs)

		assert.Equal(t, other, logs[0].Other)
	})

	t.Run("unprivileged", func(t *testing.T) {
		const unprivileged = `{"public_id":9007199254740993,"model_price":0.004}`

		userLogs := []*Log{{Other: unprivileged}}
		formatUserLogs(userLogs, 0)
		assert.Equal(t, unprivileged, userLogs[0].Other)

		adminLogs := []*Log{{Other: unprivileged}}
		FormatAdminLogs(adminLogs)
		assert.Equal(t, unprivileged, adminLogs[0].Other)
	})
}

// TestUserLogViewsHideErrorLogs restores the P-12 contract lost in the rc.39
// merge: error logs (type 5) are filtered out of every self log view — list,
// export stream and token view, for every role — while the global log view used
// by administrators still returns them with their diagnostics.
func TestUserLogViewsHideErrorLogs(t *testing.T) {
	previousLogDB, previousLogType := LOG_DB, common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		LOG_DB = previousLogDB
		common.SetLogDatabaseType(previousLogType)
		require.NoError(t, sqlDB.Close())
	})

	for _, entry := range []Log{
		{UserId: 7, Username: "owner", TokenId: 71, Type: LogTypeConsume, RequestId: "consume-row", Other: `{"model_price":0.004}`},
		{UserId: 7, Username: "owner", TokenId: 71, Type: LogTypeError, RequestId: "error-row", Content: "upstream failed", Other: `{"admin_info":{"status_code":502}}`},
		{UserId: 8, Username: "stranger", TokenId: 81, Type: LogTypeError, RequestId: "other-error"},
	} {
		require.NoError(t, db.Create(&entry).Error)
	}

	for _, view := range []struct {
		name string
		read func() ([]*Log, int64, error)
	}{
		{"user list", func() ([]*Log, int64, error) { return GetUserLogs(7, LogFilter{}, 0, 10) }},
		{"admin self list", func() ([]*Log, int64, error) {
			return GetUserLogsForRole(7, common.RoleAdminUser, LogFilter{}, 0, 10)
		}},
		{"root self list", func() ([]*Log, int64, error) {
			return GetUserLogsForRole(7, common.RoleRootUser, LogFilter{}, 0, 10)
		}},
	} {
		logs, total, err := view.read()
		require.NoError(t, err, view.name)
		require.EqualValues(t, 1, total, view.name)
		require.Len(t, logs, 1, view.name)
		require.Equal(t, LogTypeConsume, logs[0].Type, view.name)
		require.Equal(t, "consume-row", logs[0].RequestId, view.name)
	}

	// An explicit error-type query inside a self view returns nothing.
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
		logs, total, err := GetUserLogsForRole(7, role, LogFilter{Type: LogTypeError}, 0, 10)
		require.NoError(t, err)
		require.Zero(t, total)
		require.Empty(t, logs)
	}

	tokenLogs, err := GetLogByTokenId(71)
	require.NoError(t, err)
	require.Len(t, tokenLogs, 1)
	require.Equal(t, LogTypeConsume, tokenLogs[0].Type)

	var exported []*Log
	require.NoError(t, StreamUserLogsForRole(context.Background(), 7, common.RoleAdminUser, LogFilter{}, func(entry *Log) error {
		exported = append(exported, entry)
		return nil
	}))
	require.Len(t, exported, 1)
	require.Equal(t, LogTypeConsume, exported[0].Type)

	// The global log view keeps the error row with its admin diagnostics.
	adminLogs, total, err := GetAllLogs(LogFilter{Type: LogTypeError, Username: "owner"}, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, adminLogs, 1)
	require.Equal(t, "error-row", adminLogs[0].RequestId)
	require.Contains(t, adminLogs[0].Other, `"admin_info"`)
	require.Contains(t, adminLogs[0].Other, `"status_code":502`)
}
