package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func optionsRevision(t *testing.T) int64 {
	t.Helper()
	revisions, err := GetLatestSyncRevisions()
	require.NoError(t, err)
	return revisions[SyncTopicOptions]
}

func useSensitiveWordMigrationDB(t *testing.T) {
	t.Helper()
	db := useFrontendOptionMigrationDB(t)
	require.NoError(t, db.AutoMigrate(
		&SystemTask{}, &User{}, &SensitiveWordRule{}, &SensitiveWordRuleWord{},
		&SensitiveWordRuleGroup{}, &SensitiveWordPolicy{}, &SensitiveWordAuditEvent{},
	))
	setSensitiveWordRuntimeUnavailable(false)
	t.Cleanup(func() {
		setSensitiveWordRuntimeUnavailable(false)
	})
}

func TestMigrateSensitiveWordsKeepsLocalRulesAndIndependentPlugins(t *testing.T) {
	tests := []struct {
		name         string
		checkEnabled string
		checkPrompt  string
		wantPolicyOn bool
	}{
		{name: "both old switches on", checkEnabled: "true", checkPrompt: "true", wantPolicyOn: true},
		{name: "prompt switch off", checkEnabled: "true", checkPrompt: "false", wantPolicyOn: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			useSensitiveWordMigrationDB(t)
			pluginInstances := `[{"id":"keep","plugin":"request-redact","enabled":true}]`
			options := []Option{
				{Key: "CheckSensitiveEnabled", Value: tc.checkEnabled},
				{Key: "CheckSensitiveOnPromptEnabled", Value: tc.checkPrompt},
				{Key: "StopOnSensitiveEnabled", Value: "false"},
				{Key: "SensitiveWords", Value: " Bad \n\nworse\r\n"},
				{Key: "StreamCacheQueueLength", Value: "3"},
				{Key: request_plugin_setting.InstancesOption, Value: pluginInstances},
			}
			require.NoError(t, DB.Create(&options).Error)

			require.NoError(t, MigrateSensitiveWords())
			for _, key := range retiredSensitiveOptionKeys {
				requireOptionMissing(t, DB, key)
			}
			assert.Equal(t, "3", requireOptionValue(t, DB, "StreamCacheQueueLength"))
			assert.Equal(t, pluginInstances, requireOptionValue(t, DB, request_plugin_setting.InstancesOption))

			policy := GetSensitiveWordPolicy()
			assert.Equal(t, tc.wantPolicyOn, policy.Enabled && policy.CheckPrompt)
			rules, err := ListSensitiveWordRules()
			require.NoError(t, err)
			require.Len(t, rules, 1)
			assert.Equal(t, SensitiveWordScopeGlobal, rules[0].Scope)
			assert.Equal(t, SensitiveWordModeBlock, rules[0].Mode)
			detail, err := GetSensitiveWordRuleDetail(rules[0].ID)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"Bad", "worse"}, detail.Words)

			revision := optionsRevision(t)
			assert.NotZero(t, revision)
			require.NoError(t, MigrateSensitiveWords())
			assert.Equal(t, revision, optionsRevision(t))
			assert.Equal(t, pluginInstances, requireOptionValue(t, DB, request_plugin_setting.InstancesOption))
		})
	}
}

func TestMigrateSensitiveWordsWithoutWordsAndInvalidWords(t *testing.T) {
	t.Run("no old options", func(t *testing.T) {
		useSensitiveWordMigrationDB(t)
		require.NoError(t, MigrateSensitiveWords())
		assert.Zero(t, optionsRevision(t))
	})

	t.Run("empty list retires old switches", func(t *testing.T) {
		useSensitiveWordMigrationDB(t)
		require.NoError(t, DB.Create(&[]Option{
			{Key: "CheckSensitiveEnabled", Value: "true"},
			{Key: "SensitiveWords", Value: " \n "},
		}).Error)
		require.NoError(t, MigrateSensitiveWords())
		for _, key := range retiredSensitiveOptionKeys {
			requireOptionMissing(t, DB, key)
		}
		requireOptionMissing(t, DB, request_plugin_setting.InstancesOption)
	})

	t.Run("word list without saved enable switches stays disabled", func(t *testing.T) {
		useSensitiveWordMigrationDB(t)
		require.NoError(t, DB.Create(&Option{Key: "SensitiveWords", Value: "legacy-word"}).Error)
		require.NoError(t, MigrateSensitiveWords())
		policy := GetSensitiveWordPolicy()
		assert.False(t, policy.Enabled)
		assert.False(t, policy.CheckPrompt)
		rules, err := ListSensitiveWordRules()
		require.NoError(t, err)
		require.Len(t, rules, 1)
		assert.Equal(t, SensitiveWordModeBlock, rules[0].Mode)
	})

	t.Run("invalid legacy data remains available for repair", func(t *testing.T) {
		useSensitiveWordMigrationDB(t)
		legacy := strings.Repeat("超长旧词", 80)
		require.Greater(t, len([]rune(legacy)), SensitiveWordMaxRunes)
		require.NoError(t, DB.Create(&[]Option{
			{Key: "CheckSensitiveEnabled", Value: "true"},
			{Key: "SensitiveWords", Value: legacy},
		}).Error)
		require.Error(t, MigrateSensitiveWords())
		assert.Equal(t, legacy, requireOptionValue(t, DB, "SensitiveWords"))
		assert.Equal(t, "true", requireOptionValue(t, DB, "CheckSensitiveEnabled"))
		var markerCount int64
		require.NoError(t, DB.Model(&Option{}).Where("key = ?", sensitiveWordMigrationKey).Count(&markerCount).Error)
		assert.Zero(t, markerCount)
	})
}
