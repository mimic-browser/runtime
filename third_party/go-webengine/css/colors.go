// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

// namedColors is the small set of CSS named colours Phase 0 recognises. It is
// intentionally not the full X11 list — just the ones common in real pages and
// in the user-agent stylesheet.
var namedColors = map[string]Color{
	"black":       {0, 0, 0, 255},
	"white":       {255, 255, 255, 255},
	"red":         {255, 0, 0, 255},
	"green":       {0, 128, 0, 255},
	"lime":        {0, 255, 0, 255},
	"blue":        {0, 0, 255, 255},
	"navy":        {0, 0, 128, 255},
	"yellow":      {255, 255, 0, 255},
	"orange":      {255, 165, 0, 255},
	"purple":      {128, 0, 128, 255},
	"gray":        {128, 128, 128, 255},
	"grey":        {128, 128, 128, 255},
	"silver":      {192, 192, 192, 255},
	"maroon":      {128, 0, 0, 255},
	"teal":        {0, 128, 128, 255},
	"olive":       {128, 128, 0, 255},
	"aqua":        {0, 255, 255, 255},
	"cyan":        {0, 255, 255, 255},
	"fuchsia":     {255, 0, 255, 255},
	"magenta":     {255, 0, 255, 255},
	"lightgray":   {211, 211, 211, 255},
	"lightgrey":   {211, 211, 211, 255},
	"darkgray":    {169, 169, 169, 255},
	"darkgrey":    {169, 169, 169, 255},
	"whitesmoke":  {245, 245, 245, 255},
	"lightblue":   {173, 216, 230, 255},
	"steelblue":   {70, 130, 180, 255},
	"dodgerblue":  {30, 144, 255, 255},
	"royalblue":   {65, 105, 225, 255},
	"darkblue":    {0, 0, 139, 255},
	"lightyellow": {255, 255, 224, 255},
	"gold":        {255, 215, 0, 255},
	"crimson":     {220, 20, 60, 255},
	"darkgreen":   {0, 100, 0, 255},
	"forestgreen": {34, 139, 34, 255},
}
