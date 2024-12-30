package types

// For getStatus
type MinerStatus string

const (
	StatusUp       MinerStatus = "Up"
	StatusDegraded MinerStatus = "Degraded"
	StatusDown     MinerStatus = "Down"
)

type MinerIssue string

const (
	IssueOffline     MinerIssue = "Offline"
	IssueHighLatency MinerIssue = "HighLatency"
	// ...any other issues you want to define
)

// IMinerStatus is the shape of the response for getStatus.
type IMinerStatus struct {
	Status MinerStatus  `json:"status"`
	Issues []MinerIssue `json:"issues"`
	Logs   interface{}  `json:"logs"` // can be map[string]interface{} if you want
}

// For getLatestRewards & getAllRewards
type IAbstractReward struct {
	Date        string  `json:"date"`        // e.g. "2024-09-25T00:00:00Z"
	Amount      float64 `json:"amount"`      // numeric reward
	TokenSymbol string  `json:"tokenSymbol"` // e.g. "HNT", "SOL", etc.
}
