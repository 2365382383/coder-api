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
	canvasVideoUploadEndpoint    = "https://wcf.coderapi.vip/media-upload/upload"
	canvasVideoPublicURLDefault  = "https://coderapi.vip/media-upload/"
	canvasVideoPublishMaxBytes   = int64(512 << 20)
	canvasVideoUploadTimeout     = time.Hour
	canvasImageUploadResponseMax = int64(1 << 20)
)

var (
	// These variables keep production destinations fixed while allowing
	// isolated HTTP fixtures to exercise the upload protocol without media.
	canvasImageUploadURL       = canvasImageUploadEndpoint
	canvasImagePublicURLPrefix = canvasImagePublicURLDefault
	canvasVideoUploadURL       = canvasVideoUploadEndpoint
	canvasVideoPublicURLPrefix = canvasVideoPublicURLDefault
)

var (
	ErrCanvasImagePublishNotConfigured = errors.New("canvas image publishing is not configured")
	ErrCanvasImagePublishFailed        = errors.New("canvas image publishing failed")
	ErrCanvasImagePublishTooLarge      = errors.New("canvas image is too large for public upload")
	ErrCanvasVideoPublishNotConfigured = errors.New("canvas video publishing is not configured")
	ErrCanvasVideoPublishFailed        = errors.New("canvas video publishing failed")
	ErrCanvasVideoPublishTooLarge      = errors.New("canvas video is too large for public upload")
)

type toolsMediaUploadResponse struct {
	Success bool `json:"success"`
	Data    struct {
		URL string `json:"url"`
	} `json:"data"`
}

type canvasPublishErrors struct {
	notConfigured error
	failed        error
	tooLarge      error
}

// PublishCanvasAssetImage makes an owner-checked canvas asset temporarily
// public through the fixed Tools image-upload endpoint. The credential is
// read only on the server and never becomes part of a browser request.
func PublishCanvasAssetImage(ctx context.Context, userID int, assetID string) (string, error) {
	asset, err := canvasAssetForPublishing(userID, assetID)
	if err != nil {
		return "", err
	}
	if asset.Kind != "image" {
		return "", model.ErrCanvasInvalid
	}
	return publishCanvasImageAsset(ctx, asset)
}

// PublishCanvasAssetVideo makes an owner-checked canvas video available through
// the public media host, using the server-only New API key.
func PublishCanvasAssetVideo(ctx context.Context, userID int, assetID string) (string, error) {
	asset, err := canvasAssetForPublishing(userID, assetID)
	if err != nil {
		return "", err
	}
	if asset.Kind != "video" {
		return "", model.ErrCanvasInvalid
	}
	return publishCanvasVideoAsset(ctx, asset)
}

// PublishCanvasAsset publishes an owner-checked image or video asset according
// to its stored media kind.
func PublishCanvasAsset(ctx context.Context, userID int, assetID string) (string, error) {
	asset, err := canvasAssetForPublishing(userID, assetID)
	if err != nil {
		return "", err
	}
	switch asset.Kind {
	case "image":
		return publishCanvasImageAsset(ctx, asset)
	case "video":
		return publishCanvasVideoAsset(ctx, asset)
	default:
		return "", model.ErrCanvasInvalid
	}
}

func canvasAssetForPublishing(userID int, assetID string) (model.CanvasAsset, error) {
	if userID <= 0 || !model.ValidCanvasID(assetID) {
		return model.CanvasAsset{}, model.ErrCanvasInvalid
	}
	var asset model.CanvasAsset
	if err := model.DB.Where("user_id = ? AND asset_id = ? AND deleted_at = 0", userID, assetID).First(&asset).Error; err != nil {
		return model.CanvasAsset{}, err
	}
	return asset, nil
}

