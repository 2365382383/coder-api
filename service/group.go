package service

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

type GroupTypeOption struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Icon  string `json:"icon,omitempty"`
	Order int    `json:"order"`
}

type UserGroupOption struct {
	Ratio any              `json:"ratio"`
	Desc  string           `json:"desc"`
	Type  *GroupTypeOption `json:"type,omitempty"`
}

type UserGroupOptions struct {
	UsableGroups map[string]string
	Groups       map[string]UserGroupOption
	Types        []GroupTypeOption
}

type PricingGroupTypeOption struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Icon       string `json:"icon,omitempty"`
	Order      int    `json:"order"`
	GroupCount int    `json:"group_count"`
	ModelCount int    `json:"model_count"`
}

type PricingGroupOption struct {
	Name        string  `json:"name"`
	TypeID      *string `json:"type_id"`
	TypeName    string  `json:"type_name,omitempty"`
	TypeIcon    string  `json:"type_icon,omitempty"`
	TypeOrder   int     `json:"type_order,omitempty"`
	Ratio       float64 `json:"ratio"`
	Description string  `json:"description"`
	ModelCount  int     `json:"model_count"`
}

type PricingGroupCatalog struct {
	Version string                   `json:"group_catalog_version"`
	Types   []PricingGroupTypeOption `json:"group_types"`
	Groups  []PricingGroupOption     `json:"groups"`
}

func GetUserGroupOptions(userGroup string) UserGroupOptions {
	usableGroups := GetUserUsableGroups(userGroup)
	groupRatios := ratio_setting.GetGroupRatioCopy()
	groupOverrides := ratio_setting.GetGroupGroupRatioCopy()[userGroup]
	groupTypes, groupTypeMap := ratio_setting.GetGroupTypeOptionsCopy()

	types := make([]GroupTypeOption, 0, len(groupTypes))
	for typeID, typeInfo := range groupTypes {
		if typeID == ratio_setting.AutoGroupTypeID {
			continue
		}
		types = append(types, GroupTypeOption{
			ID:    typeID,
			Name:  typeInfo.Name,
			Icon:  typeInfo.Icon,
			Order: typeInfo.Order,
		})
	}
	slices.SortFunc(types, func(a, b GroupTypeOption) int {
		if a.Order != b.Order {
			return cmp.Compare(a.Order, b.Order)
		}
		if a.Name != b.Name {
			return cmp.Compare(a.Name, b.Name)
		}
		return cmp.Compare(a.ID, b.ID)
	})

	groups := make(map[string]UserGroupOption, len(groupRatios)+1)
	for groupName, baseRatio := range groupRatios {
		desc, ok := usableGroups[groupName]
		if !ok {
			continue
		}
		ratio := baseRatio
		if specialRatio, configured := groupOverrides[groupName]; configured {
			ratio = specialRatio
		}
		option := UserGroupOption{
			Ratio: ratio,
			Desc:  desc,
		}
		if typeID, ok := groupTypeMap[groupName]; ok && typeID != "" && typeID != ratio_setting.AutoGroupTypeID {
			if typeInfo, ok := groupTypes[typeID]; ok {
				option.Type = &GroupTypeOption{
					ID:    typeID,
					Name:  typeInfo.Name,
					Icon:  typeInfo.Icon,
					Order: typeInfo.Order,
				}
			}
		}
		groups[groupName] = option
	}
	if _, ok := usableGroups["auto"]; ok {
		groups["auto"] = UserGroupOption{
			Ratio: "auto",
			Desc:  setting.GetUsableGroupDescription("auto"),
		}
	}

	return UserGroupOptions{UsableGroups: usableGroups, Groups: groups, Types: types}
}

