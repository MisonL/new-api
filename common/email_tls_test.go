package common

import (
	"crypto/tls"
	"os"
	"strings"
	"testing"
)

// 隐式 TLS（465）分支必须校验服务端证书。此前的实现无条件
// InsecureSkipVerify: true，任何能位于网关与 SMTP 服务器之间的人出示
// 任意证书即可读到密码重置链接与邮箱验证码。
func TestSMTPImplicitTLSTrustsTheServer(t *testing.T) {
	cfg := smtpImplicitTLSConfig("smtp.example.com", false)
	if cfg.InsecureSkipVerify {
		t.Fatalf("implicit TLS must verify the SMTP server certificate")
	}
	if cfg.ServerName != "smtp.example.com" {
		t.Fatalf("ServerName = %q, want smtp.example.com", cfg.ServerName)
	}
	if cfg.MinVersion < tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x, want at least TLS 1.2", cfg.MinVersion)
	}
}

// 显式跳过校验时必须由运维开关驱动，而不是默认行为。
func TestSMTPImplicitTLSHonorsExplicitSkip(t *testing.T) {
	cfg := smtpImplicitTLSConfig("smtp.example.com", true)
	if !cfg.InsecureSkipVerify {
		t.Fatalf("an explicit skip must be honoured, otherwise the option is a no-op")
	}
}

// 光有正确的构造函数还不够：SendEmail 必须真的用它，否则回到
// InsecureSkipVerify: true 也不会被前两个测试发现。
func TestSendEmailUsesTheVerifyingTLSConfig(t *testing.T) {
	src, err := os.ReadFile("email.go")
	if err != nil {
		t.Fatalf("read email.go: %v", err)
	}
	body := string(src)
	if strings.Contains(body, "InsecureSkipVerify: true") {
		t.Fatalf("email.go still hardcodes InsecureSkipVerify: true on the implicit TLS path")
	}
	if !strings.Contains(body, "smtpImplicitTLSConfig(SMTPServer, SMTPSkipTLSVerify)") {
		t.Fatalf("the implicit TLS path must build its config through smtpImplicitTLSConfig")
	}
}
