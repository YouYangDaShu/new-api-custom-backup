package common

import (
	"strings"

	"github.com/tidwall/gjson"
)

const upstreamResponseModelMaxLength = 200

// ResponseModel keeps the official three-name log shape. UpstreamModel is the
// final model actually sent to the upstream after mapping and conversion.
type ResponseModel struct {
	RequestedModel string `json:"requested_model"`
	UpstreamModel  string `json:"upstream_model"`
	ReturnedModel  string `json:"returned_model"`
}

func (observation *ResponseModel) Mismatch() bool {
	return observation != nil && observation.ReturnedModel != "" &&
		!upstreamModelsMatch(observation.UpstreamModel, observation.ReturnedModel)
}

// UpstreamResponseModelAudit belongs to one relay attempt. Observation of a
// malformed or absent declaration never changes the relay result.
type UpstreamResponseModelAudit struct {
	SentModel     string
	firstModel    string
	Conflict      bool
	terminalModel string
}

func (audit *UpstreamResponseModelAudit) Reset() {
	*audit = UpstreamResponseModelAudit{}
}

func (audit *UpstreamResponseModelAudit) SetSentModel(model string) {
	audit.SentModel = normalizeObservedModel(model)
}

func (audit *UpstreamResponseModelAudit) Observe(model string, terminal bool) {
	model = normalizeObservedModel(model)
	if model == "" {
		return
	}
	if current := audit.ResponseModel(); current != "" && !strings.EqualFold(current, model) {
		audit.Conflict = true
	}
	if terminal {
		audit.terminalModel = model
		return
	}
	if audit.firstModel == "" {
		audit.firstModel = model
	}
}

func (audit *UpstreamResponseModelAudit) ResponseModel() string {
	if audit == nil {
		return ""
	}
	if audit.terminalModel != "" {
		return audit.terminalModel
	}
	return audit.firstModel
}

func (audit *UpstreamResponseModelAudit) Observation(requestedModel string) *ResponseModel {
	returnedModel := audit.ResponseModel()
	if returnedModel == "" {
		return nil
	}
	return &ResponseModel{
		RequestedModel: normalizeObservedModel(requestedModel),
		UpstreamModel:  audit.SentModel,
		ReturnedModel:  returnedModel,
	}
}

func (audit *UpstreamResponseModelAudit) ObserveOpenAI(payload []byte, eventType string) {
	if strings.TrimSpace(eventType) == "" {
		eventType = firstValidJSONString(payload, "type")
	}
	audit.Observe(firstValidJSONString(payload, "response.model", "model"), isResponseTerminalEvent(eventType))
}

func (audit *UpstreamResponseModelAudit) ObserveClaude(payload []byte) {
	audit.Observe(firstValidJSONString(payload, "message.model", "model"), false)
}

func (audit *UpstreamResponseModelAudit) ObserveGemini(payload []byte) {
	audit.Observe(firstValidJSONString(payload, "modelVersion", "response.modelVersion", "response.response.modelVersion"), true)
}

func normalizeObservedModel(model string) string {
	model = strings.TrimSpace(model)
	runes := []rune(model)
	if len(runes) > upstreamResponseModelMaxLength {
		return string(runes[:upstreamResponseModelMaxLength])
	}
	return model
}

func firstValidJSONString(payload []byte, paths ...string) string {
	if !gjson.ValidBytes(payload) {
		return ""
	}
	for _, path := range paths {
		value := gjson.GetBytes(payload, path)
		if value.Exists() && value.Type == gjson.String {
			if model := strings.TrimSpace(value.String()); model != "" {
				return model
			}
		}
	}
	return ""
}

func isResponseTerminalEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
		return true
	default:
		return false
	}
}

func upstreamModelsMatch(sentModel, responseModel string) bool {
	return strings.EqualFold(strings.TrimSpace(sentModel), strings.TrimSpace(responseModel))
}
