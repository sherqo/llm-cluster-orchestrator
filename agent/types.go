package main

type AgentConfig struct {
	MasterURL       string
	ListenAddr      string
	AdvertiseHost   string
	AdvertisePort   int
	AgentID         string
	WorkerImage     string
	WorkerPortStart int
	WorkerPortEnd   int
	OllamaURL       string
	ChromaURL       string
}

type AgentRegistrationRequest struct {
	AgentID string `json:"agent_id"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

type CreateWorkerRequest struct {
	Env         []string `json:"env,omitempty"`
	CallbackURL string   `json:"callback_url,omitempty"` // master endpoint to POST when worker is ready
	AgentID     string   `json:"agent_id,omitempty"`     // agent ID to echo back in callback
}

type CreateWorkerResponse struct {
	WorkerID    string `json:"worker_id"`
	Address     string `json:"address"`
	HostPort    int    `json:"host_port"`
	ContainerID string `json:"container_id"`
}
