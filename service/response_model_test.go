package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendResponseModelLogInfoUsesAdminScope(t *testing.T) {
	info := &relaycommon.RelayInfo{OriginModelName: "A"}
	info.UpstreamResponseModelAudit.SetSentModel("B")
	info.UpstreamResponseModelAudit.Observe("A", true)
	other := model.NewLogOther()

	AppendResponseModelLogInfo(info, other)

	snapshot := other.Snapshot()
	assert.NotContains(t, snapshot, "response_model")
	adminInfo, ok := snapshot["admin_info"].(map[string]any)
	require.True(t, ok)
	observation, ok := adminInfo["response_model"].(*relaycommon.ResponseModel)
	require.True(t, ok)
	assert.Equal(t, "A", observation.RequestedModel)
	assert.Equal(t, "B", observation.UpstreamModel)
	assert.Equal(t, "A", observation.ReturnedModel)
	assert.True(t, observation.Mismatch())
}

func TestAppendResponseModelLogInfoSkipsPlainMatch(t *testing.T) {
	info := &relaycommon.RelayInfo{OriginModelName: "A"}
	info.UpstreamResponseModelAudit.SetSentModel("A")
	info.UpstreamResponseModelAudit.Observe("A", true)
	other := model.NewLogOther()

	AppendResponseModelLogInfo(info, other)

	assert.NotContains(t, other.Snapshot(), "admin_info")
}
