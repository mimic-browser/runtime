//go:build (windows || linux) && amd64

package v8

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

var inspectorOwnerSequence atomic.Uint64

type inspectorPauseClient struct {
	runtime *Runtime
	running bool // actor-thread only
}

func (c *inspectorPauseClient) RunMessageLoopOnPause(_ int32) {
	c.running = true
	for c.running {
		command := <-c.runtime.commands
		if command.outerOnly {
			c.runtime.deferred = append(c.runtime.deferred, command)
			continue
		}
		command.run(c.runtime.actorState)
	}
}

func (c *inspectorPauseClient) QuitMessageLoopOnPause() { c.running = false }

type inspectorChannel struct {
	notify    func([]byte)
	responses map[int32][]byte // actor-thread only
}

func (c *inspectorChannel) SendResponse(id int32, message *gov8.InspectorStringBuffer) {
	c.responses[id] = []byte(message.StringView().String())
}

func (c *inspectorChannel) SendNotification(message *gov8.InspectorStringBuffer) {
	if c.notify != nil {
		c.notify([]byte(message.StringView().String()))
	}
}

func (*inspectorChannel) FlushProtocolNotifications() {}

type inspectorSession struct {
	runtime *Runtime
	mu      sync.Mutex
	closed  bool
	native  *gov8.InspectorSession // actor-thread only
	channel *inspectorChannel
}

func (a *adapter) InspectorOwnerID() uint64 { return a.owner.inspectorOwnerID }

func (a *adapter) ConnectInspector(notify func([]byte)) (engine.InspectorSession, error) {
	result := &inspectorSession{runtime: a.owner, channel: &inspectorChannel{notify: notify, responses: make(map[int32][]byte)}}
	_, err := a.owner.execute(func(s *state) response {
		if s.inspector == nil {
			client := &inspectorPauseClient{runtime: a.owner}
			inspector, createErr := gov8.NewInspectorWithClient(s.isolate, client)
			if createErr != nil {
				return response{err: createErr}
			}
			empty := gov8.EmptyInspectorStringView()
			for _, realm := range s.realms {
				if createErr = inspector.ContextCreated(realm, 1, empty, empty); createErr != nil {
					for _, registered := range s.realms {
						_ = inspector.ContextDestroyed(registered)
					}
					_ = inspector.Close()
					return response{err: createErr}
				}
			}
			s.inspector = inspector
			s.inspectorClient = client
			s.inspectorSessions = make(map[*inspectorSession]struct{})
		}
		native, connectErr := s.inspector.Connect(1, result.channel, gov8.EmptyInspectorStringView(), gov8.InspectorFullyTrusted)
		if connectErr != nil {
			return response{err: connectErr}
		}
		result.native = native
		s.inspectorSessions[result] = struct{}{}
		return response{}
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *inspectorSession) DispatchInspector(message []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("V8 inspector session is closed")
	}
	var envelope struct {
		ID int32 `json:"id"`
	}
	if err := json.Unmarshal(message, &envelope); err != nil || envelope.ID == 0 {
		return nil, fmt.Errorf("invalid inspector command: %w", err)
	}
	value, err := s.runtime.execute(func(_ *state) response {
		delete(s.channel.responses, envelope.ID)
		if dispatchErr := s.native.DispatchProtocolMessage(gov8.NewInspectorStringView8(message)); dispatchErr != nil {
			return response{err: dispatchErr}
		}
		payload := s.channel.responses[envelope.ID]
		if len(payload) == 0 {
			return response{err: fmt.Errorf("V8 inspector returned no response for command %d", envelope.ID)}
		}
		delete(s.channel.responses, envelope.ID)
		return response{value: append([]byte(nil), payload...)}
	})
	if err != nil {
		return nil, err
	}
	return value.([]byte), nil
}

func (s *inspectorSession) CloseInspector() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	_, err := s.runtime.execute(func(state *state) response {
		if s.native != nil {
			if closeErr := s.native.Close(); closeErr != nil {
				return response{err: closeErr}
			}
			s.native = nil
		}
		delete(state.inspectorSessions, s)
		if len(state.inspectorSessions) == 0 && state.inspector != nil {
			for _, realm := range state.realms {
				if closeErr := state.inspector.ContextDestroyed(realm); closeErr != nil {
					return response{err: closeErr}
				}
			}
			if closeErr := state.inspector.Close(); closeErr != nil {
				return response{err: closeErr}
			}
			state.inspector = nil
			state.inspectorClient = nil
		}
		return response{}
	})
	if err == nil {
		s.closed = true
	}
	return err
}

func closeInspectorState(s *state) error {
	if s.inspector == nil {
		return nil
	}
	var result error
	for session := range s.inspectorSessions {
		if session.native != nil {
			result = errors.Join(result, session.native.Close())
			session.native = nil
		}
		session.closed = true
		delete(s.inspectorSessions, session)
	}
	for _, realm := range s.realms {
		result = errors.Join(result, s.inspector.ContextDestroyed(realm))
	}
	result = errors.Join(result, s.inspector.Close())
	s.inspector = nil
	s.inspectorClient = nil
	return result
}
