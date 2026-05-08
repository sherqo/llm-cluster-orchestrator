/*
* The LB should listen to http in some port and then should try to response for it
* the request flow should be:
  * client calls the LB via HTTP
	* the LB should assign an id to this request and push the request to the DB non-blockingly
	* then the LB should figure out what worker node withh take this request
	* then it should add this info to the in-memory registry (worker, reqeustId)
	* the assigned worker node should finish the work and send back to the LB
	* the LB then needs to figure out how to send it back via the requestId and also save the response to the DB
	* and remove the requestId from the in-memory registry

* the previous flow is for the normal case, but we also need to consider the failure cases:
*/

package main

import (
	"log"
	"os"
	"strings"

	lib "master/lib"
	"master/monitoring"
	"master/tui"
)

func main() {
	monitoring.SetStdoutEnabled(false)
	monitoring.SetVerboseEnabled(true)

	router := lib.NewRouter() // create new LB and add routers to it

	// manually for now
	router.AddWorker("localhost:50051")
	router.AddWorker("localhost:50052")
	router.AddWorker("localhost:50053")

	if seed := strings.TrimSpace(os.Getenv("MASTER_WORKERS")); seed != "" {
		for _, addr := range strings.Split(seed, ",") {
			a := strings.TrimSpace(addr)
			if a == "" {
				continue
			}
			if err := router.AddWorker(a); err != nil {
				log.Printf("failed to add seeded worker %s: %v", a, err)
			}
		}
	}

	go lib.Serve(router)

	if err := tui.Run(router); err != nil {
		log.Fatal(err)
	}
}
