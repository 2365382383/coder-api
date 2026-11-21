package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanvasCatalogUsesEnabledModelsAndGroupTypes(t *testing.T) {
	previousGroups := setting.UserUsableGroups2JSONString()
	previousTypes, previousAssignments := ratio_setting.GroupTypeOptions2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousGroups))
		require.NoError(t, ratio_setting.UpdateGroupTypeOptionsByJSONString(previousTypes, previousAssignments))
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupTypeOptionsByJSONString(`{}`, `{}`))
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
	assert.Contains(t, options, CanvasModelOption{Name: "speech-model", Group: "default", Capability: "text"})
	assert.Contains(t, options, CanvasModelOption{Name: "metadata-media", Group: "default", Capability: "text"})
	assert.Contains(t, options, CanvasModelOption{Name: "gpt-image-without-metadata", Group: "default", Capability: "text"})
	assert.NotContains(t, options, CanvasModelOption{Name: "speech-model", Group: "default", Capability: "audio"})
	assert.NotContains(t, options, CanvasModelOption{Name: "metadata-media", Group: "default", Capability: "image"})
	assert.NotContains(t, options, CanvasModelOption{Name: "metadata-media", Group: "default", Capability: "video"})
	for _, option := range options {
		assert.NotEqual(t, "private-not-usable", option.Group)
		assert.Equal(t, "text", option.Capability)
	}
	capabilities := DefaultImageCapabilities()
	require.NotNil(t, capabilities)
	assert.Equal(t, "aspect_ratio_resolution", capabilities.SizeMode)
	assert.Equal(t, []string{"1K", "2K", "4K"}, capabilities.Resolutions)
	assert.Equal(t, 10, capabilities.MaxImages)
	imageGroups, err := GetUserImageModelGroups("default")
	require.NoError(t, err)
	require.Empty(t, imageGroups)
}

func TestCanvasCatalogDoesNotInferVideoFromChannelRoutes(t *testing.T) {
	previousTypes, previousAssignments := ratio_setting.GroupTypeOptions2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupTypeOptionsByJSONString(previousTypes, previousAssignments))
	})
	require.NoError(t, ratio_setting.UpdateGroupTypeOptionsByJSONString(`{}`, `{}`))
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
		Name:       "video-route-without-metadata",
		Group:      "video-route",
		Capability: "text",
	})
	assert.NotContains(t, options, CanvasModelOption{Name: "video-route-without-metadata", Group: "video-route", Capability: "video"})
}

func TestCanvasCatalogUsesMediaGroupTypesWithoutEndpointFiltering(t *testing.T) {
	previousGroups := setting.UserUsableGroups2JSONString()
	previousTypes, previousAssignments := ratio_setting.GroupTypeOptions2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousGroups))
		require.NoError(t, ratio_setting.UpdateGroupTypeOptionsByJSONString(previousTypes, previousAssignments))
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","image-group":"Image","video-group":"Video","audio-group":"Audio","untyped-image":"Untyped"}`))
	require.NoError(t, ratio_setting.UpdateGroupTypeOptionsByJSONString(
		`{"image-type":{"name":"图片","order":1},"video-type":{"name":"视频","order":2},"audio-type":{"name":"音频","order":3}}`,
		`{"image-group":"image-type","video-group":"video-type","audio-group":"audio-type"}`,
	))

	truncate(t)
	model.InitColumnNamesForTest()
	require.NoError(t, model.DB.AutoMigrate(&model.Ability{}, &model.Channel{}, &model.Model{}))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id IN ?", []int{907, 908, 909, 910}).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Where("id IN ?", []int{907, 908, 909, 910}).Delete(&model.Channel{}).Error)
		require.NoError(t, model.DB.Where("model_name = ?", "image-with-endpoint-metadata").Delete(&model.Model{}).Error)
	})
	for _, channel := range []model.Channel{
		{Id: 907, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "image-group"},
		{Id: 908, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "video-group"},
		{Id: 909, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "audio-group"},
		{Id: 910, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "untyped-image"},
	} {
		require.NoError(t, model.DB.Create(&channel).Error)
	}
	for _, ability := range []model.Ability{
		{Group: "image-group", Model: "image-without-endpoint-metadata", ChannelId: 907, Enabled: true},
		{Group: "video-group", Model: "video-without-endpoint-metadata", ChannelId: 908, Enabled: true},
		{Group: "audio-group", Model: "audio-without-endpoint-metadata", ChannelId: 909, Enabled: true},
		{Group: "untyped-image", Model: "image-with-endpoint-metadata", ChannelId: 910, Enabled: true},
	} {
		require.NoError(t, model.DB.Create(&ability).Error)
	}
	require.NoError(t, model.DB.Create(&model.Model{
		ModelName: "image-with-endpoint-metadata",
		Endpoints: `{"image-generation":{"path":"/v1/images/generations","method":"POST"}}`,
	}).Error)

	options, err := CanvasModelCatalog("default")
	require.NoError(t, err)
	assert.Contains(t, options, CanvasModelOption{Name: "image-without-endpoint-metadata", Group: "image-group", Capability: "image"})
	assert.Contains(t, options, CanvasModelOption{Name: "video-without-endpoint-metadata", Group: "video-group", Capability: "video", VideoAdapter: "openai"})
	assert.Contains(t, options, CanvasModelOption{Name: "audio-without-endpoint-metadata", Group: "audio-group", Capability: "audio"})
	assert.Contains(t, options, CanvasModelOption{Name: "image-with-endpoint-metadata", Group: "untyped-image", Capability: "text"})
	assert.NotContains(t, options, CanvasModelOption{Name: "image-with-endpoint-metadata", Group: "untyped-image", Capability: "image"})
}
