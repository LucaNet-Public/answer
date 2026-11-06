package router

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apache/answer/internal/controller"
	"github.com/apache/answer/plugin"
	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/server"
)

type transportTestPlugin struct{}

func (transportTestPlugin) Info() plugin.Info { return plugin.Info{SlugName: "test_mcp_transport"} }
func (transportTestPlugin) RegisterMCPRouter(r *gin.RouterGroup, s *server.MCPServer) {
	r.GET("/test-transport", func(c *gin.Context) {
		names := []string{}
		for name := range s.ListTools() {
			names = append(names, name)
		}
		c.JSON(200, names)
	})
}
func TestMCPTransportExtensionPreservesLegacyAuthAndEnableGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	plugin.Register(transportTestPlugin{})
	var mc controller.MCPController
	a := &AnswerAPIRouter{mcpController: &mc}
	r := gin.New()
	enabled := true
	group := r.Group("/answer/api/v1", func(c *gin.Context) {
		if !enabled {
			c.AbortWithStatus(403)
		}
	})
	a.RegisterMCPRouter(group, func(c *gin.Context) {
		if c.GetHeader("Authorization") != "legacy-key" {
			c.AbortWithStatus(401)
		}
	})
	request := func(path, token string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", token)
		r.ServeHTTP(w, req)
		return w
	}
	if w := request("/answer/api/v1/mcp/sse", ""); w.Code != 401 {
		t.Fatalf("legacy auth lost: %d", w.Code)
	}
	if w := request("/answer/api/v1/test-transport", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "semantic_search") {
		t.Fatalf("existing tool registry unavailable: %d %s", w.Code, w.Body)
	}
	enabled = false
	for _, path := range []string{"/answer/api/v1/mcp/sse", "/answer/api/v1/test-transport"} {
		if w := request(path, "legacy-key"); w.Code != 403 {
			t.Fatalf("MCP switch bypassed on %s: %d", path, w.Code)
		}
	}
}
