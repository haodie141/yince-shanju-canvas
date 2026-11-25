package handler

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

var errSystemProxyResponseTooLarge = errors.New("系统渠道流式响应超过配置上限")
var systemProxyDoneEvent = regexp.MustCompile(`(?m)^data: ?\[DONE\]\r?\n\r?\n`)

func isSystemProxyEventStream(resp *http.Response) bool {
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, "text/event-stream")
}

func copySystemProxyResponseHeaders(c *gin.Context, resp *http.Response) {
	for _, key := range []string{"Content-Type", "Cache-Control", "Content-Disposition"} {
		if value := resp.Header.Get(key); value != "" {
			c.Header(key, value)
		}
	}
	c.Header("X-Content-Type-Options", "nosniff")
}

func streamSystemProxyResponse(c *gin.Context, resp *http.Response, responseLimit int64) ([]byte, error) {
	copySystemProxyResponseHeaders(c, resp)
	// 同时通知镜像内和可能存在的外层 Nginx，不得重新缓冲模型事件流。
	c.Header("Cache-Control", "no-cache, no-store, no-transform")
	c.Header("X-Accel-Buffering", "no")
	c.Status(resp.StatusCode)
	c.Writer.WriteHeaderNow()
	c.Writer.Flush()

	var captured bytes.Buffer
	buffer := make([]byte, 32<<10)
	remaining := responseLimit
	for {
		readLimit := int64(len(buffer))
		if remaining < readLimit {
			readLimit = remaining + 1
		}
		if readLimit < 1 {
			readLimit = 1
		}
		read, readErr := resp.Body.Read(buffer[:int(readLimit)])
		if read > 0 {
			if int64(read) > remaining {
				return captured.Bytes(), errSystemProxyResponseTooLarge
			}
			chunk := buffer[:read]
			previous := captured.Len()
			_, _ = captured.Write(chunk)
			if _, writeErr := c.Writer.Write(chunk); writeErr != nil {
				return captured.Bytes(), writeErr
			}
			c.Writer.Flush()
			remaining -= int64(read)
			// [DONE] 是流式协议的结束事件；客户端收到后可以立即关闭连接。
			// 继续等待上游 EOF 会将这个正常关闭误记为 context canceled。
			start := max(previous-32, 0)
			if start > 0 {
				start-- // 保留前一个换行，避免从行中间匹配。
			}
			if systemProxyDoneEvent.Match(captured.Bytes()[start:]) {
				return captured.Bytes(), nil
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return captured.Bytes(), nil
			}
			return captured.Bytes(), readErr
		}
	}
}
