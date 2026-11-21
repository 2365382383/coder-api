package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanvasCatalogUsesGroupScopedRoutingAndExplicitEndpointMetadata(t *testing.T) {
	truncate(t)
	model.InitColumnNamesForTest()
	require.NoError(t, model.DB.AutoMigrate(&model.Ability{}, &model.Model{}))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id IN ?", []int{901, 902, 903, 904, 905}).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Where("model_name IN ?", []string{"public-model", "speech-model", "metadata-media"}).Delete(&model.Model{}).Error)
	})
	for _, channel := range []model.Channel{
		{Id: 901, Type: constant.ChannelTypeCodex, Status: common.ChannelStatusEnabled, Name: "text"},
		{Id: 902, Type: constant.ChannelTypeSora, Status: common.ChannelStatusEnabled, Name: "video"},
		{Id: 903, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "audio"},
		{Id: 904, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "metadata"},
		{Id: 905, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "legacy-image"},
	} {
		require.NoError(t, model.DB.Create(&channel).Error)
	}
	for _, ability := range []model.Ability{
		{Group: "default", Model: "public-model", ChannelId: 901, Enabled: true},
		{Group: "private-not-usable", Model: "public-model", ChannelId: 902, Enabled: true},
		{Group: "default", Model: "speech-model", ChannelId: 903, Enabled: true},
		{Group: "default", Model: "metadata-media", ChannelId: 904, Enabled: true},
		{Group: "default", Model: "gpt-image-without-metadata", ChannelId: 905, Enabled: true},
	} {
		require.NoError(t, model.DB.Create(&ability).Error)
	}
	require.NoError(t, model.DB.Create(&model.Model{ModelName: "speech-model", Endpoints: `{"speech":{"path":"/v1/audio/speech","method":"POST"}}`}).Error)
	require.NoError(t, model.DB.Create(&model.Model{ModelName: "public-model", Endpoints: `{"openai-response":{"path":"/v1/responses","method":"POST"}}`}).Error)
	require.NoError(t, model.DB.Create(&model.Model{ModelName: "metadata-media", Endpoints: `{"image-generation":{"path":"/v1/images/generations","method":"POST"},"openai-video":{"path":"/v1/videos","method":"POST"}}`}).Error)
	options, err := CanvasModelCatalog("default")
	require.NoError(t, err)
	assert.Contains(t, options, CanvasModelOption{Name: "public-model", Group: "default", Capability: "text"})
	assert.Contains(t, options, CanvasModelOption{Name: "speech-model", Group: "default", Capability: "audio"})
	assert.Contains(t, options, CanvasModelOption{Name: "metadata-media", Group: "default", Capability: "image"})
	assert.Contains(t, options, CanvasModelOption{Name: "metadata-media", Group: "default", Capability: "video", VideoAdapter: "openai"})
	assert.NotContains(t, options, CanvasModelOption{Name: "gpt-image-without-metadata", Group: "default", Capability: "image"}, "channel defaults cannot bypass model metadata")
	assert.NotContains(t, options, CanvasModelOption{Name: "public-model", Group: "default", Capability: "video"}, "same model on a hidden group's video channel cannot leak capabilities")
	for _, option := range options {
		assert.NotEqual(t, "private-not-usable", option.Group)
	}
	capabilities := DefaultImageCapabilities()
	require.NotNil(t, capabilities)
	assert.Equal(t, "aspect_ratio_resolution", capabilities.SizeMode)
	assert.Equal(t, []string{"1K", "2K", "4K"}, capabilities.Resolutions)
	assert.Equal(t, 10, capabilities.MaxImages)
	imageGroups, err := GetUserImageModelGroups("default")
	require.NoError(t, err)
	require.Len(t, imageGroups, 1)
	require.Len(t, imageGroups[0].Models, 1)
	assert.Equal(t, "metadata-media", imageGroups[0].Models[0].Value)
	assert.Equal(t, "aspect_ratio_resolution", imageGroups[0].Models[0].Capabilities.SizeMode)
	assert.Equal(t, []string{"1K", "2K", "4K"}, imageGroups[0].Models[0].Capabilities.Resolutions)
}

func TestCanvasCatalogIncludesResolvedVideoRoutesWithoutModelMetadata(t *testing.T) {
	truncate(t)
	model.InitColumnNamesForTest()
	require.NoError(t, model.DB.AutoMigrate(&model.Ability{}, &model.Channel{}))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id = ?", 906).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Where("id = ?", 906).Delete(&model.Channel{}).Error)
	})
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:     906,
		Type:   constant.ChannelTypeSora,
		Status: common.ChannelStatusEnabled,
		Name:   "video-route-without-metadata",
	}).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group:     "video-route",
		Model:     "video-route-without-metadata",
		ChannelId: 906,
		Enabled:   true,
	}).Error)

	options, err := CanvasModelCatalog("video-route")
	require.NoError(t, err)
	assert.Contains(t, options, CanvasModelOption{
		Name:         "video-route-without-metadata",
		Group:        "video-route",
		Capability:   "video",
		VideoAdapter: "sora",
	})
}
