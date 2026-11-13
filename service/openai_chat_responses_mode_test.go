package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatCompletionsResponsesUpgradeUsesConfiguredPolicy(t *testing.T) {
	policy := model_setting.ChatCompletionsToResponsesPolicy{
		Enabled:       true,
		AllChannels:   true,
		ModelPatterns: []string{"^gpt-5.*$"},
	}

	require.True(t, ShouldChatCompletionsUseResponsesPolicy(policy, 1, 1, "gpt-5"))
	assert.False(t, ShouldChatCompletionsUseResponsesPolicy(policy, 1, 1, "claude-sonnet"))
	policy.AllChannels = false
	policy.ChannelIDs = []int{10}
	policy.ChannelTypes = []int{20}
	assert.True(t, ShouldChatCompletionsUseResponsesPolicy(policy, 10, 1, "gpt-5"))
	assert.True(t, ShouldChatCompletionsUseResponsesPolicy(policy, 1, 20, "gpt-5"))
	assert.False(t, ShouldChatCompletionsUseResponsesPolicy(policy, 1, 1, "gpt-5"))

	settings := model_setting.GetGlobalSettings()
	previous := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() { settings.ChatCompletionsToResponsesPolicy = previous })
	settings.ChatCompletionsToResponsesPolicy = policy
	assert.True(t, ShouldChatCompletionsUseResponsesGlobal(10, 1, "gpt-5"))
	settings.ChatCompletionsToResponsesPolicy.Enabled = false
	assert.False(t, ShouldChatCompletionsUseResponsesGlobal(10, 1, "gpt-5"))
}
