package store

// State is runs/<run_id>/state.json (FRAME: Persisted formats).
type State struct {
	RunID             string  `json:"run_id"`
	ConversationID    string  `json:"conversation_id"`
	Turn              int     `json:"turn"`
	State             string  `json:"state"` // queued|running|done|failed|cancelled|timeout|lost
	Background        bool    `json:"background"`
	AdmittedAt        string  `json:"admitted_at"`
	StartedAt         *string `json:"started_at"`
	EndedAt           *string `json:"ended_at"`
	WorkerPID         int     `json:"worker_pid"`
	ProviderPGID      int     `json:"provider_pgid"`
	ProviderStartTime *string `json:"provider_start_time"`
	ExitCode          *int    `json:"exit_code"`
	Outcome           *string `json:"outcome"`
	ErrorExcerpt      *string `json:"error_excerpt"`
	UnparsedEvents    int     `json:"unparsed_events"`
	OutputPath        string  `json:"output_path"`
	RunDir            string  `json:"run_dir"`
}

// Defaults are the first-turn settings a conversation keeps.
type Defaults struct {
	Scenario string `json:"scenario"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
	Sandbox  string `json:"sandbox"`
}

// Conversation is conversations/<conversation_id>.json.
type Conversation struct {
	ConversationID    string   `json:"conversation_id"`
	Provider          string   `json:"provider"`
	ProviderSessionID *string  `json:"provider_session_id"`
	Cwd               string   `json:"cwd"`
	Defaults          Defaults `json:"defaults"`
	Turns             []string `json:"turns"`
	ActiveRunID       *string  `json:"active_run_id"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}
