package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// telegramMaxAuthAge bounds how long a widget-signed auth payload stays
// acceptable. Without it the signed query string is a permanent login
// credential for the account: any leak of the callback URL (browser history,
// proxy logs) could be replayed at any later time.
const telegramMaxAuthAge = 24 * time.Hour

// telegramClockSkew bounds how far into the future a signed auth_date may be.
// Without it a future timestamp (signable only by the token holder, but still)
// would stay inside the freshness window for as long as the timestamp lies
// ahead.
const telegramClockSkew = 5 * time.Minute

// telegramUsedAuth remembers consumed auth payloads so a valid signature
// cannot be replayed even within the freshness window. Keyed by the full
// signed hash; entries expire lazily. Widgets sign per-login data, so the
// set stays bounded by real logins.
var telegramUsedAuth sync.Map // map[string]time.Time (consumed at)

// telegramAuthOnce rejects a signature that was already consumed. A cleanup
// pass runs opportunistically; correctness does not depend on it, because an
// expired entry is rejected by the auth_date freshness check anyway.
func telegramAuthOnce(hash string) bool {
	now := time.Now()
	if _, loaded := telegramUsedAuth.LoadOrStore(hash, now); loaded {
		return false
	}
	// Opportunistic sweep: only on store, cheap enough at login frequency.
	telegramUsedAuth.Range(func(key, value any) bool {
		if ts, ok := value.(time.Time); ok && now.Sub(ts) > telegramMaxAuthAge {
			telegramUsedAuth.Delete(key)
		}
		return true
	})
	return true
}

func TelegramBind(c *gin.Context) {
	if !common.TelegramOAuthEnabled {
		c.JSON(200, gin.H{
			"message": "管理员未开启通过 Telegram 登录以及注册",
			"success": false,
		})
		return
	}
	params := c.Request.URL.Query()
	if !checkTelegramAuthorization(params, common.TelegramBotToken) {
		c.JSON(200, gin.H{
			"message": "无效的请求",
			"success": false,
		})
		return
	}
	telegramId := params["id"][0]
	if model.IsTelegramIdAlreadyTaken(telegramId) {
		c.JSON(200, gin.H{
			"message": "该 Telegram 账户已被绑定",
			"success": false,
		})
		return
	}

	user, err := getSessionUser(c)
	if err != nil {
		c.JSON(200, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	user.TelegramId = telegramId
	if err := user.Update(false); err != nil {
		c.JSON(200, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}

	// XHR 调用（axios 跟随 302 拿到 HTML 无法解析）返回 JSON；浏览器整页
	// 跳转保持 302 到个人资料页。
	if strings.Contains(c.GetHeader("Accept"), "application/json") {
		c.JSON(200, gin.H{
			"message": "",
			"success": true,
			"data":    "bind",
		})
		return
	}
	c.Redirect(302, "/console/personal")
}

func TelegramLogin(c *gin.Context) {
	if !common.TelegramOAuthEnabled {
		c.JSON(200, gin.H{
			"message": "管理员未开启通过 Telegram 登录以及注册",
			"success": false,
		})
		return
	}
	params := c.Request.URL.Query()
	if !checkTelegramAuthorization(params, common.TelegramBotToken) {
		c.JSON(200, gin.H{
			"message": "无效的请求",
			"success": false,
		})
		return
	}

	telegramId := params["id"][0]
	user := model.User{TelegramId: telegramId}
	if err := user.FillUserByTelegramId(); err != nil {
		c.JSON(200, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	setupLogin(&user, c)
}

func checkTelegramAuthorization(params map[string][]string, token string) bool {
	// fail-closed：开关启用而 token 被清空时，HMAC 钥匙退化为 sha256("")
	// 这一公开常量，任何人可离线伪造合法签名接管绑定账户。宁可拒绝全部
	// 请求，也不接受可伪造的"验证"。
	if strings.TrimSpace(token) == "" {
		return false
	}

	// Freshness: a signed payload older than telegramMaxAuthAge is rejected
	// even with a valid HMAC (see telegramMaxAuthAge).
	if vals, ok := params["auth_date"]; ok && len(vals) > 0 {
		if ts, err := strconv.ParseInt(strings.TrimSpace(vals[0]), 10, 64); err == nil {
			age := time.Since(time.Unix(ts, 0))
			if age > telegramMaxAuthAge || age < -telegramClockSkew {
				return false
			}
		} else {
			return false
		}
	} else {
		return false
	}

	strs := []string{}
	var hash = ""
	for k, v := range params {
		if k == "hash" {
			hash = v[0]
			continue
		}
		strs = append(strs, k+"="+v[0])
	}
	sort.Strings(strs)
	var imploded = ""
	for _, s := range strs {
		if imploded != "" {
			imploded += "\n"
		}
		imploded += s
	}
	sha256hash := sha256.New()
	io.WriteString(sha256hash, token)
	hmachash := hmac.New(sha256.New, sha256hash.Sum(nil))
	io.WriteString(hmachash, imploded)
	ss := hex.EncodeToString(hmachash.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(hash), []byte(ss)) != 1 {
		return false
	}
	// One-time consumption: a valid signature is spent on first use.
	return telegramAuthOnce(hash)
}
