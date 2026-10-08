package router

import "testing"

func TestShouldReturnRelayNotFound(t *testing.T) {
	tests := []struct {
		name        string
		requestURI  string
		requestPath string
		want        bool
	}{
		{
			name:        "api path",
			requestURI:  "/api/status",
			requestPath: "/api/status",
			want:        true,
		},
		{
			name:        "relay path",
			requestURI:  "/v1/chat/completions",
			requestPath: "/v1/chat/completions",
			want:        true,
		},
		{
			name:        "classic asset chunk",
			requestURI:  "/assets/chunk.js?version=old",
			requestPath: "/assets/chunk.js",
			want:        true,
		},
		{
			name:        "default static chunk",
			requestURI:  "/static/js/chunk.js",
			requestPath: "/static/js/chunk.js",
			want:        true,
		},
		{
			name:        "known root static file",
			requestURI:  "/logo.png",
			requestPath: "/logo.png",
			want:        true,
		},
		{
			name:        "missing root image",
			requestURI:  "/logo-missing.png",
			requestPath: "/logo-missing.png",
			want:        true,
		},
		{
			name:        "missing root script",
			requestURI:  "/missing.js",
			requestPath: "/missing.js",
			want:        true,
		},
		{
			name:        "missing root json",
			requestURI:  "/manifest-missing.json",
			requestPath: "/manifest-missing.json",
			want:        true,
		},
		{
			name:        "missing root mp4",
			requestURI:  "/preview.mp4",
			requestPath: "/preview.mp4",
			want:        true,
		},
		{
			name:        "missing root webm",
			requestURI:  "/preview.webm",
			requestPath: "/preview.webm",
			want:        true,
		},
		{
			name:        "browser well-known probe",
			requestURI:  "/.well-known/appspecific/com.chrome.devtools.json",
			requestPath: "/.well-known/appspecific/com.chrome.devtools.json",
			want:        true,
		},
		{
			name:        "spa page",
			requestURI:  "/console/midjourney",
			requestPath: "/console/midjourney",
			want:        false,
		},
		{
			name:        "nested static-looking spa page",
			requestURI:  "/console/usage.js",
			requestPath: "/console/usage.js",
			want:        false,
		},
		{
			name:        "leading double slash api path",
			requestURI:  "//api/status",
			requestPath: "//api/status",
			want:        true,
		},
		{
			name:        "leading double slash relay path",
			requestURI:  "//v1/chat/completions",
			requestPath: "//v1/chat/completions",
			want:        true,
		},
		{
			name:        "leading double slash spa page",
			requestURI:  "//console/midjourney",
			requestPath: "//console/midjourney",
			want:        false,
		},
		{
			name:        "leading dot slash api path",
			requestURI:  "/./api/status",
			requestPath: "/./api/status",
			want:        true,
		},
		{
			name:        "uppercase api path",
			requestURI:  "/API/status",
			requestPath: "/API/status",
			// Routes are case-sensitive, so "/API/status" is not an API path;
			// it stays on the SPA fallback by design.
			want: false,
		},
		{
			name:        "encoded slash api path",
			requestURI:  "/%2f%2fapi/status",
			requestPath: "//api/status",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldReturnRelayNotFound(tt.requestURI, tt.requestPath)
			if got != tt.want {
				t.Fatalf("shouldReturnRelayNotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}
