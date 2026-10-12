package browser

import _ "embed"

// Captured before author scripts. Sessions retain private object tables;
// passive inspection must never execute author accessors or Proxy traps.
//go:embed debugger_intrinsics.js
var debuggerFactorySource string
