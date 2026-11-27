package service

import (
	"sort"

	"github.com/QuantumNous/new-api/dto"
)

// GetUserImageModelGroups builds Playground image options from the same
// group-type-driven model catalog used by the embedded canvas.
func GetUserImageModelGroups(userGroup string) ([]dto.UserImageGroupOption, error) {
	usableGroups := GetUserUsableGroups(userGroup)
	catalog, err := CanvasModelCatalog(userGroup)
	if err != nil {
		return nil, err
	}

	groupNames := make([]string, 0, len(usableGroups))
	for group := range usableGroups {
		groupNames = append(groupNames, group)
	}
	sort.Strings(groupNames)

	options := make([]dto.UserImageGroupOption, 0, len(groupNames))
	for _, group := range groupNames {
		ratio := any(GetUserGroupRatio(userGroup, group))
		if group == "auto" {
			ratio = "自动"
		}
		models := make([]dto.UserImageModelOption, 0)
		seen := map[string]bool{}
		for _, item := range catalog {
			if item.Group != group || item.Capability != "image" || seen[item.Name] {
				continue
			}
			seen[item.Name] = true
			capabilities := item.ImageCapabilities
			if capabilities == nil {
				capabilities = DefaultImageCapabilities()
			}
			models = append(models, dto.UserImageModelOption{
				Label:        item.Name,
				Value:        item.Name,
				Capabilities: *capabilities,
			})
		}
		if len(models) == 0 {
			continue
		}
		options = append(options, dto.UserImageGroupOption{
			Label:  group,
			Value:  group,
			Ratio:  ratio,
			Desc:   usableGroups[group],
			Models: models,
		})
	}
	return options, nil
}
