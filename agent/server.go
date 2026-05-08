package main

import (
	"log"
	"net/http"
)

func main() {
	cfg, err := readConfig()
	if err != nil {
		log.Fatal(err)
	}

	if err := startOllama(cfg); err != nil {
		log.Fatal(err)
	}

	if err := startChroma(cfg); err != nil {
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
