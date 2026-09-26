package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"tgstrm/internal/config"
	"tgstrm/internal/httpapi"
)

var version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		runHealthcheck()
		return
	}
	addr := os.Getenv("TGSTRM_ADDR")
	if addr == "" {
		addr = "127.0.0.1:5151"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		log.Fatalf("TGSTRM_ADDR debe tener el formato host:puerto: %v", err)
	}
	loopback := host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
	if !loopback && os.Getenv("TGSTRM_ALLOW_REMOTE") != "1" {
		log.Fatal("La exposición fuera de loopback requiere TGSTRM_ALLOW_REMOTE=1. Publica el servicio detrás de HTTPS y del login administrativo.")
	}
	dataDir := os.Getenv("TGSTRM_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	store, err := config.Open(dataDir)
	if err != nil {
		log.Fatal("No se puede abrir la configuración persistente: ", err)
	}
	if err = store.EnsureDataLayout(); err != nil {
		log.Fatal("No se puede preparar la estructura persistente: ", err)
	}
	trustedProxyCIDRs := strings.Split(os.Getenv("TGSTRM_TRUSTED_PROXY_CIDRS"), ",")
	api, err := httpapi.NewWithOptions(store, httpapi.Options{TrustedProxyCIDRs: trustedProxyCIDRs})
	if err != nil {
		log.Fatal("Configuración persistente inválida; no se reiniciará la instalación: ", err)
	}
	defer api.Close()
	if cfg, _ := store.LoadDatabase(); cfg == nil {
		log.Printf("Código de instalación (solo para configurar PostgreSQL desde la web): %s", store.Token)
	}
	server := &http.Server{Addr: addr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Print(err)
		}
	}()
	log.Printf("TgStrm %s: http://%s", version, addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func runHealthcheck() {
	url := os.Getenv("TGSTRM_HEALTHCHECK_URL")
	if url == "" {
		url = "http://127.0.0.1:5151/api/v1/health/live"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		log.Fatal(fmt.Sprintf("healthcheck returned HTTP %d", response.StatusCode))
	}
}
