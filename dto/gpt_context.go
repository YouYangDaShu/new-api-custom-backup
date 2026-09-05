package dto

// GPTContextMode controls long-context handling for a billing group.
type GPTContextMode string

const (
	GPTContextModeStandard    GPTContextMode = "standard"
	GPTContextModeLongContext GPTContextMode = "gpt_long_context"
)

func (m GPTContextMode) Valid() bool {
	return m == "" || m == GPTContextModeStandard || m == GPTContextModeLongContext
}

func (m GPTContextMode) Normalized() GPTContextMode {
	if m == GPTContextModeLongContext {
		return GPTContextModeLongContext
	}
	return GPTContextModeStandard
}
