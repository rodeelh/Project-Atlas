// Package engine manages the Engine LM subprocess — a bundled llama-server
// binary that provides a local OpenAI-compatible inference endpoint.
package engine

// EngineStatus describes the current state of the Engine LM process.
type EngineStatus struct {
	Running      bool    `json:"running"`
	LoadedModel  string  `json:"loadedModel"`
	Port         int     `json:"port"`
	BinaryReady  bool    `json:"binaryReady"`
	BuildVersion string  `json:"buildVersion,omitempty"`
	LastError    string  `json:"lastError,omitempty"`
	LastTPS       float64 `json:"lastTPS,omitempty"`      // true decode tokens/sec from /metrics
	ContextTokens int     `json:"contextTokens,omitempty"` // tokens currently in KV cache from /slots
}

// ModelInfo describes a GGUF model file stored in the models directory.
type ModelInfo struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	SizeHuman string `json:"sizeHuman"`
}
