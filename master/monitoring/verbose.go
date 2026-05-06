package monitoring

import (
	"fmt"
	"sync"
)

// var verboseEnabled = os.Getenv("VERBOSE") == "1"
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
