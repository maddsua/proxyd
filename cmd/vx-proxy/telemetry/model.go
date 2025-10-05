package telemetry

type ModelTelemetryState struct {
	RunID  string          `json:"run_id"`
	Uptime int64           `json:"uptime_s"`
	Auth   ModelAuthStatus `json:"auth"`
}

type ModelAuthStatus struct {
	Type      string   `json:"type"`
	ErrorRate *float64 `json:"error_rate"`
}
