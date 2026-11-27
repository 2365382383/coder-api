package model

import (
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	imagedto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/imagecapability"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

type PricingPluginVariant struct {
	PluginKey            string                               `json:"plugin_key"`
	PluginName           string                               `json:"plugin_name"`
	Icon                 string                               `json:"icon,omitempty"`
	BillingExpr          string                               `json:"billing_expr"`
	BillingMode          string                               `json:"billing_mode"`
	BillingUsageSchema   map[string]jsplugin.UsageFieldSchema `json:"billing_usage_schema"`
	BillingUsageExamples []jsplugin.UsageExample              `json:"billing_usage_examples,omitempty"`
}

type Pricing struct {
	BillingPluginVariants  []PricingPluginVariant                      `json:"billing_plugin_variants,omitempty"`
	ModelName              string                                      `json:"model_name"`
	Description            string                                      `json:"description,omitempty"`
	Icon                   string                                      `json:"icon,omitempty"`
	Tags                   string                                      `json:"tags,omitempty"`
	VendorID               int                                         `json:"vendor_id,omitempty"`
	QuotaType              int                                         `json:"quota_type"`
	ModelRatio             float64                                     `json:"model_ratio"`
	ModelPrice             float64                                     `json:"model_price"`
	OwnerBy                string                                      `json:"owner_by"`
	CompletionRatio        float64                                     `json:"completion_ratio"`
	CacheRatio             *float64                                    `json:"cache_ratio,omitempty"`
	CreateCacheRatio       *float64                                    `json:"create_cache_ratio,omitempty"`
	ImageRatio             *float64                                    `json:"image_ratio,omitempty"`
	AudioRatio             *float64                                    `json:"audio_ratio,omitempty"`
	AudioCompletionRatio   *float64                                    `json:"audio_completion_ratio,omitempty"`
	EnableGroup            []string                                    `json:"enable_groups"`
	GroupTypeIDs           []string                                    `json:"group_type_ids,omitempty"`
	GroupPluginKeys        map[string][]string                         `json:"group_plugin_keys,omitempty"`
	GroupEndpointTypes     map[string][]constant.EndpointType          `json:"group_endpoint_types,omitempty"`
	GroupImageCapabilities map[string]*imagedto.ImageModelCapabilities `json:"group_image_capabilities,omitempty"`
	SupportedEndpointTypes []constant.EndpointType                     `json:"supported_endpoint_types"`
	ImageCapabilities      *imagedto.ImageModelCapabilities            `json:"image_capabilities,omitempty"`
	BillingMode            string                                      `json:"billing_mode,omitempty"`
	BillingExpr            string                                      `json:"billing_expr,omitempty"`
	BillingUsageSchema     map[string]jsplugin.UsageFieldSchema        `json:"billing_usage_schema,omitempty"`
	BillingUsageExamples   []jsplugin.UsageExample                     `json:"billing_usage_examples,omitempty"`
	PricingVersion         string                                      `json:"pricing_version,omitempty"`
	// OrdinaryPricing reports that requests outside the plugin variants still
	// pay the model-level price: chat requests to a plugin relaying the model,
	// or requests to ordinary channels.
	OrdinaryPricing bool `json:"ordinary_pricing,omitempty"`
}

type PricingVendor struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
}

var (
	pricingMap           []Pricing
	vendorsList          []PricingVendor
	supportedEndpointMap map[string]common.EndpointInfo
	lastGetPricingTime   time.Time
	updatePricingLock    sync.Mutex

	// 缓存映射：模型名 -> 启用分组 / 计费类型
	modelEnableGroups     = make(map[string][]string)
	modelQuotaTypeMap     = make(map[string]int)
	modelEnableGroupsLock = sync.RWMutex{}
)

var (
	modelSupportEndpointTypes  = make(map[string][]constant.EndpointType)
	modelMetadataEndpointTypes = make(map[string][]constant.EndpointType)
	modelSupportEndpointsLock  = sync.RWMutex{}
)

