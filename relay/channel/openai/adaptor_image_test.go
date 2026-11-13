package openai

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertImageRequestPreservesProviderParametersAndWorkbenchPresets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		fields   map[string]any
		wantSize string
	}{
		{
			name: "svg parameters",
			fields: map[string]any{
				"model": "gpt-image-2.6-flare", "size": "auto", "quality": "max",
				"output_format": "svg", "complexity": "low", "svg": "provider-native",
			},
			wantSize: "auto",
		},
		{
			name: "gif parameters and explicit zero",
			fields: map[string]any{
				"model": "gpt-image-2.6-sunburst", "size": "auto", "output_format": "gif",
				"frame_count": 2, "frame_duration_ms": 0,
			},
			wantSize: "auto",
		},
		{
			name: "provider size and format",
			fields: map[string]any{
				"model": "gpt-image-2", "size": "4096x2160", "output_format": "provider-format",
			},
			wantSize: "4096x2160",
		},
		{
			name:   "omitted provider defaults",
			fields: map[string]any{"model": "gpt-image-2.6-flare"},
		},
		{
			name:     "workbench landscape 4K",
			fields:   map[string]any{"model": "gpt-image-2.6-flare", "aspect_ratio": "16:9", "resolution": "4K"},
			wantSize: "3840x2160",
		},
		{
			name:     "workbench auto",
			fields:   map[string]any{"model": "gpt-image-2.6-flare", "aspect_ratio": "auto", "resolution": "4K"},
			wantSize: "auto",
		},
		{
			name: "explicit size takes priority over workbench presets",
			fields: map[string]any{
				"model": "gpt-image-2.6-flare", "size": "4096x2160",
				"aspect_ratio": "provider-ratio", "resolution": "8K",
			},
			wantSize: "4096x2160",
		},
	}

	for _, tt := range tests {
		for _, transport := range []string{"json generation", "json edit", "multipart edit"} {
			t.Run(tt.name+"/"+transport, func(t *testing.T) {
				fields := map[string]any{"prompt": "a product photo"}
				for key, value := range tt.fields {
					fields[key] = value
				}
				payload, err := common.Marshal(fields)
				require.NoError(t, err)
				var expectedJSON map[string]any
				require.NoError(t, common.Unmarshal(payload, &expectedJSON))
				mode, path := relayconstant.RelayModeImagesGenerations, "/v1/images/generations"
				if transport != "json generation" {
					mode, path = relayconstant.RelayModeImagesEdits, "/v1/images/edits"
				}
				var body bytes.Buffer
				contentType := "application/json"
				if transport == "multipart edit" {
					writer := multipart.NewWriter(&body)
					for key, value := range fields {
						require.NoError(t, writer.WriteField(key, fmt.Sprint(value)))
					}
					for _, name := range []string{"image", "markup"} {
						part, err := writer.CreateFormFile(name, name+".png")
						require.NoError(t, err)
						_, err = io.WriteString(part, "fake "+name)
						require.NoError(t, err)
					}
					require.NoError(t, writer.Close())
					contentType = writer.FormDataContentType()
				} else {
					_, err = body.Write(payload)
					require.NoError(t, err)
				}

				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest(http.MethodPost, path, &body)
				ctx.Request.Header.Set("Content-Type", contentType)
				request, err := helper.GetAndValidOpenAIImageRequest(ctx, mode)
				require.NoError(t, err)
				converted, err := (&Adaptor{}).ConvertImageRequest(ctx, &relaycommon.RelayInfo{RelayMode: mode}, *request)
				require.NoError(t, err)

				forwarded := map[string]any{}
				if transport == "multipart edit" {
					payload, ok := converted.(*bytes.Buffer)
					require.True(t, ok)
					replay := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload.Bytes()))
					replay.Header.Set("Content-Type", ctx.Request.Header.Get("Content-Type"))
					require.NoError(t, replay.ParseMultipartForm(1<<20))
					t.Cleanup(func() { require.NoError(t, replay.MultipartForm.RemoveAll()) })
					for key := range replay.MultipartForm.Value {
						forwarded[key] = replay.FormValue(key)
					}
					for _, name := range []string{"image", "markup"} {
						require.Len(t, replay.MultipartForm.File[name], 1)
						file, err := replay.MultipartForm.File[name][0].Open()
						require.NoError(t, err)
						data, err := io.ReadAll(file)
						require.NoError(t, file.Close())
						require.NoError(t, err)
						assert.Equal(t, "fake "+name, string(data))
					}
				} else {
					out, ok := converted.(dto.ImageRequest)
					require.True(t, ok)
					payload, err := common.Marshal(out)
					require.NoError(t, err)
					require.NoError(t, common.Unmarshal(payload, &forwarded))
				}

				if tt.wantSize == "" {
					assert.NotContains(t, forwarded, "size")
				} else {
					assert.Equal(t, tt.wantSize, forwarded["size"])
				}
				assert.NotContains(t, forwarded, "aspect_ratio")
				assert.NotContains(t, forwarded, "resolution")
				for _, key := range []string{"model", "quality", "output_format", "complexity", "svg", "frame_count", "frame_duration_ms"} {
					if expected, exists := fields[key]; exists {
						if transport == "multipart edit" {
							assert.Equal(t, fmt.Sprint(expected), forwarded[key], key)
						} else {
							assert.Equal(t, expectedJSON[key], forwarded[key], key)
						}
					} else {
						assert.NotContains(t, forwarded, key)
					}
				}
			})
		}
	}
}
