/*
* The LB should listen to http in some port and then should try to response for it
 */

package main

import (
	"fmt"
	"net/http"
)

func chat(w http.ResponseWriter, req *http.Request) {
    fmt.Fprintf(w, "chat\n")
}

func headers(w http.ResponseWriter, req *http.Request) {

    for name, headers := range req.Header {
        for _, h := range headers {
            fmt.Fprintf(w, "%v: %v\n", name, h)
        }
    }
}

func main() {

    http.HandleFunc("/chat", chat)

    http.ListenAndServe(":8090", nil)
} // from config.go file