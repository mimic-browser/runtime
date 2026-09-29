package mimicmain

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/moreveal/mimic/chrome"
	"github.com/moreveal/mimic/internal/browser"
	"github.com/moreveal/mimic/internal/cdp"
	"github.com/moreveal/mimic/internal/cliui"
	"github.com/moreveal/mimic/internal/engine"
	gojaengine "github.com/moreveal/mimic/internal/engine/goja"
	quickjsengine "github.com/moreveal/mimic/internal/engine/quickjs"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"github.com/moreveal/mimic/internal/state"
)

func Run() {
	listen := flag.String("listen", "127.0.0.1:9222", "CDP HTTP/WebSocket listen address")
	milestone := flag.Int("chrome", 152, "installed Chrome compatibility milestone")
	navigationTimeout := flag.Duration("navigation-timeout", 0, "optional navigation execution cap; 0 keeps loading until completion or cancellation")
	engineName := flag.String("engine", "v8", "ECMAScript engine adapter: v8, quickjs, or goja")
	browserMode := flag.String("browser-mode", "headful", "selected environment profile: headful or headless")
	resourcePolicyPath := flag.String("resource-policy", "", "JSON resource policy for new contexts")
	devPreview := flag.Bool("dev-preview", false, "enable the visual debug viewer at /debug/preview/")
	flag.Parse()
	// The V8 frontend keeps a substantial Go-side DOM and CDP projection during
	// Page execution. A 50% heap growth target reduced measured concurrent and
	// Wikipedia RSS without a sustained throughput penalty. Respect an explicit
	// Go runtime setting, including GOGC=off, for deployments that tune it.
	if *engineName == "v8" && os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}
	var cpuProfile *os.File
	if path := os.Getenv("MIMIC_GO_CPU_PROFILE"); path != "" {
		var err error
		cpuProfile, err = os.Create(path)
		if err != nil {
			log.Fatal(err)
		}
		if err := pprof.StartCPUProfile(cpuProfile); err != nil {
			_ = cpuProfile.Close()
			log.Fatal(err)
		}
		defer func() { pprof.StopCPUProfile(); _ = cpuProfile.Close() }()
	}
	bundle, err := chrome.GetForMode(*milestone, state.BrowserMode(*browserMode))
	if err != nil {
		log.Fatal(err)
	}
	var factory engine.Factory
	switch *engineName {
	case "quickjs":
		factory = quickjsengine.Factory{}
	case "goja":
		factory = gojaengine.Factory{}
	case "v8":
		factory = v8engine.Factory{}
	default:
		log.Fatalf("unsupported JavaScript engine %q", *engineName)
	}
	var resourcePolicyJSON []byte
	if *resourcePolicyPath != "" {
		resourcePolicyJSON, err = os.ReadFile(*resourcePolicyPath)
		if err != nil {
			log.Fatal(err)
		}
	}
	b, err := browser.NewWithOptions(factory, bundle, browser.Options{ResourcePolicyJSON: resourcePolicyJSON, DevPreview: *devPreview})
	if err != nil {
		log.Fatal(err)
	}
	cacheDir := bootstrapCacheDir()
	if cacheDir != "" {
		if err := b.ConfigureBootstrapCache(cacheDir); err != nil {
			_ = b.Close()
			log.Fatal(err)
		}
	}
	s, err := cdp.New(b)
	if err != nil {
		log.Fatal(err)
	}
	s.SetNavigationTimeout(*navigationTimeout)
	l, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	if cliui.IsInteractive(os.Stdout) {
		info := cliui.StartupInfo{Version: cliui.BuildVersion(), Chrome: *milestone, Engine: *engineName, Address: l.Addr().String()}
		if err := cliui.WriteStartup(os.Stdout, info, os.Getenv("NO_COLOR") == ""); err != nil {
			log.Print(err)
		}
	} else {
		// Keep redirected output stable for launchers that parse readiness.
		fmt.Printf("Mimic listening on http://%s\n", l.Addr())
	}
	if *devPreview {
		fmt.Printf("Debug preview: http://%s/debug/preview/\n", l.Addr())
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cacheDir != "" {
		go func() {
			// Give protocol discovery and an immediate first request the scheduler
			// before cold-cache compilation starts consuming a V8 worker.
			timer := time.NewTimer(2 * time.Second)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return
			}
			prepare, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			if err := b.PrepareBootstrap(prepare, cacheDir); err != nil && ctx.Err() == nil {
				log.Printf("bootstrap preparation: %v", err)
			}
		}()
	}
	if parentExited := parentExitSignal(); parentExited != nil {
		go func() {
			select {
			case <-parentExited:
				stop()
			case <-ctx.Done():
			}
		}()
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Close(shutdown)
	}()
	if err := s.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Print(err)
	}
	stop()
	<-shutdownDone
}

func bootstrapCacheDir() string {
	if value, ok := os.LookupEnv("MIMIC_BOOTSTRAP_CACHE_DIR"); ok {
		return value
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "Mimic", "bootstrap")
}
