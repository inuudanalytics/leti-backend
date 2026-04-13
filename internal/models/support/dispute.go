package support

// DisputeEvidence holds a single uploaded image reference.
type DisputeEvidence struct {
	URL      string `json:"url"`
	PublicID string `json:"public_id"`
}

// DisputeDecisionRequest is the body for admin resolve endpoints.
type DisputeDecisionRequest struct {
	// Action must be one of: refund_full, release_full, refund_partial, dismiss
	Action     string  `json:"action"`
	Amount     float64 `json:"amount,omitempty"` // required for refund_partial
	AdminNotes string  `json:"admin_notes,omitempty"`
	Resolution string  `json:"resolution,omitempty"`
}
