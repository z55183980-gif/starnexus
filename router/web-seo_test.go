/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package router

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

func withSEOTestState(t *testing.T, headerNavModules string) {
	t.Helper()

	previousServerAddress := system_setting.ServerAddress
	previousSystemName := common.SystemName
	previousDocsLink := operation_setting.GetGeneralSetting().DocsLink
	previousLegal := *system_setting.GetLegalSettings()

	common.OptionMapRWMutex.Lock()
	optionMapWasNil := common.OptionMap == nil
	if optionMapWasNil {
		common.OptionMap = make(map[string]string)
	}
	previousHeaderNav, hadHeaderNav := common.OptionMap["HeaderNavModules"]
	previousAbout, hadAbout := common.OptionMap["About"]
	previousHomePageContent, hadHomePageContent := common.OptionMap["HomePageContent"]
	previousAPIBaseURL, hadAPIBaseURL := common.OptionMap["ApiBaseUrl"]
	common.OptionMap["HeaderNavModules"] = headerNavModules
	common.OptionMap["About"] = ""
	common.OptionMap["HomePageContent"] = ""
	common.OptionMap["ApiBaseUrl"] = "https://api.dkby.com"
	common.OptionMapRWMutex.Unlock()

	system_setting.ServerAddress = "https://DKBY.com"
	common.SystemName = "星域互联 StarNexus"
	operation_setting.GetGeneralSetting().DocsLink = "https://docs.dkby.com/docs/help.html"
	system_setting.GetLegalSettings().UserAgreement = ""
	system_setting.GetLegalSettings().PrivacyPolicy = ""
	t.Setenv(seoCanonicalOriginEnv, "")

	t.Cleanup(func() {
		system_setting.ServerAddress = previousServerAddress
		common.SystemName = previousSystemName
		operation_setting.GetGeneralSetting().DocsLink = previousDocsLink
		*system_setting.GetLegalSettings() = previousLegal

		common.OptionMapRWMutex.Lock()
		if optionMapWasNil {
			common.OptionMap = nil
			common.OptionMapRWMutex.Unlock()
			return
		}
		if hadHeaderNav {
			common.OptionMap["HeaderNavModules"] = previousHeaderNav
		} else {
			delete(common.OptionMap, "HeaderNavModules")
		}
		if hadAbout {
			common.OptionMap["About"] = previousAbout
		} else {
			delete(common.OptionMap, "About")
		}
		if hadHomePageContent {
			common.OptionMap["HomePageContent"] = previousHomePageContent
		} else {
			delete(common.OptionMap, "HomePageContent")
		}
		if hadAPIBaseURL {
			common.OptionMap["ApiBaseUrl"] = previousAPIBaseURL
		} else {
			delete(common.OptionMap, "ApiBaseUrl")
		}
		common.OptionMapRWMutex.Unlock()
	})
}

func performSEORequest(router http.Handler, method, target, host, forwardedProto string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, nil)
	request.Host = host
	if forwardedProto != "" {
		request.Header.Set("X-Forwarded-Proto", forwardedProto)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestCanonicalRedirectNormalizesSchemeAndHost(t *testing.T) {
	withSEOTestState(t, `{}`)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(webCanonicalRedirect())
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	tests := []struct {
		name           string
		host           string
		forwardedProto string
		wantStatus     int
		wantLocation   string
	}{
		{name: "canonical request", host: "dkby.com", forwardedProto: "https", wantStatus: http.StatusNoContent},
		{name: "http request", host: "dkby.com", forwardedProto: "http", wantStatus: http.StatusMovedPermanently, wantLocation: "https://dkby.com/?source=test"},
		{name: "www request", host: "www.dkby.com", forwardedProto: "https", wantStatus: http.StatusMovedPermanently, wantLocation: "https://dkby.com/?source=test"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performSEORequest(router, http.MethodGet, "/?source=test", test.host, test.forwardedProto)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if location := response.Header().Get("Location"); location != test.wantLocation {
				t.Fatalf("Location = %q, want %q", location, test.wantLocation)
			}
		})
	}
}

