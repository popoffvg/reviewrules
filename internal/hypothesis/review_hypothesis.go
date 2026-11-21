package hypothesis

// ReviewHypothesis asks a yes/no question. A yes answer indicates a defect at At.
type ReviewHypothesis struct {
	ID       string `json:"id,omitempty"`
	Slug     string `json:"slug,omitempty"`
	Commit   string `json:"commit,omitempty"`
	Hunt     string `json:"hunt"`
	At       string `json:"at"`
	Question string `json:"question"`
	Look     string `json:"look"`
}
