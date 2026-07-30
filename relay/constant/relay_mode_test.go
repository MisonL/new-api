package constant

import "testing"

func TestPath2RelayModeRecognizesOnlyExactResponsesCompactEndpoints(t *testing.T) {
	testCases := []struct {
		path string
		want int
	}{
		{path: "/v1/responses/compact", want: RelayModeResponsesCompact},
		{path: "/responses/compact", want: RelayModeResponsesCompact},
		{path: "/v1/responses", want: RelayModeResponses},
		{path: "/v1/responses/compact/extra", want: RelayModeResponses},
	}

	for _, testCase := range testCases {
		t.Run(testCase.path, func(t *testing.T) {
			if got := Path2RelayMode(testCase.path); got != testCase.want {
				t.Fatalf("Path2RelayMode(%q) = %d, want %d", testCase.path, got, testCase.want)
			}
		})
	}
}
