package main

import (
	"flag"
	"fmt"
	"net"
	"strconv"
	"strings"
)

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

	Verbose("config", "parsing flags")

	if cfg.AdvertiseHost == "" {
		host, err := detectAdvertiseHost()
		if err != nil {
			Verbose("config", "failed to detect advertise host: "+err.Error())
			return AgentConfig{}, err
		}
		cfg.AdvertiseHost = host
		Verbose("config", "detected advertise host: "+host)
	}

	if cfg.AdvertisePort == 0 {
		port, err := portFromListenAddr(cfg.ListenAddr)
		if err != nil {
			Verbose("config", "failed to get port from listen addr: "+err.Error())
			return AgentConfig{}, err
		}
		cfg.AdvertisePort = port
	}

	if cfg.WorkerPortStart <= 0 || cfg.WorkerPortEnd < cfg.WorkerPortStart {
		err := fmt.Errorf("invalid worker port range %d-%d", cfg.WorkerPortStart, cfg.WorkerPortEnd)
		Verbose("config", err.Error())
		return AgentConfig{}, err
	}

	cfg.MasterURL = strings.TrimRight(cfg.MasterURL, "/")
	cfg.AgentID = fmt.Sprintf("agent-%s-%d", cfg.AdvertiseHost, cfg.AdvertisePort)

	Verbose("config", "agent ID: "+cfg.AgentID)
	Verbose("config", "master URL: "+cfg.MasterURL)
	Verbose("config", "listen: "+cfg.ListenAddr)
	Verbose("config", "worker port range: "+strconv.Itoa(cfg.WorkerPortStart)+"-"+strconv.Itoa(cfg.WorkerPortEnd))

	return cfg, nil
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

			if ip == nil {
				continue
			}

			return ip.String(), nil
		}
	}

	return "", fmt.Errorf("could not detect advertise host; pass --advertise-host")
}
