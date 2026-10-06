// Package optimize orchestrates arbitrary external commands. It does not own
// browser semantics: each trial uses the ordinary Mimic executable and CDP.
package optimize

import (
	"os/exec"
	"sync"
	"time"
)

type process struct {
	cmd      *exec.Cmd
	done     chan struct{}
	err      error
	platform processPlatform
	once     sync.Once
}

func startProcess(cmd *exec.Cmd) (*process, error) {
	p := &process{cmd: cmd, done: make(chan struct{})}
	if err := p.platform.start(cmd); err != nil {
		return nil, err
	}
	go func() { p.err = cmd.Wait(); close(p.done) }()
	return p, nil
}

func (p *process) stop() {
	p.once.Do(func() {
		p.platform.stop(p.cmd)
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			_ = p.cmd.Process.Kill()
		}
		p.platform.close()
	})
}

func (p *process) stats() (float64, uint64) { return p.platform.stats(p.cmd) }
