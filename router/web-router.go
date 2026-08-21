package router

import (
	"embed"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// ThemeAssets holds the embedded frontend assets for both themes.
type ThemeAssets struct {
	DefaultBuildFS   embed.FS
	DefaultIndexPage []byte
	ClassicBuildFS   embed.FS
	ClassicIndexPage []byte
}

func SetWebRouter(router *gin.Engine, assets ThemeAssets) {
	defaultFS := common.EmbedFolder(assets.DefaultBuildFS, "web/default/dist")
	classicFS := common.EmbedFolder(assets.ClassicBuildFS, "web/classic/dist")
	themeFS := common.NewThemeAwareFS(defaultFS, classicFS)

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(middleware.GlobalWebRateLimit())
	router.Use(middleware.Cache())
	router.Use(webCanonicalRedirect())
	registerSEORoutes(router)
	rootHandler := webFallbackHandler(assets)
	router.GET("/", rootHandler)
	router.HEAD("/", rootHandler)
	router.Use(static.Serve("/", themeFS))
	router.NoRoute(webFallbackHandler(assets))
}

func webFallbackHandler(assets ThemeAssets) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		requestPath := c.Request.URL.Path
		if strings.HasPrefix(requestPath, "/v1") || strings.HasPrefix(requestPath, "/api") || strings.HasPrefix(requestPath, "/assets") {
			c.Header("X-Robots-Tag", "noindex, nofollow, noarchive")
			controller.RelayNotFound(c)
			return
		}

		c.Header("Cache-Control", "no-cache")
		indexPage := assets.DefaultIndexPage
		if common.GetTheme() == "classic" {
			indexPage = assets.ClassicIndexPage
		}

		if canonicalPath, indexable := indexablePublicPath(requestPath); indexable {
			origin, _ := canonicalOrigin(c)
			canonicalURL := canonicalPageURL(origin, canonicalPath)
			links := []string{
				fmt.Sprintf("<%s>; rel=\"canonical\"", canonicalURL),
				fmt.Sprintf("<%s>; rel=\"describedby\"", canonicalPageURL(origin, "/llms.txt")),
			}
			if canonicalPath == "/" {
				links = append(links, fmt.Sprintf("<%s>; rel=\"alternate\"; type=\"text/markdown\"", canonicalPageURL(origin, "/index.md")))
			}
			c.Header("Link", strings.Join(links, ", "))
			page := renderWebIndex(indexPage, indexableHeadMarkup(canonicalURL, canonicalPath))
			if canonicalPath == "/" && !optionValueIsPresent("HomePageContent") {
				page = renderPrerenderedHomeContent(page, prerenderedHomeContent(origin))
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", page)
			return
		}

		c.Header("X-Robots-Tag", "noindex, nofollow, noarchive")
		status := http.StatusOK
		if !isKnownWebPath(requestPath) {
			status = http.StatusNotFound
			c.Header("Cache-Control", "no-store")
		}
		c.Data(status, "text/html; charset=utf-8", renderWebIndex(indexPage, noindexHeadMarkup()))
	}
}
