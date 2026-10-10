package generated

import (
	_ "embed"
	"encoding/json"
	"sync"

	"github.com/moreveal/mimic/compatibility"
)

// Window exposure is captured from the exact browser named by target.json.
// It deliberately lives outside bundle_data.go because that file is replaced
// by the IDL/CDP generator.

//go:embed window-secure.json
var windowSecureJSON []byte

// Frozen headful Chrome 152 secure Window own-key order. The descriptor
// capture is alphabetized and cannot encode installation order.
//
//go:embed window-secure-order.json
var windowSecureOrderJSON []byte

// Separate observed member order; the descriptor capture remains alphabetized.
//
//go:embed window-secure-member-order.json
var windowSecureMemberOrderJSON []byte

//go:embed window-insecure-order.json
var windowInsecureOrderJSON []byte

//go:embed window-secure-isolated-order.json
var windowSecureIsolatedOrderJSON []byte

//go:embed window-secure-isolated.json
var windowSecureIsolatedJSON []byte

//go:embed worker-secure.json
var workerSecureJSON []byte

//go:embed window-insecure.json
var windowInsecureJSON []byte

var windowInsecureOnce = sync.OnceValue(func() compatibility.RealmExposure {
	return parseOrderedWindowExposure(windowInsecureJSON, windowInsecureOrderJSON)
})

func InsecureWindowExposure() compatibility.RealmExposure { return windowInsecureOnce() }

func parseWindowExposure(raw []byte) compatibility.RealmExposure {
	var capture struct {
		Properties []compatibility.SurfaceProperty            `json:"properties"`
		Prototypes map[string][]compatibility.SurfaceProperty `json:"prototypes"`
	}
	if err := json.Unmarshal(raw, &capture); err != nil {
		panic("invalid generated Chrome 152 Window exposure: " + err.Error())
	}
	return compatibility.RealmExposure{Properties: capture.Properties, Prototypes: capture.Prototypes}
}

var windowSecureOnce = sync.OnceValue(func() compatibility.RealmExposure {
	return parseOrderedWindowExposure(windowSecureJSON, windowSecureOrderJSON)
})

func parseOrderedWindowExposure(raw, order []byte) compatibility.RealmExposure {
	exposure := parseWindowExposure(raw)
	// Frozen Window member order is invariant across secure and insecure
	// contexts. The installer filters unavailable members in each exposure;
	// security gating must not leave handwritten class installation order visible.
	if err := json.Unmarshal(windowSecureMemberOrderJSON, &exposure); err != nil {
		panic("invalid Chrome 152 member order: " + err.Error())
	}
	if err := json.Unmarshal(order, &exposure.PropertyOrder); err != nil {
		panic(err)
	}
	return exposure
}

func SecureWindowExposure() compatibility.RealmExposure { return windowSecureOnce() }

var windowSecureIsolatedOnce = sync.OnceValue(func() compatibility.RealmExposure {
	return parseOrderedWindowExposure(windowSecureIsolatedJSON, windowSecureIsolatedOrderJSON)
})

func SecureIsolatedWindowExposure() compatibility.RealmExposure { return windowSecureIsolatedOnce() }

var workerSecureOnce = sync.OnceValue(func() compatibility.RealmExposure {
	return parseWindowExposure(workerSecureJSON)
})

func SecureWorkerExposure() compatibility.RealmExposure { return workerSecureOnce() }
