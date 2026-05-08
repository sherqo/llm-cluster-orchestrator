package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func main() {
	cfg, err := readConfig()
	if err != nil {
		log.Fatal(err)
	}

	docker, err := NewDockerManager(cfg)
	if err != nil {
		log.Fatal(err)
	}

	if cfg.MasterURL != "" {
		if err := RegisterWithMaster(cfg); err != nil {
			log.Fatal(err)
		}
	}

	http.HandleFunc(
		"/system/info",
		SystemInfoHandler,
	)

	http.HandleFunc(
		"/workers/create",
		CreateWorkerHandler(docker),
	)

	log.Println("agent listening on " + cfg.ListenAddr)

	err = http.ListenAndServe(cfg.ListenAddr, nil)
	if err != nil {
		log.Fatal(err)
	}
}

func readConfig() (AgentConfig, error) {
	var cfg AgentConfig

	flag.StringVar(&cfg.MasterURL, "master-url", "", "master HTTP base URL")
	flag.StringVar(&cfg.ListenAddr, "listen", ":8080", "agent listen address")
	flag.StringVar(&cfg.AdvertiseHost, "advertise-host", "", "LAN host/IP the master can use to reach this agent")
	flag.IntVar(&cfg.AdvertisePort, "advertise-port", 0, "LAN port the master can use to reach this agent")
	flag.StringVar(&cfg.WorkerImage, "worker-image", "llm-worker:latest", "Docker image to use for worker containers")
	flag.IntVar(&cfg.WorkerPortStart, "worker-port-start", 50051, "first host port to publish worker gRPC")
	flag.IntVar(&cfg.WorkerPortEnd, "worker-port-end", 50150, "last host port to publish worker gRPC")
	flag.Parse()

	if cfg.AdvertiseHost == "" {
		host, err := detectAdvertiseHost()
		if err != nil {
			return AgentConfig{}, err
		}
		cfg.AdvertiseHost = host
	}

	if cfg.AdvertisePort == 0 {
		port, err := portFromListenAddr(cfg.ListenAddr)
		if err != nil {
			return AgentConfig{}, err
		}
		cfg.AdvertisePort = port
	}

	if cfg.WorkerPortStart <= 0 || cfg.WorkerPortEnd < cfg.WorkerPortStart {
		return AgentConfig{}, fmt.Errorf("invalid worker port range %d-%d", cfg.WorkerPortStart, cfg.WorkerPortEnd)
	}

	cfg.MasterURL = strings.TrimRight(cfg.MasterURL, "/")
	cfg.AgentID = fmt.Sprintf("agent-%s-%d", cfg.AdvertiseHost, cfg.AdvertisePort)
	return cfg, nil
}

func RegisterWithMaster(cfg AgentConfig) error {
	body, err := json.Marshal(AgentRegistrationRequest{
		AgentID: cfg.AgentID,
		Address: fmt.Sprintf(
			"%s:%d",
			cfg.AdvertiseHost,
			cfg.AdvertisePort,
		),
		Host: cfg.AdvertiseHost,
		Port: cfg.AdvertisePort,
	})
	if err != nil {
		return err
	}

	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(
		cfg.MasterURL+"/agents/register",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("master registration failed with status %d", resp.StatusCode)
	}

	log.Printf("registered agent %s with master %s", cfg.AgentID, cfg.MasterURL)
	return nil
}

func portFromListenAddr(addr string) (int, error) {
	_, portText, err := net.SplitHostPort(addr)
	if err != nil {
		portText = strings.TrimPrefix(addr, ":")
	}

	port, err := strconv.Atoi(portText)
	if err != nil {
		return 0, err
	}
	return port, nil
}

func detectAdvertiseHost() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}

			ip = ip.To4()
			if ip == nil {
				continue
			}

			return ip.String(), nil
		}
	}

	return "", fmt.Errorf("could not detect advertise host; pass --advertise-host")
}