func GetPricing() []Pricing {
	if time.Since(lastGetPricingTime) > time.Minute*1 || len(pricingMap) == 0 {
		updatePricingLock.Lock()
		defer updatePricingLock.Unlock()
		// Double check after acquiring the lock
		if time.Since(lastGetPricingTime) > time.Minute*1 || len(pricingMap) == 0 {
			modelSupportEndpointsLock.Lock()
			defer modelSupportEndpointsLock.Unlock()
			updatePricing()
		}
	}
	return pricingMap
}

func InvalidatePricingCache() {
	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()

	pricingMap = nil
	vendorsList = nil
	lastGetPricingTime = time.Time{}
}

// GetVendors 返回当前定价接口使用到的供应商信息
func GetVendors() []PricingVendor {
	GetPricing()
	return vendorsList
}

func GetModelSupportEndpointTypes(model string) []constant.EndpointType {
	if model == "" {
		return make([]constant.EndpointType, 0)
	}
	GetPricing()
	modelSupportEndpointsLock.RLock()
	defer modelSupportEndpointsLock.RUnlock()
	if endpoints, ok := modelSupportEndpointTypes[model]; ok {
		return endpoints
	}
	return make([]constant.EndpointType, 0)
}

// GetModelMetadataEndpointTypes returns endpoints explicitly declared by the
// persisted model metadata. The boolean reports whether metadata declared at
// least one usable endpoint, allowing user-facing catalogs to treat metadata as
// authoritative instead of merging channel defaults.
func GetModelMetadataEndpointTypes(model string) ([]constant.EndpointType, bool) {
	if model == "" || DB == nil {
		return nil, false
	}
	GetPricing()
	modelSupportEndpointsLock.RLock()
	defer modelSupportEndpointsLock.RUnlock()
	endpoints, ok := modelMetadataEndpointTypes[model]
	if !ok {
		return nil, false
	}
	return append([]constant.EndpointType(nil), endpoints...), true
}

func ModelDeclaresEndpoint(model string, endpoint constant.EndpointType) bool {
	endpoints, declared := GetModelMetadataEndpointTypes(model)
	return declared && slices.Contains(endpoints, endpoint)
}

// pricingUsesRelayRoutes reports a channel type whose endpoints come from its
// relay routes: Advanced Custom settings, or a task plugin's meta.relay.
func pricingUsesRelayRoutes(channelType int) bool {
	return channelType == constant.ChannelTypeAdvancedCustom || channelType == constant.ChannelTypeTaskPlugin
}

// loadPricingAdvancedCustomConfigs runs inside updatePricing while
// updatePricingLock is held, and nests channelSyncLock.RLock. This defines the
// global lock order updatePricingLock -> channelSyncLock: any code path holding
// channelSyncLock must release it before touching the pricing cache (see
// InitChannelCache / CacheUpdateChannel), otherwise it deadlocks.
// The returned configs are pointers shared with the channel cache; they are
// replaced wholesale on update and never mutated in place, so reading them after
// RUnlock is safe.
func loadPricingAdvancedCustomConfigs(enableAbilities []AbilityWithChannel) map[int]*dto.AdvancedCustomConfig {
	channelIDs := make([]int, 0)
	seen := make(map[int]struct{})
	for _, ability := range enableAbilities {
		if !pricingUsesRelayRoutes(ability.ChannelType) {
			continue
		}
		if _, exists := seen[ability.ChannelId]; exists {
			continue
		}
		seen[ability.ChannelId] = struct{}{}
		channelIDs = append(channelIDs, ability.ChannelId)
	}
	if len(channelIDs) == 0 {
		return nil
	}

	configs := make(map[int]*dto.AdvancedCustomConfig, len(channelIDs))
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		for _, channelID := range channelIDs {
			if config := channel2advancedCustomConfig[channelID]; config != nil {
				configs[channelID] = config
			} else if channel := channelsIDM[channelID]; channel != nil {
				if config := channelRelayConfig(channel); config != nil {
					configs[channelID] = config
				}
			}
		}
		return configs
	}

	for _, channelID := range channelIDs {
		channel, err := CacheGetChannel(channelID)
		if err != nil {
			common.SysLog(common.LogText("load advanced custom channel settings error: channel_id=%d, error=%v", channelID, err))
			continue
		}
		if config := channelRelayConfig(channel); config != nil {
			configs[channelID] = config
			continue
		}
		if channel.Type != constant.ChannelTypeAdvancedCustom {
			continue
		}
		if config := channel.GetOtherSettings().AdvancedCustom; config != nil {
			configs[channelID] = config
		}
	}
	return configs
}

