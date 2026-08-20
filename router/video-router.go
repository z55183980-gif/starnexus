package router

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetVideoRouter(router *gin.Engine) {
	// Native Seedance protocol. The request body is kept in the upstream
	// content[] shape and still runs through starnexus auth, billing, task
	// persistence and polling. The marker is deliberately route-scoped so the
	// OpenAI-compatible /v1/videos behavior remains unchanged.
	nativeSeedanceMarker := func(c *gin.Context) {
		c.Set(string(constant.ContextKeyZQBAPINativeSeedanceRequest), true)
		c.Next()
	}
	nativeSeedanceRouter := router.Group("/api/v3")
	nativeSeedanceRouter.Use(middleware.RouteTag("relay"))
	nativeSeedanceRouter.Use(middleware.TokenAuth())
	{
		nativeSeedanceRouter.POST("/contents/generations/tasks", nativeSeedanceMarker, middleware.UserConcurrencyLimit(), middleware.RelayConcurrency(), middleware.Distribute(), controller.RelayTask)
		nativeSeedanceRouter.GET("/contents/generations/tasks/:task_id", nativeSeedanceMarker, middleware.RelayConcurrency(), controller.RelayTaskFetch)
	}

	directUploadRouter := router.Group("/v1/video-inputs")
	directUploadRouter.Use(middleware.RouteTag("relay"))
	directUploadRouter.Use(middleware.TokenAuth(), middleware.UploadRateLimit())
	{
		directUploadRouter.POST("/presign", controller.PresignDoubaoVideo2Upload)
		directUploadRouter.POST("/complete", controller.CompleteDoubaoVideo2Upload)
	}

	// Video proxy: accepts either session auth (dashboard) or token auth (API clients)
	videoProxyRouter := router.Group("/v1")
	videoProxyRouter.Use(middleware.RouteTag("relay"))
	videoProxyRouter.Use(middleware.TokenOrUserAuth())
	{
		videoProxyRouter.GET("/videos", controller.ListDoubaoVideo2OpenAIVideos)
		videoProxyRouter.DELETE("/videos/:task_id", controller.DeleteDoubaoVideo2OpenAIVideo)
		videoProxyRouter.GET("/videos/:task_id/content", middleware.RelayConcurrency(), controller.VideoProxy)
	}

	videoV1Router := router.Group("/v1")
	videoV1Router.Use(middleware.RouteTag("relay"))
	videoV1Router.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		videoV1Router.POST("/video/generations", middleware.UserConcurrencyLimit(), middleware.RelayConcurrency(), controller.RelayTask)
		videoV1Router.GET("/video/generations/:task_id", middleware.RelayConcurrency(), controller.RelayTaskFetch)
		videoV1Router.POST("/videos/:video_id/remix", middleware.UserConcurrencyLimit(), middleware.RelayConcurrency(), controller.RelayTask)
	}
	// openai compatible API video routes
	// docs: https://developers.openai.com/api/reference/resources/videos/methods/create
	{
		videoV1Router.POST("/videos", middleware.UserConcurrencyLimit(), middleware.RelayConcurrency(), controller.RelayTask)
		videoV1Router.GET("/videos/:task_id", middleware.RelayConcurrency(), controller.RelayTaskFetch)
	}

	klingV1Router := router.Group("/kling/v1")
	klingV1Router.Use(middleware.RouteTag("relay"))
	klingV1Router.Use(middleware.KlingRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		klingV1Router.POST("/videos/text2video", middleware.UserConcurrencyLimit(), middleware.RelayConcurrency(), controller.RelayTask)
		klingV1Router.POST("/videos/image2video", middleware.UserConcurrencyLimit(), middleware.RelayConcurrency(), controller.RelayTask)
		klingV1Router.GET("/videos/text2video/:task_id", middleware.RelayConcurrency(), controller.RelayTaskFetch)
		klingV1Router.GET("/videos/image2video/:task_id", middleware.RelayConcurrency(), controller.RelayTaskFetch)
	}

	// Jimeng official API routes - direct mapping to official API format
	jimengOfficialGroup := router.Group("jimeng")
	jimengOfficialGroup.Use(middleware.RouteTag("relay"))
	jimengOfficialGroup.Use(middleware.JimengRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		// Maps to: /?Action=CVSync2AsyncSubmitTask&Version=2022-08-31 and /?Action=CVSync2AsyncGetResult&Version=2022-08-31
		jimengOfficialGroup.POST("/", middleware.RelayConcurrency(), controller.RelayTask)
	}
}
