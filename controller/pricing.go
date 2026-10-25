package controller

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	imagedto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func filterPricingByUsableGroups(pricing []model.Pricing, usableGroup map[string]string) []model.Pricing {
	if len(pricing) == 0 {
		return pricing
	}
	if len(usableGroup) == 0 {
		return []model.Pricing{}
	}

	filtered := make([]model.Pricing, 0, len(pricing))
	for _, item := range pricing {
		visibleGroups := make([]string, 0, len(item.EnableGroup))
		if slices.Contains(item.EnableGroup, "all") {
			visibleGroups = append(visibleGroups, "all")
		} else {
			for _, group := range item.EnableGroup {
				if group == "auto" || group == "" {
					continue
				}
				if _, ok := usableGroup[group]; ok {
					visibleGroups = append(visibleGroups, group)
				}
			}
		}
		if len(visibleGroups) > 0 {
			item.EnableGroup = visibleGroups
			if item.GroupEndpointTypes != nil {
				endpoints := make(map[string][]constant.EndpointType)
				images := make(map[string]*imagedto.ImageModelCapabilities)
				plugins := make(map[string][]string)
				allowedPlugins := make(map[string]bool)
				publicEndpoints := make([]constant.EndpointType, 0)
				for _, group := range visibleGroups {
					plugins[group] = item.GroupPluginKeys[group]
					for _, key := range plugins[group] {
						allowedPlugins[key] = true
					}
					endpoints[group] = item.GroupEndpointTypes[group]
					for _, endpoint := range endpoints[group] {
						if !slices.Contains(publicEndpoints, endpoint) {
							publicEndpoints = append(publicEndpoints, endpoint)
						}
					}
					if capability := item.GroupImageCapabilities[group]; capability != nil {
						images[group] = capability
					}
				}
				for _, endpoint := range item.SupportedEndpointTypes {
					if _, standard := common.GetDefaultEndpointInfo(endpoint); !standard && !slices.Contains(publicEndpoints, endpoint) {
						publicEndpoints = append(publicEndpoints, endpoint)
					}
				}
				slices.Sort(publicEndpoints)
				item.GroupEndpointTypes = endpoints
				if item.GroupPluginKeys != nil {
					item.GroupPluginKeys = plugins
					if len(allowedPlugins) == 0 {
						item.BillingUsageSchema, item.BillingUsageExamples = nil, nil
					}
					if len(item.BillingPluginVariants) > 0 {
						variants := make([]model.PricingPluginVariant, 0)
						for _, variant := range item.BillingPluginVariants {
							if allowedPlugins[variant.PluginKey] {
								variants = append(variants, variant)
							}
						}
						item.BillingPluginVariants = variants
						if len(variants) > 0 {
							first := variants[0]
							item.BillingMode, item.BillingExpr = first.BillingMode, first.BillingExpr
							item.BillingUsageSchema, item.BillingUsageExamples = first.BillingUsageSchema, first.BillingUsageExamples
						} else {
							item.BillingUsageSchema, item.BillingUsageExamples = nil, nil
						}
					}
				}
				item.GroupImageCapabilities = images
				item.SupportedEndpointTypes = publicEndpoints
				if len(images) == 0 {
					item.ImageCapabilities = nil
				} else if len(images) == 1 {
					for _, capability := range images {
						item.ImageCapabilities = capability
					}
				}
			}
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func GetPricing(c *gin.Context) {
	pricing := model.GetPricing()
	userId, exists := c.Get("id")
	var group string
	if exists {
		user, err := model.GetUserCache(userId.(int))
		if err == nil {
			group = user.Group
		}
	}

	groupOptions := service.GetUserGroupOptions(group)
	pricing = filterPricingByUsableGroups(pricing, groupOptions.UsableGroups)
	groupCatalog, modelTypeIDs, err := service.BuildPricingGroupCatalog(pricing, groupOptions)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for index := range pricing {
		pricing[index].GroupTypeIDs = modelTypeIDs[pricing[index].ModelName]
	}
	groupRatio := make(map[string]float64, len(groupCatalog.Groups))
	for _, catalogGroup := range groupCatalog.Groups {
		groupRatio[catalogGroup.Name] = catalogGroup.Ratio
	}
	// Keep the legacy numeric Auto entry for existing API consumers. Auto is
	// deliberately absent from the new fixed-price group directory.
	if _, visible := groupOptions.UsableGroups["auto"]; visible {
		if ratio, configured := ratio_setting.GetGroupRatioCopy()["auto"]; configured {
			if override, exists := ratio_setting.GetGroupGroupRatio(group, "auto"); exists {
				ratio = override
			}
			groupRatio["auto"] = ratio
		}
	}

	c.JSON(200, gin.H{
		"success":               true,
		"data":                  pricing,
		"vendors":               model.GetVendors(),
		"group_ratio":           groupRatio,
		"usable_group":          groupOptions.UsableGroups,
		"supported_endpoint":    model.GetSupportedEndpointMap(),
		"auto_groups":           service.GetUserAutoGroup(group),
		"group_catalog_version": groupCatalog.Version,
		"group_types":           groupCatalog.Types,
		"groups":                groupCatalog.Groups,
		"pricing_version":       "a42d372ccf0b5dd13ecf71203521f9d2",
	})
}

func ResetModelRatio(c *gin.Context) {
	defaultStr := ratio_setting.DefaultModelRatio2JSONString()
	err := model.UpdateOption("ModelRatio", defaultStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	err = ratio_setting.UpdateModelRatioByJSONString(defaultStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccessT(c, "Model ratios reset", nil)
}