func appendPricingEndpoint(endpoints []string, endpoint string) []string {
	if endpoint == "" || common.StringsContains(endpoints, endpoint) {
		return endpoints
	}
	return append(endpoints, endpoint)
}

func catalogMetadataEndpoints(raw string) map[string]common.EndpointInfo {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var value any
	if common.UnmarshalJsonStr(raw, &value) != nil {
		return nil
	}
	endpoints := make(map[string]common.EndpointInfo)
	switch declared := value.(type) {
	case []any:
		for _, item := range declared {
			if name, ok := item.(string); ok && name != "openai_video" {
				if info, found := common.GetDefaultEndpointInfo(constant.EndpointType(name)); found {
					endpoints[name] = info
				}
			}
		}
	case map[string]any:
		for name, item := range declared {
			if name == "openai_video" {
				continue
			}
			info := common.EndpointInfo{Method: "POST"}
			switch definition := item.(type) {
			case string:
				info.Path = definition
			case map[string]any:
				info.Path, _ = definition["path"].(string)
				if method, ok := definition["method"].(string); ok {
					info.Method = strings.ToUpper(method)
				}
			default:
				continue
			}
			if strings.HasPrefix(info.Path, "/") {
				endpoints[name] = info
			}
		}
	}
	return endpoints
}

