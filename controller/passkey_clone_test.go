package controller

import (
	"testing"

	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
)

// 签名计数不递增时 go-webauthn 只设置 CloneWarning，不返回错误。一份被复制
// 出来的私钥因此可以通过认证；调用方必须显式拒绝，否则克隆凭证永久有效。
func TestPasskeyCredentialIsCloned(t *testing.T) {
	cloned := &webauthnlib.Credential{}
	cloned.Authenticator.CloneWarning = true
	if !passkeyCredentialIsCloned(cloned) {
		t.Fatalf("a credential with CloneWarning must be treated as cloned")
	}

	normal := &webauthnlib.Credential{}
	normal.Authenticator.SignCount = 7
	if passkeyCredentialIsCloned(normal) {
		t.Fatalf("a normal credential must not be treated as cloned")
	}

	if passkeyCredentialIsCloned(nil) {
		t.Fatalf("nil credential must not be reported as cloned")
	}
}
