/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
)

const (
	canvasImageUploadKeyEnv      = "CANVAS_IMAGE_UPLOAD_API_KEY"
	canvasImageUploadEndpoint    = "https://wcf.coderapi.vip/image-upload/upload"
	canvasImagePublicURLDefault  = "https://coderapi.vip/image-upload/"
	canvasImagePublishMaxBytes   = int64(20 << 20)
	canvasImageUploadTimeout     = 2 * time.Minute
	canvasImageUploadResponseMax = int64(1 << 20)
)

var (
	// These variables keep the production destination fixed while allowing an
	// isolated HTTP fixture to exercise the upload protocol without real media.
	canvasImageUploadURL       = canvasImageUploadEndpoint
	canvasImagePublicURLPrefix = canvasImagePublicURLDefault
)

var (
	ErrCanvasImagePublishNotConfigured = errors.New("canvas image publishing is not configured")
	ErrCanvasImagePublishFailed        = errors.New("canvas image publishing failed")
	ErrCanvasImagePublishTooLarge      = errors.New("canvas image is too large for public upload")
)

type toolsImageUploadResponse struct {
	Success bool `json:"success"`
	Data    struct {
		URL string `json:"url"`
	} `json:"data"`
}

// PublishCanvasAssetImage makes an owner-checked canvas asset temporarily
// public through the fixed Tools image-upload endpoint. The credential is
// read only on the server and never becomes part of a browser request.
func PublishCanvasAssetImage(ctx context.Context, userID int, assetID string) (string, error) {
	if userID <= 0 || !model.ValidCanvasID(assetID) {
		return "", model.ErrCanvasInvalid
	}
	if strings.TrimSpace(os.Getenv(canvasImageUploadKeyEnv)) == "" {
		return "", ErrCanvasImagePublishNotConfigured
	}
	var asset model.CanvasAsset
	if err := model.DB.Where("user_id = ? AND asset_id = ? AND deleted_at = 0", userID, assetID).First(&asset).Error; err != nil {
		return "", err
	}
	if asset.Kind != "image" {
		return "", model.ErrCanvasInvalid
	}
	contentType, err := canvasImageContentType(asset.MIME)
	if err != nil {
		return "", model.ErrCanvasInvalid
	}
	if asset.Size <= 0 || asset.Size > canvasImagePublishMaxBytes {
		return "", ErrCanvasImagePublishTooLarge
	}
	path, err := CanvasStoragePath(asset.StorageKey)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", ErrCanvasImagePublishFailed
	}
	defer file.Close()
	return uploadCanvasImageToTools(ctx, file, canvasImageFilename(contentType), contentType, asset.Size)
}

func canvasImageContentType(declared string) (string, error) {
	parsed, _, err := mime.ParseMediaType(strings.TrimSpace(declared))
	if err != nil {
		return "", err
	}
	switch strings.ToLower(parsed) {
	case "image/jpeg", "image/png", "image/webp":
		return strings.ToLower(parsed), nil
	default:
		return "", fmt.Errorf("unsupported canvas image type")
	}
}

func canvasImageFilename(contentType string) string {
	extension := map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
	}[contentType]
	return "canvas-reference" + extension
}

func uploadCanvasImageToTools(ctx context.Context, source io.Reader, filename, contentType string, expectedSize int64) (string, error) {
	key := strings.TrimSpace(os.Getenv(canvasImageUploadKeyEnv))
	if key == "" {
		return "", ErrCanvasImagePublishNotConfigured
	}
	if expectedSize <= 0 || expectedSize > canvasImagePublishMaxBytes {
		return "", ErrCanvasImagePublishTooLarge
	}
	if _, err := canvasImageContentType(contentType); err != nil {
		return "", model.ErrCanvasInvalid
	}
	if strings.TrimSpace(canvasImageUploadURL) == "" {
		return "", ErrCanvasImagePublishFailed
	}

	uploadContext, cancel := context.WithTimeout(ctx, canvasImageUploadTimeout)
	defer cancel()
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	request, err := http.NewRequestWithContext(uploadContext, http.MethodPost, canvasImageUploadURL, reader)
	if err != nil {
		_ = reader.CloseWithError(err)
		return "", ErrCanvasImagePublishFailed
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", form.FormDataContentType())

	writeDone := make(chan error, 1)
	go func() {
		part, writeErr := form.CreateFormFile("file", filepath.Base(filename))
		if writeErr == nil {
			written, copyErr := io.Copy(part, io.LimitReader(source, canvasImagePublishMaxBytes+1))
			if copyErr != nil {
				writeErr = copyErr
			} else if written > canvasImagePublishMaxBytes {
				writeErr = ErrCanvasImagePublishTooLarge
			} else if written != expectedSize {
				writeErr = ErrCanvasImagePublishFailed
			}
		}
		if closeErr := form.Close(); writeErr == nil && closeErr != nil {
			writeErr = closeErr
		}
		_ = writer.CloseWithError(writeErr)
		writeDone <- writeErr
	}()

	client := GetHttpClient()
	if client == nil {
		_ = reader.CloseWithError(ErrCanvasImagePublishFailed)
		<-writeDone
		return "", ErrCanvasImagePublishFailed
	}
	response, err := client.Do(request)
	if err != nil {
		_ = reader.CloseWithError(err)
		<-writeDone
		return "", ErrCanvasImagePublishFailed
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, canvasImageUploadResponseMax+1))
	writeErr := <-writeDone
	if writeErr != nil {
		if errors.Is(writeErr, ErrCanvasImagePublishTooLarge) {
			return "", writeErr
		}
		return "", ErrCanvasImagePublishFailed
	}
	if readErr != nil || int64(len(body)) > canvasImageUploadResponseMax || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", ErrCanvasImagePublishFailed
	}
	var envelope toolsImageUploadResponse
	if err := json.Unmarshal(body, &envelope); err != nil || !envelope.Success {
		return "", ErrCanvasImagePublishFailed
	}
	publicURL := strings.TrimSpace(envelope.Data.URL)
	if !validCanvasPublicImageURL(publicURL) {
		return "", ErrCanvasImagePublishFailed
	}
	return publicURL, nil
}

func validCanvasPublicImageURL(raw string) bool {
	if !strings.HasPrefix(raw, canvasImagePublicURLPrefix) {
		return false
	}
	parsed, err := url.ParseRequestURI(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.RawQuery == "" && parsed.Fragment == ""
}