// BuildPricingGroupCatalog projects the visible groups and model memberships;
// category names and IDs always come from the configured group types.
func BuildPricingGroupCatalog(pricing []model.Pricing, options UserGroupOptions) (PricingGroupCatalog, map[string][]string, error) {
	typeByID := make(map[string]GroupTypeOption, len(options.Types))
	for _, groupType := range options.Types {
		if groupType.ID != "" && groupType.ID != ratio_setting.AutoGroupTypeID {
			typeByID[groupType.ID] = groupType
		}
	}
	groups := make([]PricingGroupOption, 0, len(options.Groups))
	groupByName := make(map[string]PricingGroupOption, len(options.Groups))
	for name, option := range options.Groups {
		if name == "" || name == "auto" {
			continue
		}
		ratio, ok := option.Ratio.(float64)
		if !ok || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 {
			return PricingGroupCatalog{}, nil, common.NewMessage("Group price is unavailable: {{group}}", map[string]any{"group": name})
		}
		group := PricingGroupOption{Name: name, Ratio: ratio, Description: option.Desc}
		if option.Type != nil {
			if groupType, found := typeByID[option.Type.ID]; found {
				typeID := groupType.ID
				group.TypeID = &typeID
				group.TypeName = groupType.Name
				group.TypeIcon = groupType.Icon
				group.TypeOrder = groupType.Order
			}
		}
		groups = append(groups, group)
		groupByName[name] = group
	}
	slices.SortFunc(groups, func(a, b PricingGroupOption) int {
		if (a.TypeID == nil) != (b.TypeID == nil) {
			if a.TypeID == nil {
				return 1
			}
			return -1
		}
		if a.TypeOrder != b.TypeOrder {
			return cmp.Compare(a.TypeOrder, b.TypeOrder)
		}
		if a.TypeName != b.TypeName {
			return cmp.Compare(a.TypeName, b.TypeName)
		}
		return cmp.Compare(a.Name, b.Name)
	})

	groupModels := make(map[string]map[string]struct{}, len(groups))
	typeModels := make(map[string]map[string]struct{}, len(typeByID))
	for _, group := range groups {
		groupModels[group.Name] = make(map[string]struct{})
	}
	for typeID := range typeByID {
		typeModels[typeID] = make(map[string]struct{})
	}
	modelTypeIDs := make(map[string][]string, len(pricing))
	versionModels := slices.Clone(pricing)
	for index, item := range pricing {
		names := item.EnableGroup
		if slices.Contains(names, "all") {
			names = make([]string, 0, len(groups))
			for _, group := range groups {
				names = append(names, group.Name)
			}
		}
		modelTypes := make(map[string]struct{})
		visibleNames := make([]string, 0, len(names))
		for _, name := range names {
			group, visible := groupByName[name]
			if !visible {
				continue
			}
			visibleNames = append(visibleNames, name)
			groupModels[name][item.ModelName] = struct{}{}
			if group.TypeID != nil {
				modelTypes[*group.TypeID] = struct{}{}
				typeModels[*group.TypeID][item.ModelName] = struct{}{}
			}
		}
		ids := make([]string, 0, len(modelTypes))
		for id := range modelTypes {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		modelTypeIDs[item.ModelName] = ids
		slices.Sort(visibleNames)
		versionModels[index].EnableGroup = slices.Compact(visibleNames)
		versionModels[index].GroupTypeIDs = ids
		versionModels[index].PricingVersion = ""
	}
	for index := range groups {
		groups[index].ModelCount = len(groupModels[groups[index].Name])
	}

	typeGroupCounts := make(map[string]int)
	for _, group := range groups {
		if group.TypeID != nil {
			typeGroupCounts[*group.TypeID]++
		}
	}
	types := make([]PricingGroupTypeOption, 0, len(typeGroupCounts))
	for id, count := range typeGroupCounts {
		groupType := typeByID[id]
		types = append(types, PricingGroupTypeOption{
			ID: id, Name: groupType.Name, Icon: groupType.Icon, Order: groupType.Order,
			GroupCount: count, ModelCount: len(typeModels[id]),
		})
	}
	slices.SortFunc(types, func(a, b PricingGroupTypeOption) int {
		if a.Order != b.Order {
			return cmp.Compare(a.Order, b.Order)
		}
		if a.Name != b.Name {
			return cmp.Compare(a.Name, b.Name)
		}
		return cmp.Compare(a.ID, b.ID)
	})
	slices.SortFunc(versionModels, func(a, b model.Pricing) int {
		return cmp.Compare(a.ModelName, b.ModelName)
	})
	versionBytes, err := common.Marshal(struct {
		Types  []PricingGroupTypeOption `json:"types"`
		Groups []PricingGroupOption     `json:"groups"`
		Models []model.Pricing          `json:"models"`
	}{types, groups, versionModels})
	if err != nil {
		return PricingGroupCatalog{}, nil, err
	}
	digest := sha256.Sum256(versionBytes)
	return PricingGroupCatalog{Version: hex.EncodeToString(digest[:]), Types: types, Groups: groups}, modelTypeIDs, nil
}

func GetUserUsableGroups(userGroup string) map[string]string {
	groupsCopy := setting.GetUserUsableGroupsCopy()
	if userGroup != "" {
		specialSettings, b := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		if b {
			// 处理特殊可用分组
			for specialGroup, desc := range specialSettings {
				if after, ok := strings.CutPrefix(specialGroup, "-:"); ok {
					// 移除分组
					groupToRemove := after
					delete(groupsCopy, groupToRemove)
				} else if after, ok := strings.CutPrefix(specialGroup, "+:"); ok {
					// 添加分组
					groupToAdd := after
					groupsCopy[groupToAdd] = desc
				} else {
					// 直接添加分组
					groupsCopy[specialGroup] = desc
				}
			}
		}
		// 如果userGroup不在UserUsableGroups中，返回UserUsableGroups + userGroup。
		// 描述是英文源文本，由 web 控制台翻译；管理员配置的描述原样显示。
		if _, ok := groupsCopy[userGroup]; !ok {
			groupsCopy[userGroup] = "User group"
		}
	}
	return groupsCopy
}

func GroupInUserUsableGroups(userGroup, groupName string) bool {
	_, ok := GetUserUsableGroups(userGroup)[groupName]
	return ok
}

func IsUserSelectableGroup(userGroup, groupName string) bool {
	if groupName == "" || groupName == "auto" {
		return false
	}
	return GroupInUserUsableGroups(userGroup, groupName) && ratio_setting.ContainsGroupRatio(groupName)
}

// GetUserAutoGroup 根据用户分组获取自动分组设置
func GetUserAutoGroup(userGroup string) []string {
	autoGroups := make([]string, 0)
	seen := make(map[string]struct{})
	for _, group := range setting.GetAutoGroups() {
		if !IsUserSelectableGroup(userGroup, group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		autoGroups = append(autoGroups, group)
	}
	return autoGroups
}

// FilterUserTokenAutoGroups applies current permissions before the current
// per-token limit. It intentionally does not fall back to the global Auto list.
func FilterUserTokenAutoGroups(userGroup string, groups []string) []string {
	maxCount := setting.GetMaxTokenAutoGroups()
	filtered := make([]string, 0, min(len(groups), maxCount))
	seen := make(map[string]struct{})
	for _, group := range groups {
		if !IsUserSelectableGroup(userGroup, group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		filtered = append(filtered, group)
		if len(filtered) == maxCount {
			break
		}
	}
	return filtered
}

// GetRequestAutoGroups resolves the ordered Auto groups for the current token.
// The absence of the context value means that the token inherits the complete
// global Auto list; a present (even empty) value is an explicit token snapshot.
func GetRequestAutoGroups(c *gin.Context, userGroup string) []string {
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenAutoGroups)
	if !ok {
		return GetUserAutoGroup(userGroup)
	}
	groups, ok := value.([]string)
	if !ok {
		return []string{}
	}
	return FilterUserTokenAutoGroups(userGroup, groups)
}

// GetGroupsEnabledModels 按 groups 顺序获取各分组启用的模型并去重
func GetGroupsEnabledModels(groups []string) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)
	for _, group := range groups {
		for _, modelName := range model.GetGroupEnabledModels(group) {
			if _, ok := seen[modelName]; !ok {
				seen[modelName] = struct{}{}
				models = append(models, modelName)
			}
		}
	}
	return models
}

// GetUserGroupRatio 获取用户使用某个分组的倍率
// userGroup 用户分组
// group 需要获取倍率的分组
func GetUserGroupRatio(userGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}
