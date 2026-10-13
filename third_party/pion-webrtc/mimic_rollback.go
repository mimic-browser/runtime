// SPDX-License-Identifier: MIT

package webrtc

import (
	"errors"

	"github.com/pion/webrtc/v4/pkg/rtcerr"
)

// rollbackDataDescription deliberately rejects media rollback and rollback of
// an ICE restart: these require restoring additional negotiated media state or
// preserving an old ICE agent. Rejection precedes every state mutation.
func (pc *PeerConnection) rollbackDataDescription(description SessionDescription) error {
	pc.mu.Lock()
	state := pc.SignalingState()
	if state == SignalingStateStable || state == SignalingStateClosed {
		pc.mu.Unlock()
		return &rtcerr.InvalidStateError{Err: errSignalingStateCannotRollback}
	}
	if description.SDP != "" {
		pc.mu.Unlock()
		return &rtcerr.InvalidAccessError{Err: errors.New("rollback description must have empty SDP")}
	}
	for _, description := range []*SessionDescription{pc.pendingLocalDescription, pc.pendingRemoteDescription} {
		if description == nil {
			continue
		}
		for _, media := range description.parsed.MediaDescriptions {
			if media.MediaName.Media != "application" {
				pc.mu.Unlock()
				return &rtcerr.NotSupportedError{Err: errors.New("media rollback requires negotiated codec and transceiver restoration")}
			}
		}
	}
	if current := pc.currentLocalDescription; current != nil {
		parameters, err := pc.iceGatherer.GetLocalParameters()
		if err != nil {
			pc.mu.Unlock()
			return err
		}
		for _, media := range current.parsed.MediaDescriptions {
			ufrag, _ := media.Attribute("ice-ufrag")
			password, _ := media.Attribute("ice-pwd")
			if ufrag != parameters.UsernameFragment || password != parameters.Password {
				pc.mu.Unlock()
				return &rtcerr.NotSupportedError{Err: errors.New("ICE restart rollback requires preserving the previous ICE transport")}
			}
		}
	}
	pc.pendingLocalDescription = nil
	pc.pendingRemoteDescription = nil
	pc.stagedICEParameters = nil
	if pc.currentLocalDescription == nil && pc.currentRemoteDescription == nil {
		pc.dataMid = ""
	}
	pc.signalingState.Set(SignalingStateStable)
	pc.isNegotiationNeeded.Store(false)
	pc.onNegotiationNeeded()
	pc.mu.Unlock()
	pc.onSignalingStateChange(SignalingStateStable)
	return nil
}