func updatePricing() {
	//modelRatios := common.GetModelRatios()
	enableAbilities, err := GetAllEnableAbilityWithChannels()
	if err != nil {
		common.SysLog(common.LogText("GetAllEnableAbilityWithChannels error: %v", err))
		return
	}
	// 预加载模型元数据与供应商一次，避免循环查询
	var allMeta []Model
	_ = DB.Find(&allMeta).Error
	names := make([]string, 0, len(enableAbilities))
	for _, ability := range enableAbilities {
		names = append(names, ability.Model)
	}
	metaMap := resolveModelMetadata(allMeta, names)

	// 预加载供应商
	var vendors []Vendor
	_ = DB.Find(&vendors).Error
	vendorMap := make(map[int]*Vendor)
	for i := range vendors {
		vendorMap[vendors[i].Id] = &vendors[i]
	}

	// 初始化默认供应商映射
	initDefaultVendorMapping(metaMap, vendorMap, enableAbilities)

	// 构建对前端友好的供应商列表
	vendorsList = make([]PricingVendor, 0, len(vendorMap))
	for _, v := range vendorMap {
		vendorsList = append(vendorsList, PricingVendor{
			ID:          v.Id,
			Name:        v.Name,
			Description: v.Description,
			Icon:        v.Icon,
		})
	}

	modelGroupsMap := make(map[string]*types.Set[string])
	pluginGeneration := jsplugin.DefaultRegistry.Generation()
	// Models an ordinary channel serves: neither a task plugin channel nor a
	// legacy channel type that one of the model's plugins claims.
	ordinaryChannelModels := make(map[string]bool)

	for _, ability := range enableAbilities {
		groups, ok := modelGroupsMap[ability.Model]
		if !ok {
			groups = types.NewSet[string]()
			modelGroupsMap[ability.Model] = groups
		}
		groups.Add(ability.Group)
		if ability.ChannelType == constant.ChannelTypeTaskPlugin || ordinaryChannelModels[ability.Model] {
			continue
		}
		plugin, claimed := pluginGeneration.GetByChannelType(ability.ChannelType)
		if !claimed || !slices.Contains(pluginGeneration.PluginsByModel(ability.Model), plugin) {
			ordinaryChannelModels[ability.Model] = true
		}
	}

	//这里使用切片而不是Set，因为一个模型可能支持多个端点类型，并且第一个端点是优先使用端点
	modelSupportEndpointsStr := make(map[string][]string)
	modelAbilities := make(map[string][]AbilityWithChannel)
	type imageAggregate struct {
		value       imagecapability.Capability
		initialized bool
		unsupported bool
	}
	imageCapabilities := make(map[string]imageAggregate)
	groupEndpoints := make(map[string]map[string][]constant.EndpointType)
	groupPlugins := make(map[string]map[string][]string)
	groupImages := make(map[string]map[string]imageAggregate)
	advancedCustomConfigs := loadPricingAdvancedCustomConfigs(enableAbilities)
	metadataEndpointTypes := make(map[string][]constant.EndpointType)
	for modelName, meta := range metaMap {
		declared := catalogMetadataEndpoints(meta.Endpoints)
		if len(declared) == 0 {
			continue
		}
		keys := make([]string, 0, len(declared))
		for key := range declared {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		endpoints := make([]constant.EndpointType, 0, len(keys))
		for _, key := range keys {
			endpoints = append(endpoints, constant.EndpointType(key))
		}
		metadataEndpointTypes[modelName] = endpoints
	}

	// 先根据已有能力填充原生端点
	for _, ability := range enableAbilities {
		modelAbilities[ability.Model] = append(modelAbilities[ability.Model], ability)
		endpoints := modelSupportEndpointsStr[ability.Model]
		channelTypes := ResolveAbilityEndpointTypes(ability, pluginGeneration, advancedCustomConfigs[ability.ChannelId])
		if declared, ok := metadataEndpointTypes[ability.Model]; ok {
			channelTypes = append([]constant.EndpointType(nil), declared...)
		} else {
			channelTypes = slices.DeleteFunc(channelTypes, func(endpoint constant.EndpointType) bool {
				return endpoint == constant.EndpointTypeImageGeneration
			})
		}
		if groupPlugins[ability.Model] == nil {
			groupPlugins[ability.Model] = make(map[string][]string)
		}
		keys := groupPlugins[ability.Model][ability.Group]
		if keys == nil {
			keys = make([]string, 0)
		}
		if upstream, _, mappingErr := common.ResolveModelMapping(ability.Model, ability.ChannelModelMapping); mappingErr == nil && !(ability.ChannelType == constant.ChannelTypeAdvancedCustom && advancedCustomConfigs[ability.ChannelId] != nil) {
			_, eligible := resolvePricingPluginBindings(ability, upstream, pluginGeneration)
			for key, allowed := range eligible {
				if allowed && !slices.Contains(keys, key) {
					keys = append(keys, key)
				}
			}
		}
		slices.Sort(keys)
		groupPlugins[ability.Model][ability.Group] = keys
		if groupEndpoints[ability.Model] == nil {
			groupEndpoints[ability.Model] = make(map[string][]constant.EndpointType)
		}
		groupTypes := groupEndpoints[ability.Model][ability.Group]
		if groupTypes == nil {
			groupTypes = make([]constant.EndpointType, 0)
		}
		for _, endpoint := range channelTypes {
			if !slices.Contains(groupTypes, endpoint) {
				groupTypes = append(groupTypes, endpoint)
			}
		}
		slices.Sort(groupTypes)
		groupEndpoints[ability.Model][ability.Group] = groupTypes
		for _, channelType := range channelTypes {
			if !common.StringsContains(endpoints, string(channelType)) {
				endpoints = append(endpoints, string(channelType))
			}
		}
		modelSupportEndpointsStr[ability.Model] = endpoints
		if slices.Contains(channelTypes, constant.EndpointTypeImageGeneration) {
			if groupImages[ability.Model] == nil {
				groupImages[ability.Model] = make(map[string]imageAggregate)
			}
			groupImage := groupImages[ability.Model][ability.Group]
			current := imageCapabilities[ability.Model]
			capability := ImageCapabilityForChannelType(ability.ChannelType)
			if current.initialized {
				current.value = imagecapability.Intersect(current.value, capability)
			} else {
				current.value = capability
				current.initialized = true
			}
			if groupImage.initialized {
				groupImage.value = imagecapability.Intersect(groupImage.value, capability)
			} else {
				groupImage.value = capability
				groupImage.initialized = true
			}
			groupImages[ability.Model][ability.Group] = groupImage
			imageCapabilities[ability.Model] = current
		}
	}

	// Explicit custom endpoints remain available; standard routes cannot be
	// invented by metadata for a model served solely by task-plugin channels.
	for modelName, meta := range metaMap {
		endpoints := modelSupportEndpointsStr[modelName]
		declared := catalogMetadataEndpoints(meta.Endpoints)
		keys := make([]string, 0, len(declared))
		for key := range declared {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if pluginOnlyCatalogModel(modelAbilities[modelName]) {
				if _, standard := common.GetDefaultEndpointInfo(constant.EndpointType(key)); standard && !common.StringsContains(endpoints, key) {
					continue
				}
			}
			endpoints = appendPricingEndpoint(endpoints, key)
		}
		if len(endpoints) > 0 {
			modelSupportEndpointsStr[modelName] = endpoints
		}
	}

	modelSupportEndpointTypes = make(map[string][]constant.EndpointType)
	for model, endpoints := range modelSupportEndpointsStr {
		supportedEndpoints := make([]constant.EndpointType, 0)
		for _, endpointStr := range endpoints {
			endpointType := constant.EndpointType(endpointStr)
			supportedEndpoints = append(supportedEndpoints, endpointType)
		}
		modelSupportEndpointTypes[model] = supportedEndpoints
	}
	modelMetadataEndpointTypes = metadataEndpointTypes

	// 构建全局 supportedEndpointMap（默认 + 自定义覆盖）
	supportedEndpointMap = make(map[string]common.EndpointInfo)
	// 1. 默认端点
	for _, endpoints := range modelSupportEndpointTypes {
		for _, et := range endpoints {
			if info, ok := common.GetDefaultEndpointInfo(et); ok {
				if _, exists := supportedEndpointMap[string(et)]; !exists {
					supportedEndpointMap[string(et)] = info
				}
			}
		}
	}
	// 2. Retain metadata paths only for non-standard endpoint names. Built-in
	// routes are host-owned and their URL must not vary by metadata record.
	metaNames := make([]string, 0, len(metaMap))
	for name := range metaMap {
		metaNames = append(metaNames, name)
	}
	sort.Strings(metaNames)
	for _, name := range metaNames {
		for key, info := range catalogMetadataEndpoints(metaMap[name].Endpoints) {
			if _, standard := common.GetDefaultEndpointInfo(constant.EndpointType(key)); !standard && common.StringsContains(modelSupportEndpointsStr[name], key) {
				supportedEndpointMap[key] = info
			}
		}
	}

	pricingMap = make([]Pricing, 0)
	for model, groups := range modelGroupsMap {
		pricing := Pricing{
			ModelName:              model,
			EnableGroup:            groups.Items(),
			SupportedEndpointTypes: modelSupportEndpointTypes[model],
			GroupEndpointTypes:     groupEndpoints[model],
			GroupPluginKeys:        groupPlugins[model],
			GroupImageCapabilities: make(map[string]*imagedto.ImageModelCapabilities),
		}
		for groupName, aggregate := range groupImages[model] {
			if aggregate.initialized && !aggregate.unsupported {
				converted := imagedto.ImageCapabilitiesFrom(aggregate.value)
				pricing.GroupImageCapabilities[groupName] = &converted
			}
		}
		if capability := imageCapabilities[model]; capability.initialized && !capability.unsupported {
			converted := imagedto.ImageCapabilitiesFrom(capability.value)
			pricing.ImageCapabilities = &converted
		}

		// 补充模型元数据（描述、标签、供应商、状态）
		if meta, ok := metaMap[model]; ok {
			// 若模型被禁用(status!=1)，则直接跳过，不返回给前端
			if meta.Status != 1 {
				continue
			}
			pricing.Description = meta.Description
			pricing.Icon = meta.Icon
			pricing.Tags = meta.Tags
			pricing.VendorID = meta.VendorID
		}
		modelPrice, findPrice := ratio_setting.GetModelPrice(model, false)
		if findPrice {
			pricing.ModelPrice = modelPrice
			pricing.QuotaType = 1
		} else {
			modelRatio, _, _ := ratio_setting.GetModelRatio(model)
			pricing.ModelRatio = modelRatio
			pricing.CompletionRatio = ratio_setting.GetCompletionRatio(model)
			pricing.QuotaType = 0
		}
		if cacheRatio, ok := ratio_setting.GetCacheRatio(model); ok {
			pricing.CacheRatio = &cacheRatio
		}
		if createCacheRatio, ok := ratio_setting.GetCreateCacheRatio(model); ok {
			pricing.CreateCacheRatio = &createCacheRatio
		}
		if imageRatio, ok := ratio_setting.GetImageRatio(model); ok {
			pricing.ImageRatio = &imageRatio
		}
		if ratio_setting.ContainsAudioRatio(model) {
			audioRatio := ratio_setting.GetAudioRatio(model)
			pricing.AudioRatio = &audioRatio
		}
		if ratio_setting.ContainsAudioCompletionRatio(model) {
			audioCompletionRatio := ratio_setting.GetAudioCompletionRatio(model)
			pricing.AudioCompletionRatio = &audioCompletionRatio
		}
		if billingMode := billing_setting.GetBillingMode(model); billingMode == "tiered_expr" {
			if expr, ok := billing_setting.GetBillingExpr(model); ok && strings.TrimSpace(expr) != "" {
				pricing.BillingMode = billingMode
				pricing.BillingExpr = expr
			}
		} else if target, resolved := ResolveTaskModelAlias(pluginGeneration, model); resolved && target.Declared != "" {
			if tailMode := billing_setting.GetBillingMode(target.Declared); tailMode == "tiered_expr" {
				if expr, ok := billing_setting.GetBillingExpr(target.Declared); ok && strings.TrimSpace(expr) != "" {
					pricing.BillingMode = tailMode
					pricing.BillingExpr = expr
				}
			}
		}
		usageModel := model
		plugin, ok := pluginGeneration.GetByModel(model)
		if !ok {
			if target, resolved := ResolveTaskModelAlias(pluginGeneration, model); resolved {
				plugin, ok = pluginGeneration.Get(target.PluginKey)
				usageModel = target.Declared
			}
		}
		if ok && plugin != nil {
			usageSchema, usageExamples := plugin.Meta.UsageForModel(usageModel)
			pricing.BillingUsageSchema = jsplugin.CloneUsageSchema(usageSchema)
			pricing.BillingUsageExamples = jsplugin.CloneUsageExamples(usageExamples)
		}
		providers := pluginGeneration.PluginsByModel(model)
		hasProviderOverride, relayed := false, false
		for _, provider := range providers {
			if _, configured := billing_setting.GetPluginBillingExpr(provider.Meta.Key, model); configured {
				hasProviderOverride = true
			}
			relayed = relayed || provider.Meta.RelaysModel(model)
		}
		// The model-level price may be only a chat price: a provider also
		// relays the model, or the expression reads chat request quantities.
		// List the providers so those whose tasks it cannot price show as
		// unconfigured.
		tieredModel := pricing.BillingMode == billing_setting.BillingModeTieredExpr
		if hasProviderOverride || relayed || tieredModel && (len(providers) >= 2 || billingexpr.UsesChatQuantities(pricing.BillingExpr)) {
			_, defaultPrice := ratio_setting.GetDefaultModelPriceMap()[model]
			perCall := pricing.QuotaType == 1 || defaultPrice
			for _, provider := range providers {
				schema, examples := provider.Meta.UsageForModel(model)
				if schema == nil {
					schema = map[string]jsplugin.UsageFieldSchema{}
				}
				expression, overridden, hasExpression := billing_setting.ResolveTaskBillingExpr(provider.Meta.Key, model, "")
				relaysModel := provider.Meta.RelaysModel(model)
				mode := billing_setting.BillingModeRatio
				// Inherited ratio pricing prices the provider's tasks per call
				// only; with just a token ratio its tasks have no price.
				if hasExpression || billing_setting.GetBillingMode(model) == billing_setting.BillingModeTieredExpr || relaysModel && !perCall {
					mode = billing_setting.BillingModeTieredExpr
				}
				if mode == billing_setting.BillingModeTieredExpr && (!billing_setting.TaskExprCompatible(expression, schema) || !billing_setting.TaskPriceExpr(expression, overridden, relaysModel)) {
					expression = ""
				}
				pricing.BillingPluginVariants = append(pricing.BillingPluginVariants, PricingPluginVariant{
					PluginKey: provider.Meta.Key, PluginName: provider.Meta.Name, Icon: provider.Meta.Icon,
					BillingExpr: expression, BillingMode: mode,
					BillingUsageSchema: jsplugin.CloneUsageSchema(schema), BillingUsageExamples: jsplugin.CloneUsageExamples(examples),
				})
			}
			// An expression reading u() prices tasks only; ordinary requests
			// reject it.
			pricing.OrdinaryPricing = (relayed || ordinaryChannelModels[model]) && !(tieredModel && billingexpr.UsedVars(pricing.BillingExpr)["u"])
		}
		pricingMap = append(pricingMap, pricing)
	}

	// 防止大更新后数据不通用
	if len(pricingMap) > 0 {
		pricingMap[0].PricingVersion = "5a90f2b86c08bd983a9a2e6d66c255f4eaef9c4bc934386d2b6ae84ef0ff1f1f"
	}

	// 刷新缓存映射，供高并发快速查询
	modelEnableGroupsLock.Lock()
	modelEnableGroups = make(map[string][]string)
	modelQuotaTypeMap = make(map[string]int)
	for _, p := range pricingMap {
		modelEnableGroups[p.ModelName] = p.EnableGroup
		modelQuotaTypeMap[p.ModelName] = p.QuotaType
	}
	modelEnableGroupsLock.Unlock()

	lastGetPricingTime = time.Now()
}

// imageCapabilityForChannelType keeps provider-specific image behavior tied
// to the configured channel protocol. Model names and aliases remain data in
// the model/channel catalog rather than source-level capability switches.
func ImageCapabilityForChannelType(channelType int) imagecapability.Capability {
	if channelType == constant.ChannelTypeGemini {
		return imagecapability.GeminiNativeCapability()
	}
	if channelType == constant.ChannelTypeXai {
		return imagecapability.XAINativeCapability()
	}
	return imagecapability.DefaultCapability()
}

// GetSupportedEndpointMap 返回全局端点到路径的映射
func GetSupportedEndpointMap() map[string]common.EndpointInfo {
	GetPricing()
	return supportedEndpointMap
}
