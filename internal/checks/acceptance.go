package checks

// AcceptReviewGrowth is the accept_review of a check whose acceptance
// nats-lens ends early when the entity's week's growth changes. The API
// reads the key through internal/checkaccept.
const AcceptReviewGrowth = "growth"
