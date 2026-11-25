package handler

import (
	"errors"
	"net/http"
	"time"

	"yingce/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterCanvasShareRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.POST("/admin/canvas-shares/migrate-snapshots", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil { failService(c, err); return }
		var req struct { After string `json:"after"`; Limit int `json:"limit"` }
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		if err := c.ShouldBindJSON(&req); err != nil { fail(c, http.StatusBadRequest, errors.New("迁移请求无效")); return }
		result, err := svc.MigrateLegacyCanvasShares(actor, req.After, req.Limit)
		if err != nil { failService(c, err); return }
		ok(c, result)
	})
	r.GET("/canvas-projects/:id/share", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		share, err := svc.CanvasShareStatus(user.ID, c.Param("id"))
		if err != nil {
			fail(c, http.StatusNotFound, errors.New("画布不存在"))
			return
		}
		ok(c, gin.H{"share": share})
	})

	r.POST("/canvas-projects/:id/share", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		var req service.CanvasShareRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		share, err := svc.CreateCanvasShare(user.ID, c.Param("id"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"share": share})
	})

	r.DELETE("/canvas-projects/:id/share", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.DeleteCanvasShare(user.ID, c.Param("id")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"id": c.Param("id")})
	})

	r.PATCH("/canvas-projects/:id/share/copy-permission", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil { failService(c, err); return }
		var req struct { AllowCopy *bool `json:"allowCopy"` }
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		if err := c.ShouldBindJSON(&req); err != nil || req.AllowCopy == nil { fail(c, http.StatusBadRequest, errors.New("请选择是否允许保存副本")); return }
		share, err := svc.SetCanvasShareCopyPermission(user.ID, c.Param("id"), *req.AllowCopy)
		if err != nil { failService(c, err); return }
		ok(c, gin.H{"share": share})
	})

	r.GET("/public/canvas-shares/:token", func(c *gin.Context) {
		if !enforceRateLimit(c, "public-canvas:"+c.ClientIP(), 120, time.Minute) {
			return
		}
		share, err := svc.PublicCanvasShareVersion(c.Param("token"), c.Query("snapshot"))
		if err != nil {
			fail(c, http.StatusNotFound, errors.New("分享链接无效或已失效"))
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Robots-Tag", "noindex, nofollow")
		ok(c, share)
	})

	r.POST("/public/canvas-shares/:token/clone", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil { failService(c, err); return }
		if !enforceRateLimit(c, "canvas-share-clone:"+user.ID, 30, time.Minute) { return }
		var req struct { SnapshotID string `json:"snapshotId"`; RequestID string `json:"requestId"` }
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		if err := c.ShouldBindJSON(&req); err != nil || req.SnapshotID == "" || req.RequestID == "" { fail(c, http.StatusBadRequest, errors.New("请提供有效的画布版本和请求标识")); return }
		id, err := svc.CloneCanvasShare(user.ID, c.Param("token"), req.SnapshotID, req.RequestID)
		if err != nil { failService(c, err); return }
		c.Header("Cache-Control", "no-store")
		ok(c, gin.H{"canvasId": id})
	})

	r.GET("/public/canvas-shares/:token/resources/:resourceId/file", func(c *gin.Context) {
		if !enforceRateLimit(c, "public-canvas-resource:"+c.ClientIP(), 300, time.Minute) {
			return
		}
		opts := resourceAccessOptions(c)
		delivery, err := svc.PrepareSharedCanvasResourceDelivery(c.Param("token"), c.Param("resourceId"), opts, c.GetHeader("Range"))
		if err != nil {
			fail(c, http.StatusNotFound, errors.New("分享资源不存在"))
			return
		}
		c.Header("Content-Security-Policy", "sandbox")
		c.Header("X-Robots-Tag", "noindex, nofollow")
		serveResourceDelivery(c, delivery, "no-store", "")
	})

	r.GET("/public/canvas-shares/:token/snapshots/:snapshotId/resources/:resourceId/file", func(c *gin.Context) {
		if !enforceRateLimit(c, "public-canvas-resource:"+c.ClientIP(), 300, time.Minute) { return }
		delivery, err := svc.PrepareSharedCanvasSnapshotResourceDelivery(c.Param("token"), c.Param("snapshotId"), c.Param("resourceId"), resourceAccessOptions(c), c.GetHeader("Range"))
		if err != nil { fail(c, http.StatusNotFound, errors.New("分享资源不存在")); return }
		c.Header("Content-Security-Policy", "sandbox")
		c.Header("X-Robots-Tag", "noindex, nofollow")
		serveResourceDelivery(c, delivery, "no-store", "")
	})
}
