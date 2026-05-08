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
}

type AgentRegistrationRequest struct {
	AgentID string `json:"agent_id"`
	Address string `json:"address"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

type CreateWorkerRequest struct {
	Env []string `json:"env,omitempty"`
}

type CreateWorkerResponse struct {
	WorkerID      string `json:"worker_id"`
	Address       string `json:"address"`
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
	ContainerID   string `json:"container_id"`
}
