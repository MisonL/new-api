package middleware

import (
	"net/http"
	"net/url"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// turnstilePassTTL 定义 Turnstile 通过标记的有效期。用户解一次谜之后，标记
// 保留在该会话中；超过此时间后即使标记存在也重新要求校验。
const turnstilePassTTL = 1 * time.Hour

type turnstileCheckResponse struct {
	Success bool `json:"success"`
}

// turnstilePassActive 判断会话里的通过标记是否仍在有效期内。标记存的是
// 通过时间戳；旧实现写的是 bool true，没有任何时效信息，等同于整个会话
// cookie 有效期内（30 天）都免检，因此一律按过期处理。
func turnstilePassActive(value any, now time.Time) bool {
	passedAt, ok := value.(int64)
	if !ok {
		return false
	}
	return now.Sub(time.Unix(passedAt, 0)) <= turnstilePassTTL
}

func TurnstileCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.TurnstileCheckEnabled {
			session := sessions.Default(c)
			turnstileChecked := session.Get("turnstile")
			if turnstilePassActive(turnstileChecked, time.Now()) {
				c.Next()
				return
			}
			response := c.Query("turnstile")
			if response == "" {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "Turnstile token 为空",
				})
				c.Abort()
				return
			}
			rawRes, err := http.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", url.Values{
				"secret":   {common.TurnstileSecretKey},
				"response": {response},
				"remoteip": {c.ClientIP()},
			})
			if err != nil {
				common.SysLog(err.Error())
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				c.Abort()
				return
			}
			defer rawRes.Body.Close()
			var res turnstileCheckResponse
			err = common.DecodeJson(rawRes.Body, &res)
			if err != nil {
				common.SysLog(err.Error())
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				c.Abort()
				return
			}
			if !res.Success {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "Turnstile 校验失败，请刷新重试！",
				})
				c.Abort()
				return
			}
			session.Set("turnstile", time.Now().Unix())
			err = session.Save()
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"message": "无法保存会话信息，请重试",
					"success": false,
				})
				// A rejected request must always abort, or the handler chain
				// continues into the protected route.
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
