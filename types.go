package systemone

// QuestionType selects the kind of answer a question returns.
type QuestionType string

// Question types accepted by the System One API.
const (
	TypeChoice QuestionType = "choice"
	TypeScore  QuestionType = "score"
	TypeNoul   QuestionType = "noul"
)

// Request asks every question about State, which may be a string, object, or array.
type Request struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}

// Question is one typed question; Choice, Score, and Noul build the common forms.
type Question struct {
	Type         QuestionType `json:"type"`
	Instructions any          `json:"instructions"`
	Criteria     any          `json:"criteria,omitempty"`
}

// Choice returns a question that picks one key of criteria, each mapped to when it applies.
func Choice(instructions any, criteria map[string]any) Question {
	return Question{Type: TypeChoice, Instructions: instructions, Criteria: criteria}
}

// Score returns a question that rates the state on levels ordered from lowest to highest.
func Score(instructions any, levels ...any) Question {
	return Question{Type: TypeScore, Instructions: instructions, Criteria: levels}
}

// Noul returns a yes/no question; optional "true" and "false" descriptions go in Criteria.
func Noul(instructions any) Question {
	return Question{Type: TypeNoul, Instructions: instructions}
}

// Response holds one Answer per question id.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// Usage reports the tokens a request consumed.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Answer is the result of one question; only the fields its Type produces are set.
type Answer struct {
	Type          QuestionType       `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]any     `json:"legend,omitempty"` // levels echoed as sent, so they may be objects
	Confidence    float64            `json:"confidence,omitempty"`
}
