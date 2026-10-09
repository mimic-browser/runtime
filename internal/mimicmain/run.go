package mimicmain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"runtime/pprof"
	"strings"
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
	"github.com/moreveal/mimic/internal/optimize"
	"github.com/moreveal/mimic/internal/state"
	"github.com/moreveal/mimic/internal/workload"
)

func Run() {
	if len(os.Args) > 1 && os.Args[1] == "optimize" {
		os.Exit(optimize.Main(os.Args[2:]))
	}
	listen := flag.String("listen", "127.0.0.1:9222", "CDP HTTP/WebSocket listen address")
	milestone := flag.Int("chrome", 152, "installed Chrome compatibility milestone")
	navigationTimeout := flag.Duration("navigation-timeout", 0, "optional navigation execution cap; 0 keeps loading until completion or cancellation")
	engineName := flag.String("engine", "v8", "ECMAScript engine adapter: v8, quickjs, or goja")
	browserMode := flag.String("browser-mode", "headful", "selected environment profile: headful or headless")
	resourcePolicyPath := flag.String("resource-policy", "", "JSON resource policy for new contexts")
	profilePath := flag.String("profile", "", "named workload profile or .mprofile artifact")
	devPreview := flag.Bool("dev-preview", false, "enable the visual debug viewer at /debug/preview/")
	devToolsChrome := flag.String("devtools-chrome", "", "optional Chrome/Chromium executable for on-demand DevTools page viewing")
	workloadControl := flag.String("workload-control", "", "experimental loopback workload runner control address")
	workloadCapture := flag.String("workload-capture", "", "experimental binary transport capture output")
	workloadReplay := flag.String("workload-replay", "", "experimental offline binary transport capture input")
	workloadPlan := flag.String("workload-plan", "", "experimental offline specialization artifact")
	workloadInventory := flag.Bool("workload-inventory", false, "collect experimental search resource/script inventory")
	workloadVolatileQuery := flag.String("workload-volatile-query", "", "explicit comma-separated volatile query keys for capture matching")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: mimic [options]\n       mimic optimize --name shop -- <workload command>")
		public := flag.NewFlagSet("mimic", flag.ContinueOnError)
		public.SetOutput(flag.CommandLine.Output())
		flag.VisitAll(func(f *flag.Flag) {
			if !strings.HasPrefix(f.Name, "workload-") {
				public.Var(f.Value, f.Name, f.Usage)
			}
		})
		public.PrintDefaults()
		fmt.Fprintln(flag.CommandLine.Output(), "Optimize guide: https://mimic.boo/docs/optimize/")
	}
	flag.Parse()
	var activeProfile *workload.Profile
	if *profilePath != "" && *workloadPlan != "" {
		log.Fatal("--profile and private --workload-plan cannot be combined")
	}
	if *profilePath != "" {
		path, err := workload.ProfilePath(*profilePath)
		if err != nil {
			log.Fatal(err)
		}
		profile, err := workload.ReadProfile(path)
		if err != nil {
			log.Fatalf("Cannot load workload profile: %v. Learn more: https://mimic.boo/docs/optimize/troubleshooting/", err)
		}
		executable, err := os.Executable()
		if err != nil {
			log.Fatal(err)
		}
		identity, err := workload.FileSHA256(executable)
		if err != nil {
			log.Fatal(err)
		}
		if profile.BuildSHA256 != identity {
			log.Fatal("Workload profile was validated with a different Mimic build; re-optimize. Learn more: https://mimic.boo/docs/optimize/troubleshooting/")
		}
		flag.Visit(func(f *flag.Flag) {
			if f.Name == "engine" && *engineName != profile.Engine || f.Name == "browser-mode" && *browserMode != profile.BrowserMode || f.Name == "chrome" && *milestone != profile.Chrome {
				log.Fatal("Workload profile runtime identity differs from the requested runtime")
			}
		})
		*engineName, *browserMode, *milestone = profile.Engine, profile.BrowserMode, profile.Chrome
		activeProfile = &profile
	}
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
	options := browser.Options{ResourcePolicyJSON: resourcePolicyJSON, DevPreview: *devPreview}
	options.ExecutionProfile = activeProfile
	options.ProfileNote = func(note string) { log.Print(note) }
	if activeProfile != nil {
		log.Printf("Workload profile %s: %d recorded states; empirically validated, request-scoped. Only asserted behavior is covered. https://mimic.boo/docs/optimize/safety/", *profilePath, len(activeProfile.CaptureSHA256))
	}
	var experiment *workload.Session
	if *workloadControl == "" && (*workloadCapture != "" || *workloadReplay != "" || *workloadPlan != "") {
		log.Fatal("workload artifacts require the experimental runner control endpoint")
	}
	if *workloadControl != "" {
		var suppressed []string
		if *workloadPlan != "" {
			if *workloadReplay == "" || len(resourcePolicyJSON) != 0 {
				log.Fatal("workload plan requires replay and owns its resource policy")
			}
			plan, err := workload.ReadPlan(*workloadPlan)
			if err != nil {
				log.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				log.Fatal(err)
			}
			for path, expected := range map[string]string{executable: plan.BinarySHA256, *workloadReplay: plan.CaptureSHA256} {
				file, err := os.Open(path)
				if err != nil {
					log.Fatal(err)
				}
				digest := sha256.New()
				_, err = io.Copy(digest, file)
				_ = file.Close()
				if err != nil {
					log.Fatal(err)
				}
				if hex.EncodeToString(digest.Sum(nil)) != expected {
					log.Fatal("workload plan artifact identity mismatch")
				}
			}
			options.ResourcePolicyJSON, _ = json.Marshal(plan.Policy)
			suppressed = plan.SuppressClassic
		}
		spoolDirectory := ""
		if *workloadCapture != "" {
			spoolDirectory = filepath.Dir(*workloadCapture)
		}
		experiment, err = workload.NewWithSpoolDirectory(*workloadCapture != "", *workloadReplay, suppressed, spoolDirectory)
		if err != nil {
			log.Fatal(err)
		}
		experiment.SetInventory(*workloadInventory)
		defer experiment.Close()
		if *workloadVolatileQuery != "" {
			if err := experiment.SetVolatileQuery(strings.Split(*workloadVolatileQuery, ",")); err != nil {
				log.Fatal(err)
			}
		}
		options.TransportWrapper = experiment.Wrap
		options.ClassicScriptAdmission = experiment.AdmitClassic
		options.ClassicScriptSkipped = experiment.ObserveSuppressedClassic
	}
	b, err := browser.NewWithOptions(factory, bundle, options)
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
	s.DevToolsChrome = *devToolsChrome
	if experiment != nil {
		s.SetConnectionOpened(experiment.ObserveCDPConnection)
	}
	s.SetNavigationTimeout(*navigationTimeout)
	l, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	if cliui.IsInteractive(os.Stdout) {
		info := cliui.StartupInfo{Version: cliui.BuildVersion(), Chrome: *milestone, Engine: *engineName, Address: l.Addr().String()}
		if err := cliui.WriteStartup(os.Stdout, info, cliui.SupportsColor(os.Stdout)); err != nil {
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
	if experiment != nil {
		host, _, err := net.SplitHostPort(*workloadControl)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			log.Fatal("workload control must bind a loopback IP")
		}
		token := os.Getenv("MIMIC_WORKLOAD_TOKEN")
		if len(token) < 32 {
			log.Fatal("workload control requires MIMIC_WORKLOAD_TOKEN")
		}
		controlListener, err := net.Listen("tcp", *workloadControl)
		if err != nil {
			log.Fatal(err)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		mux.HandleFunc("/snapshot", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var captureError string
			if *workloadCapture != "" {
				if err := experiment.SaveEvidence(*workloadCapture); err != nil {
					captureError = err.Error()
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"metrics": experiment.Snapshot(), "captureError": captureError})
		})
		mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			stop()
		})
		mux.HandleFunc("/stacks", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_ = pprof.Lookup("goroutine").WriteTo(w, 2)
		})
		control := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = control.Serve(controlListener) }()
		defer func() {
			shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = control.Shutdown(shutdown)
		}()
	}
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
