package service

import (
	"cmp"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	imagedto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/imagecapability"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

type CanvasModelOption struct {
	Name              string                           `json:"name"`
	Group             string                           `json:"group"`
	Capability        string                           `json:"capability"`
	ImageCapabilities *imagedto.ImageModelCapabilities `json:"image_capabilities,omitempty"`
	VideoAdapter      string                           `json:"video_adapter,omitempty"`
}

// DefaultImageCapabilities is the single image workbench configuration used by
// Playground and the embedded canvas.
func DefaultImageCapabilities() *imagedto.ImageModelCapabilities {
	capabilities := imagedto.ImageCapabilitiesFrom(imagecapability.DefaultCapability())
	return &capabilities
}

func canvasVideoAdapter(ability model.AbilityWithChannel, generation *jsplugin.RoutingGeneration) string {
	if ability.ChannelType == constant.ChannelTypeDoubaoVideo || ability.ChannelType == constant.ChannelTypeVolcEngine {
		return "doubao"
	}
	if ability.ChannelType == constant.ChannelTypeSora {
		return "sora"
	}
	settings := dto.ChannelSettings{}
	if ability.ChannelSetting != "" && common.UnmarshalJsonStr(ability.ChannelSetting, &settings) != nil {
		return "unsupported"
	}
	upstream, _, err := common.ResolveModelMapping(ability.Model, ability.ChannelModelMapping)
	if err != nil {
		return "unsupported"
	}
	declared, ok := generation.CanonicalModel(ability.Model)
	if !ok {
		declared, _ = generation.CanonicalModel(upstream)
	}
	for _, candidate := range generation.LookupEndpointCandidates("POST", "/v1/videos", declared) {
		if candidate.Plugin == nil {
			continue
		}
		plugin := candidate.Plugin.Meta
		eligible := ability.ChannelType == constant.ChannelTypeTaskPlugin && settings.TaskPluginKey == plugin.Key ||
			ability.ChannelType == constant.ChannelTypeNewAPI && slices.Contains(settings.TaskPluginBindings(), plugin.Key) ||
			ability.ChannelType != constant.ChannelTypeTaskPlugin && ability.ChannelType != constant.ChannelTypeNewAPI && slices.Contains(plugin.ChannelTypes, ability.ChannelType)
		if eligible && (plugin.Key == "sora" || plugin.Key == "doubao") {
			return plugin.Key
		}
	}
	return "openai"
}

// Catalog capability follows published endpoint metadata, never a model-name
// heuristic or a price. Unknown capabilities stay unavailable.
func CanvasModelCatalog(userGroup string) ([]CanvasModelOption, error) {
	options := []CanvasModelOption{}
	groups := GetUserUsableGroups(userGroup)
	groupNames := make([]string, 0, len(groups))
	for group := range groups {
		groupNames = append(groupNames, group)
	}
	slices.Sort(groupNames)
	queryGroups := []string{}
	for _, group := range groupNames {
		if group == "auto" {
			queryGroups = append(queryGroups, GetUserAutoGroup(userGroup)...)
		} else {
			queryGroups = append(queryGroups, group)
		}
	}
	abilities, err := model.GetEnabledAbilitiesWithChannelsByGroups(queryGroups)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(abilities))
	for _, ability := range abilities {
		names = append(names, ability.Model)
	}
	metadata, err := model.CanvasMetadataEndpoints(names)
	if err != nil {
		return nil, err
	}
	generation := jsplugin.DefaultRegistry.Generation()
	for _, group := range groupNames {
		queryGroups := []string{group}
		if group == "auto" {
			queryGroups = GetUserAutoGroup(userGroup)
		}
		seen := map[string]bool{}
		videoAdapters := map[string]string{}
		for _, ability := range abilities {
			if !slices.Contains(queryGroups, ability.Group) {
				continue
			}
			endpoints := metadata[ability.Model]
			if len(endpoints) == 0 {
				continue
			}
			for _, endpoint := range endpoints {
				capability := ""
				switch endpoint.Path {
				case "/v1/responses":
					capability = "text"
				case "/v1/images/generations", "/v1/images/edits":
					capability = "image"
				case "/v1/videos":
					capability = "video"
					adapter := canvasVideoAdapter(ability, generation)
					if current, ok := videoAdapters[ability.Model]; ok && current != adapter {
						adapter = "unsupported"
					}
					videoAdapters[ability.Model] = adapter
				case "/v1/audio/speech":
					capability = "audio"
				}
				key := ability.Model + "\x00" + capability
				if capability != "" && !seen[key] {
					options = append(options, CanvasModelOption{Name: ability.Model, Group: group, Capability: capability})
					seen[key] = true
				}
			}
		}
		for i := range options {
			if options[i].Group == group && options[i].Capability == "video" {
				options[i].VideoAdapter = videoAdapters[options[i].Name]
			}
		}
	}
	slices.SortFunc(options, func(a, b CanvasModelOption) int {
		if a.Group != b.Group {
			return cmp.Compare(a.Group, b.Group)
		}
		if a.Capability != b.Capability {
			return cmp.Compare(a.Capability, b.Capability)
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return options, nil
}
