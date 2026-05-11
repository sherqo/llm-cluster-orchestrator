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
	flag.StringVar(&cfg.ListenAddr, "listen", ":9000", "agent listen address")
	flag.StringVar(&cfg.AdvertiseHost, "advertise-host", "", "LAN host/IP the master can use to reach this agent")
	flag.IntVar(&cfg.AdvertisePort, "advertise-port", 0, "LAN port the master can use to reach this agent")
	flag.StringVar(&cfg.WorkerImage, "worker-image", "llm-worker:latest", "Docker image to use for worker containers")
	flag.IntVar(&cfg.WorkerPortStart, "worker-port-start", 50051, "first host port to publish worker gRPC")
	flag.IntVar(&cfg.WorkerPortEnd, "worker-port-end", 50150, "last host port to publish worker gRPC")
	flag.StringVar(&cfg.OllamaURL, "ollama-url", "http://127.0.0.1:11434", "shared Ollama base URL")
	flag.StringVar(&cfg.ChromaURL, "chroma-url", "http://127.0.0.1:8000", "shared Chroma base URL")
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

	if nextPort, ok := findAvailablePort(cfg.ListenAddr, cfg.AdvertisePort, cfg.AdvertisePort+20); ok {
		if nextPort != cfg.AdvertisePort {
			cfg.ListenAddr = replaceListenPort(cfg.ListenAddr, nextPort)
			cfg.AdvertisePort = nextPort
			Verbose("config", fmt.Sprintf("listen port in use, switched to %d", nextPort))
		}
	} else {
		return AgentConfig{}, fmt.Errorf("no free listen port in range %d-%d", cfg.AdvertisePort, cfg.AdvertisePort+20)
	}

	if cfg.WorkerPortStart <= 0 || cfg.WorkerPortEnd < cfg.WorkerPortStart {
		err := fmt.Errorf("invalid worker port range %d-%d", cfg.WorkerPortStart, cfg.WorkerPortEnd)
		Verbose("config", err.Error())
		return AgentConfig{}, err
	}

	if cfg.MasterURL == "" {
		err := fmt.Errorf("master url is required; pass --master-url")
		Verbose("config", err.Error())
		return AgentConfig{}, err
	}

	cfg.MasterURL = strings.TrimRight(cfg.MasterURL, "/")
	cfg.AgentID = fmt.Sprintf("agent-%s-%d", cfg.AdvertiseHost, cfg.AdvertisePort)

	Verbose("config", "agent ID: "+cfg.AgentID)
	Verbose("config", "master URL: "+cfg.MasterURL)
	Verbose("config", "listen: "+cfg.ListenAddr)
	Verbose("config", "worker port range: "+strconv.Itoa(cfg.WorkerPortStart)+"-"+strconv.Itoa(cfg.WorkerPortEnd))
	Verbose("config", "ollama url: "+cfg.OllamaURL)
	Verbose("config", "chroma url: "+cfg.ChromaURL)

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

func replaceListenPort(addr string, port int) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Sprintf(":%d", port)
	}
	if host == "" {
		return fmt.Sprintf(":%d", port)
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func findAvailablePort(listenAddr string, start, end int) (int, bool) {
	host, _, err := net.SplitHostPort(listenAddr)
	if err != nil {
		if strings.HasPrefix(listenAddr, ":") {
			host = ""
		} else {
			host = listenAddr
		}
	}
	if host == "" {
		host = "0.0.0.0"
	}

	for p := start; p <= end; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(p)))
		if err != nil {
			continue
		}
		_ = ln.Close()
		return p, true
	}
	return 0, false
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
