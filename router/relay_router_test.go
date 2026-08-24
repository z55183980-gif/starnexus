package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRelayRouterRegistersAlphaSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)

	for _, route := range engine.Routes() {
		if route.Method == http.MethodPost && route.Path == "/v1/alpha/search" {
			return
		}
	}

	t.Fatal("POST /v1/alpha/search is not registered")
}

func TestVideoRouterRegistersOfficialCompatibilityPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetVideoRouter(engine)

	want := map[string]bool{
		http.MethodPost + " /":                                          false,
		http.MethodPost + " /api/v3/contents/generations/tasks":         false,
		http.MethodGet + " /api/v3/contents/generations/tasks/:task_id": false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, exists := want[key]; exists {
			want[key] = true
		}
	}
	for route, registered := range want {
		if !registered {
			t.Fatalf("compatibility route %s is not registered", route)
		}
	}
}
