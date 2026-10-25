package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configureUserGroupOptionsTest(t *testing.T) {
	t.Helper()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalGroupTypes := ratio_setting.GroupTypes2JSONString()
	originalGroupTypeMap := ratio_setting.GroupTypeMap2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		require.NoError(t, ratio_setting.UpdateGroupTypesByJSONString(originalGroupTypes))
		require.NoError(t, ratio_setting.UpdateGroupTypeMapByJSONString(originalGroupTypeMap))
	})

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default access","vip":"VIP access","auto":"Auto"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2}`))
	require.NoError(t, ratio_setting.UpdateGroupTypesByJSONString(`{"domestic":{"name":"Domestic","icon":"Qwen","order":2},"vendor":{"name":"Vendor","order":1}}`))
	require.NoError(t, ratio_setting.UpdateGroupTypeMapByJSONString(`{"vip":"vendor","default":"missing"}`))
}

func TestGetUserGroupOptionsIncludesGroupTypes(t *testing.T) {
	configureUserGroupOptionsTest(t)

	options := GetUserGroupOptions("")

	require.Contains(t, options.Groups, "vip")
	assert.Equal(t, float64(2), options.Groups["vip"].Ratio)
	require.NotNil(t, options.Groups["vip"].Type)
	assert.Equal(t, "vendor", options.Groups["vip"].Type.ID)
	assert.Equal(t, "Vendor", options.Groups["vip"].Type.Name)

	assert.Nil(t, options.Groups["default"].Type, "dangling type assignments must be treated as unclassified")
	assert.Nil(t, options.Groups["auto"].Type)
	assert.Equal(t, "auto", options.Groups["auto"].Ratio)

	require.Len(t, options.Types, 2)
	assert.Equal(t, "vendor", options.Types[0].ID)
	assert.Equal(t, "domestic", options.Types[1].ID)
}

func TestGetUserGroupOptionsWithoutGroupTypes(t *testing.T) {
	configureUserGroupOptionsTest(t)
	require.NoError(t, ratio_setting.UpdateGroupTypesByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateGroupTypeMapByJSONString(`{}`))

	options := GetUserGroupOptions("")

	assert.Empty(t, options.Types)
	for name, group := range options.Groups {
		assert.Nil(t, group.Type, "%s must not have a group type", name)
	}
}

func TestGroupTypeOptionsLoadFromConfig(t *testing.T) {
	previousConfig := config.GlobalConfig.ExportAllConfigs()
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(previousConfig))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"group_ratio_setting.group_ratio":    `{"default":1}`,
		"group_ratio_setting.group_types":    `{"vendor":{"name":"Vendor","icon":"OpenAI","order":1}}`,
		"group_ratio_setting.group_type_map": `{"default":"vendor"}`,
	}))

	options := GetUserGroupOptions("")
	require.NotNil(t, options.Groups["default"].Type)
	assert.Equal(t, "vendor", options.Groups["default"].Type.ID)
	assert.Equal(t, "Vendor", options.Groups["default"].Type.Name)
	require.Len(t, options.Types, 1)
	assert.Equal(t, "vendor", options.Types[0].ID)
}

func TestCheckGroupTypeSettings(t *testing.T) {
	require.NoError(t, ratio_setting.CheckGroupTypes(`{"vendor":{"name":"Vendor","order":1}}`))
	require.Error(t, ratio_setting.CheckGroupTypes(`{"vendor":{"name":""}}`))
	require.Error(t, ratio_setting.CheckGroupTypes(`{"vendor":{"name":"bad\u0000name"}}`))
	require.Error(t, ratio_setting.CheckGroupTypes(`{"vendor":{"name":"`+strings.Repeat("x", 65)+`"}}`))
	require.Error(t, ratio_setting.CheckGroupTypes(`{"a":{"name":"Same"},"b":{"name":"same"}}`))
	require.Error(t, ratio_setting.CheckGroupTypes(`{"__auto__":{"name":"Automatic routing"}}`))
	require.Error(t, ratio_setting.CheckGroupTypes(`null`))
	require.Error(t, ratio_setting.CheckGroupTypes(`not-json`))

	require.NoError(t, ratio_setting.CheckGroupTypeMap(`{"default":"vendor"}`))
	require.Error(t, ratio_setting.CheckGroupTypeMap(`{"default":""}`))
	require.Error(t, ratio_setting.CheckGroupTypeMap(`{"":"vendor"}`))
	require.Error(t, ratio_setting.CheckGroupTypeMap(`{" auto ":"vendor"}`))
	require.Error(t, ratio_setting.CheckGroupTypeMap(`{"auto":"vendor"}`))
	require.Error(t, ratio_setting.CheckGroupTypeMap(`null`))
	require.Error(t, ratio_setting.CheckGroupTypeMap(`not-json`))

	groupTypes := `{"vendor":{"name":"Vendor","order":1}}`
	require.NoError(t, ratio_setting.CheckGroupTypeOptions(groupTypes, `{"default":"vendor"}`, map[string]float64{"default": 1}))
	require.NoError(t, ratio_setting.CheckGroupTypeOptions(groupTypes, `{"AUTO":"vendor"}`, map[string]float64{"AUTO": 1}), "ordinary group names remain case sensitive")
	require.Error(t, ratio_setting.CheckGroupTypeOptions(groupTypes, `{"default":"missing"}`, map[string]float64{"default": 1}))
	require.Error(t, ratio_setting.CheckGroupTypeOptions(groupTypes, `{"retired":"vendor"}`, map[string]float64{"default": 1}))
}

