package main

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

type DockerManager struct {
	// connect to docker demon
	client *client.Client
	cfg    AgentConfig
	// two go routins may allocate the same port
	mu     sync.Mutex
	next   int
}

const workerContainerPort = 50051

func NewDockerManager(cfg AgentConfig) (*DockerManager, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)

	if err != nil {
		return nil, err
	}

	return &DockerManager{
		client: cli,
		cfg:    cfg,
		next:   cfg.WorkerPortStart,
	}, nil
}

func (d *DockerManager) CreateWorker(
	req CreateWorkerRequest,
) (CreateWorkerResponse, error) {
	Verbose("docker", "creating worker with image "+d.cfg.WorkerImage)

	image := d.cfg.WorkerImage

	hostPort, err := d.allocateHostPort()
	if err != nil {
		Verbose("docker", "failed to allocate port: "+err.Error())
		return CreateWorkerResponse{}, err
	}

	workerID := fmt.Sprintf(
		"worker-%s-%d-%d",
		d.cfg.AgentID,
		hostPort,
		time.Now().UnixMilli(),
	)
	name := "llm-" + workerID
	Verbose("docker", "worker ID: "+workerID+", host port: "+strconv.Itoa(hostPort))

	_ = nat.Port(strconv.Itoa(workerContainerPort) + "/tcp")
	env := append([]string{}, req.Env...)
	env = append(
		env,
		"WORKER_ID="+workerID,
		fmt.Sprintf("WORKER_PORT=%d", hostPort),
		"MASTER_URL="+d.cfg.MasterURL,
	)
	if d.cfg.OllamaURL != "" {
		env = append(env, "OLLAMA_URL="+d.cfg.OllamaURL)
	}
	if d.cfg.ChromaURL != "" {
		env = append(env, "CHROMA_URL="+d.cfg.ChromaURL)
	}

	ctx := context.Background()

	resp, err := d.client.ContainerCreate(
		ctx,
		&container.Config{
			Image: image,
			Env:   env,
			Labels: map[string]string{
				"llm.cluster.role":      "worker",
				"llm.cluster.agent_id":  d.cfg.AgentID,
				"llm.cluster.worker_id": workerID,
				"llm.cluster.host_port": strconv.Itoa(hostPort),
			},
		},
		&container.HostConfig{
			NetworkMode: container.NetworkMode("host"),
			RestartPolicy: container.RestartPolicy{
				Name: "unless-stopped",
			},
		},
		nil,
		nil,
		name,
	)

	if err != nil {
		Verbose("docker", "failed to create container: "+err.Error())
		return CreateWorkerResponse{}, err
	}

	Verbose("docker", "container created: "+resp.ID)

	err = d.client.ContainerStart(
		ctx,
		resp.ID,
		container.StartOptions{},
	)

	if err != nil {
		Verbose("docker", "failed to start container: "+err.Error())
		_ = d.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return CreateWorkerResponse{}, err
	}

	Verbose("docker", "container started successfully")

	return CreateWorkerResponse{
		WorkerID:    workerID,
		Address:     fmt.Sprintf("%s:%d", d.cfg.AdvertiseHost, hostPort),
		HostPort:    hostPort,
		ContainerID: resp.ID,
	}, nil
}

func (d *DockerManager) allocateHostPort() (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	total := d.cfg.WorkerPortEnd - d.cfg.WorkerPortStart + 1
	for i := 0; i < total; i++ {
		port := d.next
		d.next++
		if d.next > d.cfg.WorkerPortEnd {
			d.next = d.cfg.WorkerPortStart
		}

		if isPortAvailable(port) {
			return port, nil
		}
	}

	return 0, fmt.Errorf("no free worker ports in range %d-%d", d.cfg.WorkerPortStart, d.cfg.WorkerPortEnd)
}

func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func (d *DockerManager) CleanWorkers() (int, error) {
	ctx := context.Background()

	containers, err := d.client.ContainerList(ctx, container.ListOptions{
		All: true,
	})
	if err != nil {
		return 0, err
	}

	count := 0
	for _, c := range containers {
		if c.Image == d.cfg.WorkerImage {
			Verbose("clean", "removing container "+c.ID[:12])

			err := d.client.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
			if err != nil {
				Verbose("clean", "failed to remove "+c.ID[:12]+": "+err.Error())
				continue
			}
			count++
		}
	}

	return count, nil
}
