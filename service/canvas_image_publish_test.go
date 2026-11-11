package service

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUploadCanvasImageToToolsUsesServerOnlyKeyAndMultipart(t *testing.T) {
	const key = "sk-test-canvas-publish"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer "+key, request.Header.Get("Authorization"))
		mediaType, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		require.NoError(t, err)
		require.Equal(t, "multipart/form-data", mediaType)
		reader := multipart.NewReader(request.Body, params["boundary"])
		part, err := reader.NextPart()
		require.NoError(t, err)
		require.Equal(t, "file", part.FormName())
		require.Equal(t, "canvas-reference.png", part.FileName())
		content, err := io.ReadAll(part)
		require.NoError(t, err)
		require.Equal(t, []byte("png-bytes"), content)
		_, err = reader.NextPart()
		require.ErrorIs(t, err, io.EOF)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"success": true,
			"data":    map[string]string{"url": "https://coderapi.vip/image-upload/m_fixture"},
		})
	}))
	defer server.Close()

	previousClient := httpClient
	previousURL := canvasImageUploadURL
	previousPrefix := canvasImagePublicURLPrefix
	t.Cleanup(func() {
		httpClient = previousClient
		canvasImageUploadURL = previousURL
		canvasImagePublicURLPrefix = previousPrefix
	})
	httpClient = server.Client()
	canvasImageUploadURL = server.URL
	canvasImagePublicURLPrefix = "https://coderapi.vip/image-upload/"
	t.Setenv(canvasImageUploadKeyEnv, key)

	url, err := uploadCanvasImageToTools(t.Context(), strings.NewReader("png-bytes"), "canvas-reference.png", "image/png", int64(len("png-bytes")))
	require.NoError(t, err)
	require.Equal(t, "https://coderapi.vip/image-upload/m_fixture", url)
}

func TestUploadCanvasImageToToolsRequiresConfiguration(t *testing.T) {
	t.Setenv(canvasImageUploadKeyEnv, "")
	_, err := uploadCanvasImageToTools(t.Context(), strings.NewReader("png-bytes"), "canvas-reference.png", "image/png", int64(len("png-bytes")))
	require.ErrorIs(t, err, ErrCanvasImagePublishNotConfigured)
}
