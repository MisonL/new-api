package dto

import "testing"

func TestGetToolsMalformedDoesNotPanic(t *testing.T) {
	cases := [][]byte{[]byte("[{bad"), []byte("{bad")}
	for _, raw := range cases {
		req := &GeminiChatRequest{Tools: raw}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("GetTools(%q) panicked: %v", raw, r)
				}
			}()
			got := req.GetTools()
			if got != nil {
				t.Fatalf("GetTools(%q) = %v, want nil", raw, got)
			}
		}()
	}
}
