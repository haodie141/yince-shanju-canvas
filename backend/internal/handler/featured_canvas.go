package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"yingce/backend/internal/service"
)

func RegisterFeaturedCanvasRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/public/featured", func(c *gin.Context) {
		if !enforceRateLimit(c, "featured-list:"+c.ClientIP(), 120, time.Minute) {
			return
		}
		items, err := svc.FeaturedCanvases(nil, false)
		if err != nil {
			failService(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		ok(c, gin.H{"items": items})
	})
	r.GET("/public/featured/:id/video", func(c *gin.Context) {
		if !enforceRateLimit(c, "featured-video:"+c.ClientIP(), 300, time.Minute) {
			return
		}
		delivery, err := svc.PrepareFeaturedVideo(c.Param("id"), true, resourceAccessOptions(c), c.GetHeader("Range"))
		if err != nil {
			fail(c, http.StatusNotFound, errors.New("精选视频不可用"))
			return
		}
		c.Header("Content-Security-Policy", "sandbox")
		serveResourceDelivery(c, delivery, "public, max-age=300", "")
	})
	r.GET("/public/featured/:id/cover", func(c *gin.Context) {
		if !enforceRateLimit(c, "featured-cover:"+c.ClientIP(), 300, time.Minute) {
			return
		}
		delivery, err := svc.PrepareFeaturedCover(c.Param("id"), true, resourceAccessOptions(c), c.GetHeader("Range"))
		if err != nil {
			fail(c, http.StatusNotFound, errors.New("精选封面不可用"))
			return
		}
		c.Header("Content-Security-Policy", "sandbox")
		serveResourceDelivery(c, delivery, "public, max-age=300", "")
	})
	r.GET("/admin/featured", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		items, err := svc.FeaturedCanvases(actor, true)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"items": items})
	})
	r.GET("/admin/featured/:id/video", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		delivery, err := svc.PrepareAdminFeaturedVideo(actor, c.Param("id"), resourceAccessOptions(c), c.GetHeader("Range"))
		if err != nil {
			failService(c, err)
			return
		}
		c.Header("Content-Security-Policy", "sandbox")
		serveResourceDelivery(c, delivery, "private, max-age=300", "")
	})
	r.GET("/admin/featured/:id/cover", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		delivery, err := svc.PrepareAdminFeaturedCover(actor, c.Param("id"), resourceAccessOptions(c), c.GetHeader("Range"))
		if err != nil {
			failService(c, err)
			return
		}
		c.Header("Content-Security-Policy", "sandbox")
		serveResourceDelivery(c, delivery, "private, max-age=300", "")
	})
	save := func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.FeaturedCanvasRequest
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, errors.New("精选内容格式无效"))
			return
		}
		entry, err := svc.SaveFeaturedCanvas(actor, c.Param("id"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"item": entry})
	}
	r.POST("/admin/featured", save)
	r.PUT("/admin/featured/:id", save)
	r.POST("/admin/featured/share-preview", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.FeaturedSharePreviewRequest
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, errors.New("分享链接格式无效"))
			return
		}
		preview, err := svc.FeaturedSharePreview(actor, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, preview)
	})
	r.POST("/admin/featured/reorder", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.FeaturedReorderRequest
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, errors.New("排序请求无效"))
			return
		}
		items, err := svc.ReorderFeaturedCanvases(actor, req.Items)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"items": items})
	})
	r.PATCH("/admin/featured/:id/enabled", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
			fail(c, http.StatusBadRequest, errors.New("请选择是否在首页展示"))
			return
		}
		entry, err := svc.SetFeaturedCanvasEnabled(actor, c.Param("id"), *req.Enabled)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"item": entry})
	})
	r.DELETE("/admin/featured/:id", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.DeleteFeaturedCanvas(actor, c.Param("id")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"id": c.Param("id")})
	})
}
