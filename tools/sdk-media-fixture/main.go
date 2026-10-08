// sdk-media-fixture is an explicit test executable with deterministic camera
// and microphone providers. It never enumerates or opens native hardware.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/browser"
	"github.com/moreveal/mimic/internal/camera"
	"github.com/moreveal/mimic/internal/cdp"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"github.com/moreveal/mimic/internal/microphone"
)

type fixtureState struct {
	mu      sync.Mutex
	missing map[string]bool
	fail    map[string]bool
	opens   map[string]int
	closes  map[string]int
}

func (s *fixtureState) available(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.missing[id]
}

func (s *fixtureState) open(id string) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.missing[id] || s.fail[id] {
		return nil, fmt.Errorf("private backend /dev/sdk-fixture/%s unavailable", id)
	}
	s.opens[id]++
	return func() { s.mu.Lock(); s.closes[id]++; s.mu.Unlock() }, nil
}

type cameras struct{ state *fixtureState }

func (p cameras) Devices(context.Context) ([]camera.Device, error) {
	devices := []camera.Device{}
	for _, item := range []camera.Device{{ID: "native-camera-a", Label: "Private native camera A"}, {ID: "native-camera-b", Label: "Private native camera B"}} {
		if p.state.available(item.ID) {
			devices = append(devices, item)
		}
	}
	return devices, nil
}

func (p cameras) Open(ctx context.Context, id string, choose func([]camera.Format) (camera.Format, error)) (camera.Capture, camera.Format, error) {
	if id != "native-camera-a" && id != "native-camera-b" {
		return nil, camera.Format{}, fmt.Errorf("unknown private source %s", id)
	}
	format, err := choose([]camera.Format{{Width: 8, Height: 4, FrameRate: 30}})
	if err != nil {
		return nil, format, err
	}
	closed, err := p.state.open(id)
	if err != nil {
		return nil, format, err
	}
	pixel := color.RGBA{R: 255, A: 255}
	if id == "native-camera-b" {
		pixel = color.RGBA{B: 255, A: 255}
	}
	frame := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			frame.SetRGBA(x, y, pixel)
		}
	}
	return &videoCapture{ctx: ctx, done: make(chan struct{}), frame: frame, close: closed}, format, nil
}

type videoCapture struct {
	ctx   context.Context
	done  chan struct{}
	frame *image.RGBA
	close func()
	once  sync.Once
}

func (c *videoCapture) Read() (image.Image, func(), error) {
	timer := time.NewTimer(time.Second / 30)
	defer timer.Stop()
	select {
	case <-c.ctx.Done():
		return nil, nil, c.ctx.Err()
	case <-c.done:
		return nil, nil, io.EOF
	case <-timer.C:
		return c.frame, func() {}, nil
	}
}
func (c *videoCapture) Close() error { c.once.Do(func() { close(c.done); c.close() }); return nil }

type microphones struct{ state *fixtureState }

func (p microphones) Devices(context.Context) ([]microphone.Device, error) {
	devices := []microphone.Device{}
	for _, item := range []microphone.Device{{ID: "native-microphone-a", Label: "Private native microphone A", Default: true}, {ID: "native-microphone-b", Label: "Private native microphone B"}} {
		if p.state.available(item.ID) {
			devices = append(devices, item)
		}
	}
	return devices, nil
}
func (p microphones) Open(_ context.Context, id string, format microphone.Format) (microphone.Capture, error) {
	if id != "native-microphone-a" && id != "native-microphone-b" {
		return nil, fmt.Errorf("unknown private source %s", id)
	}
	closed, err := p.state.open(id)
	if err != nil {
		return nil, err
	}
	frequency := 440.
	if id == "native-microphone-b" {
		frequency = 660
	}
	return &audioCapture{done: make(chan struct{}), format: format, frequency: frequency, close: closed}, nil
}

type audioCapture struct {
	done      chan struct{}
	format    microphone.Format
	frequency float64
	offset    int
	close     func()
	once      sync.Once
}

func (c *audioCapture) Read(ctx context.Context) ([]float32, error) {
	timer := time.NewTimer(20 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, io.EOF
	case <-timer.C:
	}
	count := c.format.SampleRate / 50
	samples := make([]float32, count*c.format.Channels)
	for i := 0; i < count; i++ {
		value := float32(.2 * math.Sin(2*math.Pi*c.frequency*float64(c.offset+i)/float64(c.format.SampleRate)))
		for j := 0; j < c.format.Channels; j++ {
			samples[i*c.format.Channels+j] = value
		}
	}
	c.offset += count
	return samples, nil
}
func (c *audioCapture) Close() error { c.once.Do(func() { close(c.done); c.close() }); return nil }

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "CDP loopback listener")
	mode := flag.String("browser-mode", "headless", "must be headless")
	engine := flag.String("engine", "v8", "fixture engine")
	flag.Parse()
	if *mode != "headless" || *engine != "v8" {
		panic("fixture requires headless V8")
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || host != "127.0.0.1" {
		panic("fixture requires loopback")
	}
	state := &fixtureState{missing: map[string]bool{}, fail: map[string]bool{}, opens: map[string]int{}, closes: map[string]int{}}
	b, err := browser.NewWithOptions(v8engine.Factory{}, chrome152.New(), browser.Options{CameraProvider: cameras{state}, MicrophoneProvider: microphones{state}})
	if err != nil {
		panic(err)
	}
	s, err := cdp.New(b)
	if err != nil {
		panic(err)
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		panic(err)
	}
	fixtureListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"opens": state.opens, "closes": state.closes})
	})
	mux.HandleFunc("/control", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		var value struct {
			Missing map[string]bool `json:"missing"`
			Fail    map[string]bool `json:"fail"`
		}
		if json.NewDecoder(r.Body).Decode(&value) != nil {
			http.Error(w, "invalid fixture control", 400)
			return
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		for key, item := range value.Missing {
			state.missing[key] = item
		}
		for key, item := range value.Fail {
			state.fail[key] = item
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<!doctype html><title>SDK media contract fixture</title><body>Deterministic media fixture</body>")
	})
	fixture := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go fixture.Serve(fixtureListener)
	fmt.Printf("Mimic listening on http://%s\nFixture listening on http://%s\n", listener.Addr(), fixtureListener.Addr())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() { <-ctx.Done(); s.Close(context.Background()) }()
	if err := s.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
	fixture.Close()
	b.Close()
	stop()
}
