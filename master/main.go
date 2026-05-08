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
	"fmt"

	"master/lib"
)

func main() {
	router := lib.NewRouter()

	if router.WorkerCount() == 0 {
		fmt.Println("no workers available at startup")
	}

	lib.Serve(router)
}

