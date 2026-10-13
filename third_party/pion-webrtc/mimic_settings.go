// SPDX-License-Identifier: MIT

package webrtc

import "github.com/pion/sdp/v3"

// SetSDPGenerator installs a final native SDP generation step, before
// serialization and before the description becomes the accepted local SDP.
// It must preserve the negotiated transport and media content.
func (e *SettingEngine) SetSDPGenerator(generator func(*sdp.SessionDescription) error) {
	e.sdpGenerator = generator
}

// SetReserveUnappliedMIDs allocates fresh native MIDs for repeated initial
// offers before any local or remote description has been applied.
func (e *SettingEngine) SetReserveUnappliedMIDs(enabled bool) {
	e.reserveUnappliedMIDs = enabled
}

// SetSkipGatheringWithoutMedia avoids starting ICE for empty descriptions.
func (e *SettingEngine) SetSkipGatheringWithoutMedia(enabled bool) {
	e.skipGatheringWithoutMedia = enabled
}

// SetDataDescriptionOperations supports replacing pending local offers and
// data-only rollback where the active ICE transport remains unchanged.
func (e *SettingEngine) SetDataDescriptionOperations(enabled bool) {
	e.dataDescriptionOperations = enabled
}
