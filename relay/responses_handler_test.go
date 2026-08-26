package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanInputItemIDsRemovesOnlyInvalidMessageID(t *testing.T) {
	input := []byte(`[
		{"type":"message","id":"item_bad","role":"assistant","content":[{"type":"output_text","text":"hello"}]},
		{"type":"message","id":"msg_good","role":"assistant","content":[{"type":"output_text","text":"kept"}]},
		{"type":"reasoning","id":"item_reasoning","summary":[]},
		{"type":"custom_tool_call","id":"item_tool","call_id":"call_1","name":"exec","input":"{}"}
	]`)

	cleaned, err := cleanInputItemIDs(input)
	require.NoError(t, err)

	var items []map[string]any
	require.NoError(t, common.Unmarshal(cleaned, &items))
	require.Len(t, items, 4)

	_, hasInvalidMessageID := items[0]["id"]
	assert.False(t, hasInvalidMessageID)
	assert.Equal(t, "msg_good", items[1]["id"])
	assert.Equal(t, "item_reasoning", items[2]["id"])
	assert.Equal(t, "item_tool", items[3]["id"])
}

func TestCleanInputItemIDsLeavesStringInputUntouched(t *testing.T) {
	input := []byte(`"hello"`)

	cleaned, err := cleanInputItemIDs(input)
	require.NoError(t, err)
	assert.JSONEq(t, string(input), string(cleaned))
}
