// 全站默认模型设置：管理员读写与登录用户读取。

package handler

import (
	"net/http"

	"yingce/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterDefaultModelRoutes(r *gin.RouterGroup, svc *service.Service) {
	// 新用户首次登录在浏览器生成初始配置，需要读取管理员设定的默认模型。
	r.GET("/settings/default-models", func(c *gin.Context) {
		if _, err := currentUser(c, svc); err != nil {
			failService(c, err)
			return
		}
		setting, err := svc.DefaultModels()
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"defaults": setting})
	})

	r.GET("/admin/settings/default-models", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		setting, err := svc.AdminDefaultModels(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"defaults": setting})
	})

	r.PATCH("/admin/settings/default-models", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var req service.DefaultModels
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		setting, err := svc.UpdateDefaultModels(user, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"defaults": setting})
	})
}