func TestCanonicalRedirectDoesNotInterceptAPIOrAssets(t *testing.T) {
	withSEOTestState(t, `{}`)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(webCanonicalRedirect())
	router.GET("/api/status", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.GET("/assets/app.js", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, target := range []string{"/api/status", "/assets/app.js"} {
		response := performSEORequest(router, http.MethodGet, target, "origin-s2.dkby.com", "https")
		if response.Code != http.StatusNoContent {
			t.Errorf("%s status = %d, want %d", target, response.Code, http.StatusNoContent)
		}
		if location := response.Header().Get("Location"); location != "" {
			t.Errorf("%s Location = %q, want empty", target, location)
		}
	}
}

func TestCanonicalRedirectUsesCloudflareVisitorScheme(t *testing.T) {
	withSEOTestState(t, `{}`)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(webCanonicalRedirect())
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "dkby.com"
	request.Header.Set("X-Forwarded-Proto", "http")
	request.Header.Set("CF-Visitor", `{"scheme":"https"}`)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if location := recorder.Header().Get("Location"); location != "" {
		t.Fatalf("Location = %q, want empty", location)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "dkby.com"
	request.Header.Set("X-Forwarded-Proto", "http")
	request.Header.Set("CF-Ray", "test-ray")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("Cloudflare request status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestSEOEndpointsReturnMachineReadableContent(t *testing.T) {
	withSEOTestState(t, `{"pricing":{"enabled":true,"requireAuth":true},"rankings":{"enabled":false},"about":false}`)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(webCanonicalRedirect())
	registerSEORoutes(router)

	robots := performSEORequest(router, http.MethodGet, "/robots.txt", "dkby.com", "https")
	if robots.Code != http.StatusOK {
		t.Fatalf("robots status = %d", robots.Code)
	}
	robotsBody := robots.Body.String()
	for _, expected := range []string{"User-agent: OAI-SearchBot", "User-agent: GPTBot", "Sitemap: https://dkby.com/sitemap.xml", "ai-input=yes", "Allow: /api/status", "Disallow: /api/"} {
		if !strings.Contains(robotsBody, expected) {
			t.Errorf("robots.txt missing %q", expected)
		}
	}
	if strings.Contains(strings.ToLower(robotsBody), "<!doctype") {
		t.Error("robots.txt contains frontend HTML")
	}

	sitemap := performSEORequest(router, http.MethodGet, "/sitemap.xml", "dkby.com", "https")
	if sitemap.Code != http.StatusOK {
		t.Fatalf("sitemap status = %d", sitemap.Code)
	}
	if contentType := sitemap.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/xml") {
		t.Fatalf("sitemap Content-Type = %q", contentType)
	}
	var document sitemapDocument
	if err := xml.Unmarshal(sitemap.Body.Bytes(), &document); err != nil {
		t.Fatalf("sitemap XML is invalid: %v", err)
	}
	if len(document.URLs) != 1 || document.URLs[0].Loc != "https://dkby.com/" {
		t.Fatalf("sitemap URLs = %#v, want homepage only", document.URLs)
	}

	llms := performSEORequest(router, http.MethodGet, "/llms.txt", "dkby.com", "https")
	if llms.Code != http.StatusOK {
		t.Fatalf("llms status = %d", llms.Code)
	}
	llmsBody := llms.Body.String()
	for _, expected := range []string{"# 星域互联 StarNexus", "https://dkby.com/", "https://docs.dkby.com/docs/help.html", "https://dkby.com/index.md", "https://dkby.com/llms-full.txt", "https://api.dkby.com"} {
		if !strings.Contains(llmsBody, expected) {
			t.Errorf("llms.txt missing %q", expected)
		}
	}

	markdown := performSEORequest(router, http.MethodGet, "/index.md", "dkby.com", "https")
	if markdown.Code != http.StatusOK || !strings.HasPrefix(markdown.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("index.md response = %d, %q", markdown.Code, markdown.Header().Get("Content-Type"))
	}
	for _, expected := range []string{"统一 AI API 网关", "https://api.dkby.com", "Codex", "信息时效"} {
		if !strings.Contains(markdown.Body.String(), expected) {
			t.Errorf("index.md missing %q", expected)
		}
	}

	fullReference := performSEORequest(router, http.MethodGet, "/llms-full.txt", "dkby.com", "https")
	if fullReference.Code != http.StatusOK {
		t.Fatalf("llms-full.txt status = %d", fullReference.Code)
	}
	for _, expected := range []string{"核心事实", "用户为什么会选择 StarNexus", "常见问题与可直接引用的回答", "引用与时效规则"} {
		if !strings.Contains(fullReference.Body.String(), expected) {
			t.Errorf("llms-full.txt missing %q", expected)
		}
	}
}

func TestPublicPathIndexabilityFollowsModuleConfiguration(t *testing.T) {
	withSEOTestState(t, `{"pricing":{"enabled":true,"requireAuth":false},"rankings":{"enabled":false},"about":true}`)

	if canonicalPath, ok := indexablePublicPath("/pricing/model-a/"); !ok || canonicalPath != "/pricing/model-a" {
		t.Fatalf("pricing detail = %q, %v", canonicalPath, ok)
	}
	if _, ok := indexablePublicPath("/rankings"); ok {
		t.Error("disabled rankings page must not be indexable")
	}
	if _, ok := indexablePublicPath("/about"); ok {
		t.Error("empty about page must not be indexable")
	}
	if !isKnownWebPath("/system-settings/security/audit") {
		t.Error("known nested settings route was classified as unknown")
	}
	if isKnownWebPath("/definitely-missing") {
		t.Error("unknown route was classified as known")
	}
}

func TestWebFallbackAddsCanonicalNoindexAndReal404(t *testing.T) {
	withSEOTestState(t, `{"pricing":{"enabled":true,"requireAuth":true},"rankings":{"enabled":false},"about":false}`)
	gin.SetMode(gin.TestMode)
	indexPage := []byte(`<!doctype html><html><head><title>StarNexus</title></head><body><div id="root"></div></body></html>`)
	assets := ThemeAssets{DefaultIndexPage: indexPage, ClassicIndexPage: indexPage}
	router := gin.New()
	router.Use(webCanonicalRedirect())
	router.NoRoute(webFallbackHandler(assets))

	home := performSEORequest(router, http.MethodGet, "/", "dkby.com", "https")
	if home.Code != http.StatusOK {
		t.Fatalf("home status = %d", home.Code)
	}
	for _, expected := range []string{`rel="canonical" href="https://dkby.com/"`, `content="index,follow`} {
		if !strings.Contains(home.Body.String(), expected) {
			t.Errorf("home HTML missing %q", expected)
		}
	}
	for _, expected := range []string{`property="og:url" content="https://dkby.com/"`, `type="application/ld+json"`, `"@type":"WebSite"`, `"@type":"Organization"`, `rel="describedby" href="https://dkby.com/llms.txt"`, `rel="alternate" type="text/markdown" href="https://dkby.com/index.md"`} {
		if !strings.Contains(home.Body.String(), expected) {
			t.Errorf("home discovery metadata missing %q", expected)
		}
	}
	if strings.Contains(home.Body.String(), "一个 API 接入多种 AI 模型") {
		t.Error("home HTML must not contain the optional SEO prerendered product copy")
	}
	for _, expected := range []string{`<https://dkby.com/llms.txt>; rel="describedby"`, `<https://dkby.com/index.md>; rel="alternate"; type="text/markdown"`} {
		if !strings.Contains(home.Header().Get("Link"), expected) {
			t.Errorf("home Link header missing %q", expected)
		}
	}

	protected := performSEORequest(router, http.MethodGet, "/sign-in", "dkby.com", "https")
	if protected.Code != http.StatusOK {
		t.Fatalf("known private route status = %d", protected.Code)
	}
	if robotsHeader := protected.Header().Get("X-Robots-Tag"); !strings.Contains(robotsHeader, "noindex") {
		t.Fatalf("known private route X-Robots-Tag = %q", robotsHeader)
	}
	if !strings.Contains(protected.Body.String(), `content="noindex,nofollow,noarchive"`) {
		t.Error("known private route HTML is missing noindex metadata")
	}

	missing := performSEORequest(router, http.MethodGet, "/definitely-missing", "dkby.com", "https")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown route status = %d, want 404", missing.Code)
	}
	if cacheControl := missing.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("unknown route Cache-Control = %q", cacheControl)
	}
}
