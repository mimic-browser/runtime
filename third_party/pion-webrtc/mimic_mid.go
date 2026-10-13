// SPDX-License-Identifier: MIT

package webrtc

import "strconv"

func (pc *PeerConnection) unmatchedDataMid(mediaCount int) string {
	if !pc.api.settingEngine.reserveUnappliedMIDs {
		return strconv.Itoa(mediaCount)
	}
	if pc.dataMid == "" || (pc.currentLocalDescription == nil && pc.pendingLocalDescription == nil &&
		pc.currentRemoteDescription == nil && pc.pendingRemoteDescription == nil) {
		pc.greaterMid++
		pc.dataMid = strconv.Itoa(pc.greaterMid)
	}
	return pc.dataMid
}