func publishCanvasImageAsset(ctx context.Context, asset model.CanvasAsset) (string, error) {
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

func publishCanvasVideoAsset(ctx context.Context, asset model.CanvasAsset) (string, error) {
	contentType, err := canvasVideoContentType(asset.MIME)
	if err != nil {
		return "", model.ErrCanvasInvalid
	}
	if asset.Size <= 0 || asset.Size > canvasVideoPublishMaxBytes {
		return "", ErrCanvasVideoPublishTooLarge
	}
	path, err := CanvasStoragePath(asset.StorageKey)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", ErrCanvasVideoPublishFailed
	}
	defer file.Close()
	return uploadCanvasVideoToMediaHost(ctx, file, canvasVideoFilename(contentType), contentType, asset.Size)
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

func canvasVideoContentType(declared string) (string, error) {
	parsed, _, err := mime.ParseMediaType(strings.TrimSpace(declared))
	if err != nil {
		return "", err
	}
	switch strings.ToLower(parsed) {
	case "video/mp4", "video/webm", "video/quicktime":
		return strings.ToLower(parsed), nil
	default:
		return "", fmt.Errorf("unsupported canvas video type")
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

func canvasVideoFilename(contentType string) string {
	extension := map[string]string{
		"video/mp4":       ".mp4",
		"video/webm":      ".webm",
		"video/quicktime": ".mov",
	}[contentType]
	return "canvas-reference" + extension
}

func uploadCanvasImageToTools(ctx context.Context, source io.Reader, filename, contentType string, expectedSize int64) (string, error) {
	return uploadCanvasFileToTools(ctx, source, filename, contentType, expectedSize, canvasImageUploadURL, canvasImagePublicURLPrefix, canvasImagePublishMaxBytes, canvasImageUploadTimeout, canvasImageContentType, canvasPublishErrors{
		notConfigured: ErrCanvasImagePublishNotConfigured,
		failed:        ErrCanvasImagePublishFailed,
		tooLarge:      ErrCanvasImagePublishTooLarge,
	})
}

func uploadCanvasVideoToMediaHost(ctx context.Context, source io.Reader, filename, contentType string, expectedSize int64) (string, error) {
	return uploadCanvasFileToTools(ctx, source, filename, contentType, expectedSize, canvasVideoUploadURL, canvasVideoPublicURLPrefix, canvasVideoPublishMaxBytes, canvasVideoUploadTimeout, canvasVideoContentType, canvasPublishErrors{
		notConfigured: ErrCanvasVideoPublishNotConfigured,
		failed:        ErrCanvasVideoPublishFailed,
		tooLarge:      ErrCanvasVideoPublishTooLarge,
	})
}

func uploadCanvasFileToTools(ctx context.Context, source io.Reader, filename, contentType string, expectedSize int64, endpoint, publicURLPrefix string, maxBytes int64, timeout time.Duration, validateContentType func(string) (string, error), publishErrors canvasPublishErrors) (string, error) {
	key := strings.TrimSpace(os.Getenv(canvasImageUploadKeyEnv))
	if key == "" {
		return "", publishErrors.notConfigured
	}
	if expectedSize <= 0 || expectedSize > maxBytes {
		return "", publishErrors.tooLarge
	}
	contentType, err := validateContentType(contentType)
	if err != nil {
		return "", model.ErrCanvasInvalid
	}
	if strings.TrimSpace(endpoint) == "" {
		return "", publishErrors.failed
	}

	uploadContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	request, err := http.NewRequestWithContext(uploadContext, http.MethodPost, endpoint, reader)
	if err != nil {
		_ = reader.CloseWithError(err)
		return "", publishErrors.failed
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", form.FormDataContentType())

	writeDone := make(chan error, 1)
	go func() {
		part, writeErr := form.CreateFormFile("file", filepath.Base(filename))
		if writeErr == nil {
			written, copyErr := io.Copy(part, io.LimitReader(source, maxBytes+1))
			if copyErr != nil {
				writeErr = copyErr
			} else if written > maxBytes {
				writeErr = publishErrors.tooLarge
			} else if written != expectedSize {
				writeErr = publishErrors.failed
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
		_ = reader.CloseWithError(publishErrors.failed)
		<-writeDone
		return "", publishErrors.failed
	}
	response, err := client.Do(request)
	if err != nil {
		_ = reader.CloseWithError(err)
		<-writeDone
		return "", publishErrors.failed
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, canvasImageUploadResponseMax+1))
	writeErr := <-writeDone
	if writeErr != nil {
		if errors.Is(writeErr, publishErrors.tooLarge) {
			return "", writeErr
		}
		return "", publishErrors.failed
	}
	if readErr != nil || int64(len(body)) > canvasImageUploadResponseMax || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", publishErrors.failed
	}
	var envelope toolsMediaUploadResponse
	if err := json.Unmarshal(body, &envelope); err != nil || !envelope.Success {
		return "", publishErrors.failed
	}
	publicURL := strings.TrimSpace(envelope.Data.URL)
	if !validCanvasPublicURL(publicURL, publicURLPrefix) {
		return "", publishErrors.failed
	}
	return publicURL, nil
}

func validCanvasPublicURL(raw, prefix string) bool {
	if !strings.HasPrefix(raw, prefix) {
		return false
	}
	parsed, err := url.ParseRequestURI(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.RawQuery == "" && parsed.Fragment == ""
}
