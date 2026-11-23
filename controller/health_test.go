package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetHealthDoesNotDependOnApplicationState(t *testing.T) {
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/health", nil)

	GetHealth(context)

	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"success":true}`, response.Body.String())
}
