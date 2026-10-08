//go:build !windows

package mimicmain

import (
	"os"
	"time"
)

// Capture at process initialization, before potentially expensive browser setup.
// Polling the process parent avoids thread-scoped Linux PDEATHSIG semantics in
// Go and works without a wrapper daemon or inherited shell.
var initialParentPID = os.Getppid()

func parentExitSignal() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			if os.Getppid() != initialParentPID {
				close(done)
				return
			}
			<-ticker.C
		}
	}()
	return done
}
