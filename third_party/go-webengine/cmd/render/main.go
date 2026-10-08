// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// Command render fetches a URL and writes a PNG screenshot of the statically
// rendered page.
//
//	render -url https://example.com/ -out shot.png -w 1024 -h 768
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/go-webengine/engine"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr *os.File) int {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(stderr)
	url := fs.String("url", "", "URL to render (required unless -file is given)")
	file := fs.String("file", "", "local HTML file to render offline (bypasses -url)")
	base := fs.String("base", "", "base URL for resolving relative resources with -file")
	out := fs.String("out", "out.png", "output PNG path")
	w := fs.Int("w", 1024, "viewport width")
	h := fs.Int("h", 768, "viewport height")
	timeout := fs.Duration("timeout", 45*time.Second, "overall timeout")
	cpuprofile := fs.String("cpuprofile", "", "write a CPU profile to this path")
	memprofile := fs.String("memprofile", "", "write a heap profile to this path, taken after render")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *url == "" && *file == "" {
		fmt.Fprintln(stderr, "render: -url or -file is required")
		return 2
	}
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fmt.Fprintf(stderr, "render: cpuprofile: %v\n", err)
			return 1
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(stderr, "render: cpuprofile: %v\n", err)
			return 1
		}
		defer pprof.StopCPUProfile()
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	e := engine.New()
	var (
		img  *image.RGBA
		info *engine.RenderInfo
		err  error
	)
	start := time.Now()
	if *file != "" {
		var src []byte
		if src, err = os.ReadFile(*file); err != nil {
			fmt.Fprintf(stderr, "render: read file: %v\n", err)
			return 1
		}
		img, info, err = e.RenderHTML(ctx, string(src), *base, image.Rect(0, 0, *w, *h))
	} else {
		img, info, err = e.Render(ctx, *url, image.Rect(0, 0, *w, *h))
	}
	elapsed := time.Since(start)
	if err != nil {
		fmt.Fprintf(stderr, "render: %v\n", err)
		return 1
	}
	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			fmt.Fprintf(stderr, "render: memprofile: %v\n", err)
			return 1
		}
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			fmt.Fprintf(stderr, "render: memprofile: %v\n", err)
			return 1
		}
		f.Close()
	}
	png, err := engine.EncodePNG(img)
	if err != nil {
		fmt.Fprintf(stderr, "render: encode: %v\n", err)
		return 1
	}
	if err := os.WriteFile(*out, png, 0o644); err != nil {
		fmt.Fprintf(stderr, "render: write: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "rendered %q -> %s (%dx%d, title=%q, contentHeight=%d) in %s\n",
		info.URL, *out, img.Rect.Dx(), img.Rect.Dy(), info.Title, info.ContentHeight, elapsed)
	return 0
}
