// Command hexweb serves a browser front end for the hex map generators.
//
// The form is built from whatever each generator declares through mapgen, so
// a new generator appears in the picker with its own controls without this
// package changing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"time"

	_ "github.com/mdhender/marajanda/internal/generators"
	"github.com/mdhender/marajanda/internal/mapgen"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "listen address")
	open := flag.Bool("open", true, "open a browser on start")
	flag.Parse()

	if err := run(*addr, *open); err != nil {
		log.Fatalln("hexweb:", err)
	}
}

func run(addr string, openBrowser bool) error {
	gens := mapgen.All()
	if len(gens) == 0 {
		return errors.New("no generators registered")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleForm)
	mux.HandleFunc("GET /image", handleImage)
	mux.HandleFunc("GET /seed", handleSeed)

	// Listen before serving so the browser is only opened once the port is
	// actually bound, and so a port clash is reported rather than opening a
	// tab onto nothing.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	url := "http://" + ln.Addr().String() + "/"

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		// Generous: a large map at a small hex size takes a moment to render.
		WriteTimeout: 60 * time.Second,
	}

	log.Printf("serving %d generator(s) on %s", len(gens), url)
	for _, g := range gens {
		log.Printf("  %-14s %s", g.Name(), g.Title())
	}
	if openBrowser {
		go browse(url)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		log.Println("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// handleForm renders the picker and the selected generator's controls.
func handleForm(w http.ResponseWriter, r *http.Request) {
	gens := mapgen.All()

	name := r.URL.Query().Get("gen")
	g, ok := mapgen.Get(name)
	if !ok {
		g = gens[0]
	}

	// Values already in the query win, so switching generator keeps whatever
	// the user had typed for parameters the new one shares. Anything absent
	// falls back to the default, which for a seed is freshly random.
	values := mapgen.Defaults(g)
	for _, p := range g.Params() {
		if got := r.URL.Query()[p.Name]; len(got) > 0 && got[0] != "" {
			values[p.Name] = got[0]
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := page.Execute(w, formData{
		Generators: gens,
		Selected:   g,
		Values:     values,
	})
	if err != nil {
		log.Println("render:", err)
	}
}

// handleImage renders a map as a PNG. It is a GET so the result is a plain
// URL: shareable, bookmarkable, and openable in a new tab straight from the
// form's target.
func handleImage(w http.ResponseWriter, r *http.Request) {
	g, ok := mapgen.Get(r.URL.Query().Get("gen"))
	if !ok {
		http.Error(w, "unknown generator", http.StatusNotFound)
		return
	}

	start := time.Now()
	img, err := g.Generate(mapgen.FromForm(g, r.URL.Query()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	took := time.Since(start)

	b := img.Bounds()
	log.Printf("%s: %dx%d in %s", g.Name(), b.Dx(), b.Dy(), took.Round(time.Millisecond))

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	if err := png.Encode(w, img); err != nil {
		// Too late for an error status; the body is already going out.
		log.Println("encode:", err)
	}
}

// handleSeed hands the page a fresh seed, so the randomise button does not
// need a second source of randomness in the browser.
func handleSeed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, strconv.FormatUint(rand.Uint64(), 10))
}

func browse(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	if err := exec.Command(cmd, append(args, url)...).Start(); err != nil {
		log.Printf("could not open a browser (%v); visit %s", err, url)
	}
}

type formData struct {
	Generators []mapgen.Generator
	Selected   mapgen.Generator
	Values     map[string]string
}
