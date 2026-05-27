package queue

// ChatJob is the payload pushed to the Redis queue.
type ChatJob struct {
	ID             string `json:"id"`
	ConversationID int64  `json:"conversation_id"`
	Message        string `json:"message"`
	AdminID        int64  `json:"admin_id"`
	AdminLevel     int    `json:"admin_level"`
	AdminName      string `json:"admin_name"`
	FranchiseID    *int64 `json:"franchise_id,omitempty"`
	CreatedAt      int64  `json:"created_at"`
}
