package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"yingce/backend/internal/service"
)

func RegisterPromptModerationRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/admin/settings/prompt-moderation/logs", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.Query("page"))
		limit, _ := strconv.Atoi(c.Query("pageSize"))
		if page < 1 {
			page = 1
		}
		if limit < 1 || limit > 100 {
			limit = 30
		}
		logs, total, err := svc.AdminPromptModerationLogs(user, page, limit)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"logs": logs, "total": total, "page": page, "pageSize": limit})
	})
	r.GET("/admin/settings/prompt-moderation", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		setting, err := svc.AdminPromptModerationSetting(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"setting": setting})
	})
	r.PATCH("/admin/settings/prompt-moderation", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		var setting service.PromptModerationSetting
		if err := c.ShouldBindJSON(&setting); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := svc.UpdatePromptModerationSetting(user, setting)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"setting": result})
	})
	r.POST("/admin/settings/prompt-moderation/test", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
		var request struct {
			Setting service.PromptModerationSetting `json:"setting"`
			Prompt  string                          `json:"prompt"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := svc.TestPromptModeration(c.Request.Context(), user, request.Setting, request.Prompt)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
}
