package middleware

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCanvasGroupFromHeader(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty", input: "", want: ""},
		{name: "ascii", input: "Codex", want: "Codex"},
		{name: "encoded unicode", input: "%E4%B8%AD%E6%96%87%E5%88%86%E7%BB%84", want: "中文分组"},
		{name: "encoded percent", input: "group%2525", want: "group%25"},
		{name: "malformed escape", input: "%E4%B8", wantErr: true},
		{name: "raw unicode", input: "中文分组", wantErr: true},
		{name: "control", input: "group\x00", wantErr: true},
		{name: "encoded control", input: "group%00", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := canvasGroupFromHeader(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestCanvasRelayDecodesGroupAndPreservesAuthorization(t *testing.T) {
	previous := setting.UserUsableGroups2JSONString()
	t.Cleanup(func() { require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previous)) })
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"Codex特惠":"description","percent%25":"description"}`))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(common.CanvasOriginContextKey, common.CanvasOrigin)
		common.SetContextKey(c, constant.ContextKeyUserGroup, "vip")
	})
	engine.GET("/api/canvas/v1/videos/task", CanvasRelay(""), func(c *gin.Context) {
		require.Empty(t, c.GetHeader("X-Canvas-Group"))
		require.Empty(t, c.GetHeader("Authorization"))
		c.String(http.StatusOK, c.GetString("canvas_group"))
	})
	for _, test := range []struct {
		header   string
		status   int
		contains string
	}{
		{"Codex%E7%89%B9%E6%83%A0", 200, "Codex特惠"},
		{"percent%2525", 200, "percent%25"},
		{"Codex", 403, "canvas_group_forbidden"},
		{"%E4%B8", 400, "canvas_invalid_group"},
	} {
		request := httptest.NewRequest("GET", "/api/canvas/v1/videos/task", nil)
		request.Header.Set("X-Canvas-Group", test.header)
		request.Header.Set("Authorization", "must-be-removed")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		require.Equal(t, test.status, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), test.contains)
	}
}

func TestCanvasRelayParsesMultipartAndRestoresBody(t *testing.T) {
	previousGroups := setting.UserUsableGroups2JSONString()
	t.Cleanup(func() { require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousGroups)) })
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"image":"description"}`))

	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.InitColumnNamesForTest()
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Model{}))
	require.NoError(t, db.Create(&model.Channel{Id: 1001, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "image", Model: "gpt-image-2", ChannelId: 1001, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Model{
		ModelName: "gpt-image-2",
		Endpoints: `{"image-generation":{"path":"/v1/images/edits","method":"POST"}}`,
		Status:    1,
	}).Error)
	t.Cleanup(func() {
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
		model.DB = oldDB
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.WriteField("prompt", "enhance detail"))
	part, err := writer.CreateFormFile("image", "reference.webp")
	require.NoError(t, err)
	_, err = part.Write([]byte("image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(common.CanvasOriginContextKey, common.CanvasOrigin)
		common.SetContextKey(c, constant.ContextKeyUserGroup, "vip")
	})
	engine.POST("/api/canvas/v1/images/edits", CanvasRelay("image"), func(c *gin.Context) {
		require.Equal(t, "/v1/images/edits", c.Request.URL.Path)
		require.Equal(t, "gpt-image-2", c.PostForm("model"))
		file, err := c.FormFile("image")
		require.NoError(t, err)
		opened, err := file.Open()
		require.NoError(t, err)
		defer opened.Close()
		contents, err := io.ReadAll(opened)
		require.NoError(t, err)
		require.Equal(t, "image-bytes", string(contents))
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodPost, "/api/canvas/v1/images/edits", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-Canvas-Group", "image")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
}
