package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newImageGenerationContext(t *testing.T, body map[string]any) *gin.Context {
	t.Helper()

	payload, err := common.Marshal(body)
	require.NoError(t, err)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(string(payload)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	return ctx
}

func TestGetAndValidOpenAIImageRequestPreservesProviderParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name, model, size, quality, format string
	}{
		{name: "gpt image 2 defaults", model: "gpt-image-2"},
		{name: "gpt image 2.5 defaults", model: "gpt-image-2.5"},
		{name: "gpt image 2.6 defaults", model: "gpt-image-2.6"},
		{name: "flare svg", model: "gpt-image-2.6-flare", size: "auto", quality: "max", format: "svg"},
		{name: "sunburst gif", model: "gpt-image-2.6-sunburst", size: "auto", quality: "xhigh", format: "gif"},
		{name: "custom format", model: "gpt-image-2", size: "1024x1024", format: "provider-format"},
		{name: "unaligned dimensions", model: "gpt-image-2", size: "1025x1024"},
		{name: "wide aspect ratio", model: "gpt-image-2", size: "3840x1024"},
		{name: "large pixel count", model: "gpt-image-2", size: "3840x3840"},
		{name: "large dimensions", model: "gpt-image-2", size: "4096x2160"},
		{name: "model alias", model: "public-image", size: "4096x2160", format: "svg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]any{"model": tt.model, "prompt": "a product photo"}
			for key, value := range map[string]string{"size": tt.size, "quality": tt.quality, "output_format": tt.format} {
				if value != "" {
					body[key] = value
				}
			}

			request, err := GetAndValidOpenAIImageRequest(newImageGenerationContext(t, body), relayconstant.RelayModeImagesGenerations)
			require.NoError(t, err)
			assert.Equal(t, tt.model, request.Model)
			assert.Equal(t, tt.size, request.Size)
			assert.Equal(t, tt.quality, request.Quality)

			encoded, err := common.Marshal(request)
			require.NoError(t, err)
			var forwarded map[string]any
			require.NoError(t, common.Unmarshal(encoded, &forwarded))
			for _, key := range []string{"size", "quality", "output_format"} {
				if value, exists := body[key]; exists {
					assert.Equal(t, value, forwarded[key], key)
				} else {
					assert.NotContains(t, forwarded, key)
				}
			}
		})
	}
}

func TestGetAndValidOpenAIImageRequestLeavesWorkbenchPresetForAdaptor(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// An OpenAI workbench preset must not constrain another provider's ingress.
	body := map[string]any{
		"model":        "gemini-3-pro-image-preview",
		"prompt":       "a product photo",
		"aspect_ratio": "16:9",
		"resolution":   "8K",
	}
	request, err := GetAndValidOpenAIImageRequest(newImageGenerationContext(t, body), relayconstant.RelayModeImagesGenerations)
	require.NoError(t, err)
	assert.Empty(t, request.Size)
	assert.Equal(t, "16:9", request.AspectRatio)
	assert.Equal(t, "8K", request.Resolution)
}
