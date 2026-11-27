package imagecapability

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	maxImageDimension = 3840
	maxImagePixels    = 3840 * 2160
)

var (
	defaultAspectRatios = []string{
		"auto", "1:1", "1:4", "1:8", "2:3", "3:2", "3:4", "4:1", "4:3", "4:5",
		"5:4", "8:1", "9:16", "16:9", "19.5:9", "9:19.5", "20:9", "9:20", "21:9",
	}
	geminiNativeAspectRatios = []string{
		"1:1", "1:4", "1:8", "2:3", "3:2", "3:4", "4:1", "4:3", "4:5",
		"5:4", "8:1", "9:16", "16:9", "21:9",
	}
	xaiAspectRatios = []string{
		"auto", "1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3",
		"2:1", "1:2", "19.5:9", "9:19.5", "20:9", "9:20",
	}
	defaultResolutions = []string{"1K", "2K", "4K"}
)

// DefaultCapability is the single image workbench contract used by Playground
// and the embedded canvas. It describes UI options, not a claim that every
// upstream model accepts every combination.
func DefaultCapability() Capability {
	return Capability{
		Provider:                  ProviderOpenAI,
		SizeMode:                  SizeModeAspectRatioResolution,
		AspectRatios:              append([]string(nil), defaultAspectRatios...),
		Resolutions:               append([]string(nil), defaultResolutions...),
		Qualities:                 []string{"auto", "low", "medium", "high"},
		OutputFormats:             []string{"png", "jpeg", "webp"},
		DefaultAspectRatio:        "1:1",
		DefaultResolution:         "1K",
		DefaultQuality:            "auto",
		DefaultOutputFormat:       "png",
		SupportsEditing:           true,
		SupportsModeration:        true,
		SupportsOutputCompression: true,
		MaxImages:                 10,
	}
}

// GeminiNativeCapability describes the shared image contract after a request
// is routed through a native Gemini image channel. It is selected from the
// channel protocol, never from a model name.
func GeminiNativeCapability() Capability {
	return Capability{
		Provider:           ProviderGemini,
		SizeMode:           SizeModeAspectRatioResolution,
		AspectRatios:       append([]string(nil), geminiNativeAspectRatios...),
		Resolutions:        append([]string(nil), defaultResolutions...),
		DefaultAspectRatio: "1:1",
		DefaultResolution:  "1K",
		MaxImages:          4,
	}
}

// XAINativeCapability describes the xAI image request contract selected by
// the xAI channel protocol.
func XAINativeCapability() Capability {
	return Capability{
		Provider:           ProviderXAI,
		SizeMode:           SizeModeAspectRatioResolution,
		AspectRatios:       append([]string(nil), xaiAspectRatios...),
		Resolutions:        []string{"1K", "2K"},
		DefaultAspectRatio: "auto",
		DefaultResolution:  "1K",
		MaxImages:          4,
	}
}

// DimensionsFor converts the shared ratio/resolution presets into the size
// format expected by OpenAI-compatible image endpoints. The result remains
// within gpt-image-2's documented dimension and pixel limits.
func DimensionsFor(aspectRatio, resolution string) (string, error) {
	aspectRatio = strings.ToLower(strings.TrimSpace(aspectRatio))
	resolution = strings.ToUpper(strings.TrimSpace(resolution))
	if aspectRatio == "auto" {
		return "auto", nil
	}
	if aspectRatio == "" {
		aspectRatio = "1:1"
	}
	if resolution == "" {
		resolution = "1K"
	}
	longEdge, ok := map[string]float64{"1K": 1024, "2K": 2048, "4K": 3840}[resolution]
	if !ok {
		return "", fmt.Errorf("unsupported image resolution %q", resolution)
	}
	parts := strings.Split(aspectRatio, ":")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid image aspect ratio %q", aspectRatio)
	}
	widthRatio, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || widthRatio <= 0 {
		return "", fmt.Errorf("invalid image aspect ratio %q", aspectRatio)
	}
	heightRatio, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || heightRatio <= 0 {
		return "", fmt.Errorf("invalid image aspect ratio %q", aspectRatio)
	}
	ratio := widthRatio / heightRatio
	var width, height float64
	if ratio >= 1 {
		width, height = longEdge, longEdge/ratio
	} else {
		width, height = longEdge*ratio, longEdge
	}
	scale := math.Min(1, math.Sqrt(maxImagePixels/(width*height)))
	scale = math.Min(scale, maxImageDimension/math.Max(width, height))
	width = quantizeDimension(width * scale)
	height = quantizeDimension(height * scale)
	if width <= 0 || height <= 0 || width > maxImageDimension || height > maxImageDimension || width*height > maxImagePixels {
		return "", errors.New("image dimensions exceed OpenAI limits")
	}
	return fmt.Sprintf("%.0fx%.0f", width, height), nil
}

func quantizeDimension(value float64) float64 {
	quantized := math.Floor(value/16) * 16
	if quantized < 16 {
		return 16
	}
	return quantized
}

// Intersect returns the conservative capability set shared by candidate
// channels. The static default normally intersects with itself.
func Intersect(left, right Capability) Capability {
	provider := left.Provider
	if provider != right.Provider {
		provider = ProviderMixed
	}
	sizeMode := left.SizeMode
	if sizeMode != right.SizeMode {
		sizeMode = SizeModeNone
	}
	result := Capability{
		Provider:                  provider,
		SizeMode:                  sizeMode,
		Sizes:                     intersectValues(left.Sizes, right.Sizes),
		AspectRatios:              intersectValues(left.AspectRatios, right.AspectRatios),
		Resolutions:               intersectValues(left.Resolutions, right.Resolutions),
		Qualities:                 intersectValues(left.Qualities, right.Qualities),
		OutputFormats:             intersectValues(left.OutputFormats, right.OutputFormats),
		SupportsEditing:           left.SupportsEditing && right.SupportsEditing,
		SupportsModeration:        left.SupportsModeration && right.SupportsModeration,
		SupportsOutputCompression: left.SupportsOutputCompression && right.SupportsOutputCompression,
		MaxImages:                 minimumPositive(left.MaxImages, right.MaxImages),
	}
	if sizeMode == SizeModeNone {
		result.Sizes = nil
		result.AspectRatios = nil
		result.Resolutions = nil
	}
	result.DefaultSize = intersectedDefault(left.DefaultSize, right.DefaultSize, result.Sizes)
	result.DefaultAspectRatio = intersectedDefault(left.DefaultAspectRatio, right.DefaultAspectRatio, result.AspectRatios)
	result.DefaultResolution = intersectedDefault(left.DefaultResolution, right.DefaultResolution, result.Resolutions)
	result.DefaultQuality = intersectedDefault(left.DefaultQuality, right.DefaultQuality, result.Qualities)
	result.DefaultOutputFormat = intersectedDefault(left.DefaultOutputFormat, right.DefaultOutputFormat, result.OutputFormats)
	return result
}

func intersectValues(left, right []string) []string {
	values := make([]string, 0)
	for _, value := range left {
		if containsFold(right, value) {
			values = append(values, value)
		}
	}
	return values
}

func intersectedDefault(left, right string, values []string) string {
	if left != "" && strings.EqualFold(left, right) && containsFold(values, left) {
		return left
	}
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func minimumPositive(left, right int) int {
	if left <= 0 {
		return right
	}
	if right <= 0 || left < right {
		return left
	}
	return right
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
