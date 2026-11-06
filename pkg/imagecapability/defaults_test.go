package imagecapability

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultCapabilityExposesAllWorkbenchPresets(t *testing.T) {
	capability := DefaultCapability()
	assert.Equal(t, SizeModeAspectRatioResolution, capability.SizeMode)
	assert.Equal(t, "1:1", capability.DefaultAspectRatio)
	assert.Equal(t, "1K", capability.DefaultResolution)
	assert.Contains(t, capability.AspectRatios, "16:9")
	assert.Contains(t, capability.AspectRatios, "auto")
	assert.Equal(t, []string{"1K", "2K", "4K"}, capability.Resolutions)
}

func TestDimensionsForMapsSharedPresetsToOpenAI(t *testing.T) {
	tests := []struct {
		aspectRatio string
		resolution  string
		want        string
	}{
		{aspectRatio: "1:1", resolution: "1K", want: "1024x1024"},
		{aspectRatio: "16:9", resolution: "4K", want: "3840x2160"},
		{aspectRatio: "9:16", resolution: "4K", want: "2160x3840"},
		{aspectRatio: "1:1", resolution: "4K", want: "2880x2880"},
		{aspectRatio: "auto", resolution: "4K", want: "auto"},
	}
	for _, test := range tests {
		t.Run(test.aspectRatio+"-"+test.resolution, func(t *testing.T) {
			got, err := DimensionsFor(test.aspectRatio, test.resolution)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestDimensionsForRejectsInvalidPreset(t *testing.T) {
	_, err := DimensionsFor("bad", "1K")
	require.Error(t, err)
	_, err = DimensionsFor("1:1", "8K")
	require.Error(t, err)
}
