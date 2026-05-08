package main

import (
	"fmt"
	"sync"
)

var verboseEnabled = true

var verboseMu sync.Mutex

func Verbose(place, msg string) {
	if !verboseEnabled {
		return
	}
	verboseMu.Lock()
	defer verboseMu.Unlock()
	fmt.Printf("[%s] %s\n", place, msg)
}