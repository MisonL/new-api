package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

var errSessionUserDisabled = errors.New("该用户已被禁用")

// resolveEnabledBindUser 读取会话中的当前用户并复读数据库状态。
//
// 绑定回调是公开路由（不经 UserAuth 中间件），会话 cookie 可能是账号被禁用
// 之前签发的。此前各绑定路径只读 session id 再 FillUserById，从不比对 status，
// 被禁用的账号仍可用旧会话完成第三方绑定。绑定路径必须像 UserAuth 一样复读
// 并拒绝被禁用的账号。
func resolveEnabledBindUser(c *gin.Context) (*model.User, error) {
	session := sessions.Default(c)
	idRaw := session.Get("id")
	if idRaw == nil {
		return nil, errors.New("missing current user session")
	}
	id, ok := idRaw.(int)
	if !ok || id <= 0 {
		return nil, errors.New("invalid session user")
	}
	user := &model.User{Id: id}
	if err := user.FillUserById(); err != nil {
		return nil, err
	}
	if user.Status != common.UserStatusEnabled {
		return nil, errSessionUserDisabled
	}
	// 会话世代落后说明密码已改/重置：绑定回调不走 UserAuth，必须在这里
	// 用同一套判定拒绝，否则被吊销的会话能继续绑定第三方账号接管账户。
	if middleware.SessionRevokedByEpoch(session, user.SessionEpoch) {
		return nil, errSessionRevokedByPasswordChange
	}
	return user, nil
}

var errSessionRevokedByPasswordChange = errors.New("会话已失效，请重新登录")