func TestBuildPricingGroupCatalogUsesDynamicTypesAndDeduplicatesModels(t *testing.T) {
	alpha := GroupTypeOption{ID: "alpha", Name: "Alpha", Icon: "Alpha", Order: 2}
	beta := GroupTypeOption{ID: "beta", Name: "Beta", Icon: "Beta", Order: 1}
	options := UserGroupOptions{
		Types: []GroupTypeOption{alpha, beta},
		Groups: map[string]UserGroupOption{
			"alpha-group": {
				Ratio: 2.0, Desc: "Alpha access", Type: &alpha,
			},
			"beta-group": {
				Ratio: 0.5, Desc: "Beta access", Type: &beta,
			},
			"unclassified": {Ratio: 1.0, Desc: "Legacy access"},
			"auto":         {Ratio: "auto", Desc: "Automatic routing"},
		},
	}
	pricing := []model.Pricing{
		{ModelName: "shared-model", EnableGroup: []string{"alpha-group", "beta-group"}},
		{ModelName: "legacy-model", EnableGroup: []string{"unclassified"}},
		{ModelName: "all-model", EnableGroup: []string{"all"}},
	}

	catalog, modelTypeIDs, err := BuildPricingGroupCatalog(pricing, options)
	require.NoError(t, err)

	require.NotEmpty(t, catalog.Version)
	require.Equal(t, []string{"beta", "alpha"}, []string{catalog.Types[0].ID, catalog.Types[1].ID})
	assert.Equal(t, 1, catalog.Types[0].GroupCount)
	assert.Equal(t, 2, catalog.Types[0].ModelCount)
	assert.Equal(t, 1, catalog.Types[1].GroupCount)
	assert.Equal(t, 2, catalog.Types[1].ModelCount)
	assert.ElementsMatch(t, []string{"beta", "alpha"}, modelTypeIDs["shared-model"])
	assert.ElementsMatch(t, []string{"beta", "alpha"}, modelTypeIDs["all-model"])
	assert.Empty(t, modelTypeIDs["legacy-model"])

	require.Len(t, catalog.Groups, 3)
	assert.Equal(t, []string{"beta-group", "alpha-group", "unclassified"}, []string{
		catalog.Groups[0].Name,
		catalog.Groups[1].Name,
		catalog.Groups[2].Name,
	})
	assert.Nil(t, catalog.Groups[2].TypeID)
	assert.Equal(t, 2, catalog.Groups[0].ModelCount)
	assert.Equal(t, 2, catalog.Groups[1].ModelCount)
	assert.Equal(t, 2, catalog.Groups[2].ModelCount)

	again, _, err := BuildPricingGroupCatalog([]model.Pricing{pricing[2], pricing[1], pricing[0]}, options)
	require.NoError(t, err)
	assert.Equal(t, catalog.Version, again.Version, "input ordering must not change the directory version")
	options.Groups["unclassified"] = UserGroupOption{Ratio: 0.0, Desc: "Free access"}
	updated, _, err := BuildPricingGroupCatalog(pricing, options)
	require.NoError(t, err)
	assert.NotEqual(t, catalog.Version, updated.Version)
	assert.Equal(t, float64(0), updated.Groups[2].Ratio)
	options.Groups["unclassified"] = UserGroupOption{Ratio: "unknown"}
	_, _, err = BuildPricingGroupCatalog(pricing, options)
	assert.Error(t, err, "invalid prices must not become free")
}
