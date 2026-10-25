package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterPricingByUsableGroupsRedactsHiddenGroups(t *testing.T) {
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalSpecialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.ReadAll()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
		specialGroups.Clear()
		specialGroups.AddAll(originalSpecialGroups)
	})

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"public":"Public group"}`))
	specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	specialGroups.Clear()

	pricing := []model.Pricing{
		{ModelName: "shared-model", EnableGroup: []string{"public", "member", "private"},
			GroupEndpointTypes:    map[string][]constant.EndpointType{"public": {constant.EndpointTypeOpenAI}, "member": {constant.EndpointTypeAnthropic}, "private": {constant.EndpointTypeOpenAIVideo}},
			GroupPluginKeys:       map[string][]string{"public": {"public-provider"}, "member": {"public-provider"}, "private": {"private-provider"}},
			BillingPluginVariants: []model.PricingPluginVariant{{PluginKey: "public-provider", BillingMode: "tiered_expr", BillingExpr: "public expression"}, {PluginKey: "private-provider", BillingMode: "tiered_expr", BillingExpr: "hidden expression"}},
		},
		{ModelName: "private-model", EnableGroup: []string{"private"}},
		{ModelName: "all-model", EnableGroup: []string{"all", "private"}},
	}

	usableGroups := service.GetUserUsableGroups("member")
	require.Len(t, usableGroups, 2)
	assert.Contains(t, usableGroups, "public")
	assert.Contains(t, usableGroups, "member")
	filtered := filterPricingByUsableGroups(pricing, usableGroups)

	require.Len(t, filtered, 2)
	assert.Equal(t, "shared-model", filtered[0].ModelName)
	assert.Equal(t, []string{"public", "member"}, filtered[0].EnableGroup)
	assert.NotContains(t, filtered[0].GroupEndpointTypes, "private")
	assert.NotContains(t, filtered[0].GroupPluginKeys, "private")
	require.Len(t, filtered[0].BillingPluginVariants, 1)
	assert.Equal(t, "public-provider", filtered[0].BillingPluginVariants[0].PluginKey)
	assert.Equal(t, "public expression", filtered[0].BillingExpr)
	assert.Contains(t, pricing[0].GroupEndpointTypes, "private", "shared cache must remain unmodified")
	assert.Len(t, pricing[0].BillingPluginVariants, 2)
	assert.Equal(t, "all-model", filtered[1].ModelName)
	assert.Equal(t, []string{"all"}, filtered[1].EnableGroup)
	assert.Equal(t, []string{"public", "member", "private"}, pricing[0].EnableGroup)
	assert.Equal(t, []string{"all", "private"}, pricing[2].EnableGroup)
}

func TestFilterPricingByUsableGroupsRejectsEmptyVisibility(t *testing.T) {
	pricing := []model.Pricing{
		{ModelName: "all-model", EnableGroup: []string{"all"}},
	}

	filtered := filterPricingByUsableGroups(pricing, map[string]string{})

	assert.Empty(t, filtered)
}

func TestPricingCatalogDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct{ name, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(dialect.name, func(t *testing.T) {
			if dialect.env != "" && os.Getenv(dialect.env) == "" {
				t.Skip("set " + dialect.env)
			}
			db := modelManagementDB(t, dialect.name, os.Getenv(dialect.env))
			oldUsable := setting.UserUsableGroups2JSONString()
			oldTypes, oldMap := ratio_setting.GroupTypeOptions2JSONString()
			oldRatios := ratio_setting.GroupRatio2JSONString()
			oldSpecial := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.ReadAll()
			oldOverrides := ratio_setting.GroupGroupRatio2JSONString()
			t.Cleanup(func() {
				require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldUsable))
				require.NoError(t, ratio_setting.UpdateGroupTypeOptionsByJSONString(oldTypes, oldMap))
				require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldRatios))
				ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Clear()
				ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.AddAll(oldSpecial)
				require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(oldOverrides))
				model.InvalidatePricingCache()
			})
			require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"public":"Public access","auto":"Automatic routing"}`))
			const ratios = `{"public":2,"vip":0.5,"private":0.01,"auto":1}`
			require.NoError(t, db.Create(&model.Option{Key: "GroupRatio", Value: ratios}).Error)
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(ratios))
			ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Clear()
			ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Set("member", map[string]string{"+:vip": "Member access"})
			require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"member":{"public":0.75}}`))
			definitions := `{"alpha":{"name":"Custom alpha","order":2},"beta":{"name":"Custom beta","order":1},"secret":{"name":"Hidden category","order":0}}`
			assignments := `{"public":"alpha","vip":"beta","private":"secret"}`
			require.NoError(t, model.UpdateGroupTypeOptions(definitions, assignments))
			for index, incoming := range []string{"/v1/chat/completions", "/v1/responses"} {
				channel := model.Channel{Type: constant.ChannelTypeAdvancedCustom, Status: common.ChannelStatusEnabled, Key: "test-only", Name: incoming}
				channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{IncomingPath: incoming, UpstreamPath: incoming}}}})
				require.NoError(t, db.Create(&channel).Error)
				group := "public"
				if index == 1 {
					group = "private"
				}
				require.NoError(t, db.Create(&model.Ability{Group: group, Model: "shared-model", ChannelId: channel.Id, Enabled: true}).Error)
				if index == 0 {
					require.NoError(t, db.Create(&model.Ability{Group: "vip", Model: "shared-model", ChannelId: channel.Id, Enabled: true}).Error)
				}
			}
			model.InvalidatePricingCache()
			type catalogResponse struct {
				Success bool                             `json:"success"`
				Data    []model.Pricing                  `json:"data"`
				Types   []service.PricingGroupTypeOption `json:"group_types"`
				Groups  []service.PricingGroupOption     `json:"groups"`
				Version string                           `json:"group_catalog_version"`
				Ratios  map[string]float64               `json:"group_ratio"`
			}
			member := model.User{Username: "catalog-member", Group: "member"}
			require.NoError(t, db.Create(&member).Error)
			read := func(userID int) catalogResponse {
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)
				if userID != 0 {
					ctx.Set("id", userID)
				}
				GetPricing(ctx)
				var response catalogResponse
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
				require.True(t, response.Success, recorder.Body.String())
				return response
			}
			public := read(0)
			assert.Equal(t, float64(1), public.Ratios["auto"], "legacy Auto ratio remains available without becoming an ordinary category group")
			require.Len(t, public.Groups, 1)
			require.Len(t, public.Types, 1)
			assert.Equal(t, "alpha", public.Types[0].ID)
			require.Len(t, public.Data, 1)
			assert.Equal(t, []string{"public"}, public.Data[0].EnableGroup)
			assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, public.Data[0].SupportedEndpointTypes)
			assert.NotContains(t, public.Data[0].GroupEndpointTypes, "private")
			assert.Equal(t, public.Version, read(0).Version)
			personal := read(member.Id)
			require.Len(t, personal.Groups, 2)
			assert.Equal(t, "beta", personal.Types[0].ID)
			assert.Equal(t, 0.75, personal.Groups[1].Ratio)
			assert.NotEqual(t, public.Version, personal.Version)
			assert.ElementsMatch(t, []string{"alpha", "beta"}, personal.Data[0].GroupTypeIDs)
			ordinary := model.User{Username: "catalog-ordinary", Group: "regular", AffCode: "catalog-ordinary"}
			require.NoError(t, db.Create(&ordinary).Error)
			assert.Equal(t, public.Version, read(ordinary.Id).Version)
			admin := model.User{Username: "catalog-admin", Group: "member", Role: common.RoleAdminUser, AffCode: "catalog-admin"}
			require.NoError(t, db.Create(&admin).Error)
			assert.Equal(t, personal.Version, read(admin.Id).Version, "admin status must not expose other pricing groups")
			require.NoError(t, model.UpdateGroupTypeOptions(`{"alpha":{"name":"Renamed category","icon":"UnknownIcon","order":0}}`, `{"public":"alpha"}`))
			renamed := read(0)
			assert.Equal(t, "alpha", renamed.Types[0].ID)
			assert.Equal(t, "Renamed category", renamed.Types[0].Name)
			assert.NotEqual(t, public.Version, renamed.Version)
			require.NoError(t, model.UpdateGroupTypeOptions(`{}`, `{}`))
			unclassified := read(0)
			assert.Empty(t, unclassified.Types)
			require.Len(t, unclassified.Groups, 1)
			assert.Nil(t, unclassified.Groups[0].TypeID)
			assert.Len(t, unclassified.Data, 1)
		})
	}
}
