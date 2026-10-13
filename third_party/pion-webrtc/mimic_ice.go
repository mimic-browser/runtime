// SPDX-License-Identifier: MIT

package webrtc

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/pion/sdp/v3"
)

func newStagedICEParameters() (ICEParameters, error) {
	// Frozen Chrome generates a four-character ufrag and a 24-character
	// password. These independently chosen credentials are installed into the
	// real ICE agent; they are not a presentation rewrite.
	var entropy [21]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return ICEParameters{}, err
	}
	return ICEParameters{UsernameFragment: base64.RawStdEncoding.EncodeToString(entropy[:3]), Password: base64.RawStdEncoding.EncodeToString(entropy[3:])}, nil
}

// These are the credentials the native ICE agent will install when this offer
// is applied. Merely generating an offer must not reset an established agent.
func stageICEParameters(description *sdp.SessionDescription, parameters ICEParameters) {
	for _, media := range description.MediaDescriptions {
		attributes := media.Attributes[:0]
		for _, attribute := range media.Attributes {
			switch attribute.Key {
			case "ice-ufrag":
				attribute.Value = parameters.UsernameFragment
			case "ice-pwd":
				attribute.Value = parameters.Password
			case "candidate", "end-of-candidates":
				continue
			}
			attributes = append(attributes, attribute)
		}
		media.Attributes = attributes
	}
}

func (transport *ICETransport) restartWithParameters(parameters ICEParameters) error {
	transport.lock.Lock()
	defer transport.lock.Unlock()
	agent := transport.gatherer.getAgent()
	if agent == nil {
		return fmt.Errorf("%w: unable to restart ICETransport", errICEAgentNotExist)
	}
	// Gathering remains in SetLocalDescription, after signaling is accepted.
	if err := agent.Restart(parameters.UsernameFragment, parameters.Password); err != nil {
		return err
	}
	// No author-visible "new" event: the pending generation has not gathered
	// yet, and its real Gather call will publish the gathering transition.
	atomicStoreICEGathererState(&transport.gatherer.state, ICEGathererStateNew)
	return nil
}
