package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetWebSearchPricePerThousand_UsesPreviewOverrides(t *testing.T) {
	require.Equal(t, 25.0, GetWebSearchPricePerThousand("gpt-4o-search-preview", "medium"))
	require.Equal(t, 10.0, GetWebSearchPricePerThousand("gpt-5-search-api", "high"))
}

func TestGetClaudeAndFileSearchPricePerThousand_Defaults(t *testing.T) {
	require.Equal(t, 10.0, GetClaudeWebSearchPricePerThousand())
	require.Equal(t, 2.5, GetFileSearchPricePerThousand())
}

func TestValidateToolPriceJSON(t *testing.T) {
	require.NoError(t, ValidateToolPriceJSON(`{"web_search":10,"web_search_preview:gpt-4o*":25}`))
	require.Error(t, ValidateToolPriceJSON(`{"web_search":-1}`))
	require.Error(t, ValidateToolPriceJSON(`{"":10}`))
}
