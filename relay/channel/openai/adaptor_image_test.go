package openai

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertImageRequestMapsSharedPresetAndClearsNativeFields(t *testing.T) {
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	converted, err := (&Adaptor{}).ConvertImageRequest(nil, info, dto.ImageRequest{
		Model:       "gpt-image-2",
		Prompt:      "poster",
		AspectRatio: "16:9",
		Resolution:  "4K",
	})
	require.NoError(t, err)
	request := converted.(dto.ImageRequest)
	assert.Equal(t, "3840x2160", request.Size)
	assert.Empty(t, request.AspectRatio)
	assert.Empty(t, request.Resolution)
}
