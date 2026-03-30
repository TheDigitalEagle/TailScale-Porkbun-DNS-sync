package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"os/signal"
	"syscall"

	"porkbun-dns/internal/api"
	"porkbun-dns/internal/config"
	"porkbun-dns/internal/porkbun"
	"porkbun-dns/internal/publicip"
	"porkbun-dns/internal/syncer"
	"porkbun-dns/internal/tailscale"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	svc, client := buildService(cfg)
	if cfg.APIEnabled {
		server := api.NewServer(api.Config{
			ListenAddr:   cfg.APIListenAddr,
			Domain:       cfg.Domain,
			SyncInterval: cfg.SyncInterval,
		}, svc, client)

		if err := server.Run(ctx); err != nil {
			log.Fatalf("api failed: %v", err)
		}
		return
	}

	result, err := svc.Run(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			log.Fatal("sync canceled")
		}
		log.Fatalf("sync failed: %v", err)
	}

	fmt.Printf(
		"sync complete: desired=%d unchanged=%d created=%d updated=%d deleted=%d\n",
		result.Desired,
		result.Unchanged,
		result.Created,
		result.Updated,
		result.Deleted,
	)
}

func buildService(cfg config.Config) (*syncer.Service, *porkbun.Client) {
	ts := tailscale.NewCLI(cfg.TailscaleBinary)
	client := porkbun.NewClient(cfg.APIKey, cfg.SecretAPIKey, cfg.BaseURL)

	var publicIPv4 syncer.PublicIPSource
	if cfg.PublicIPEnabled {
		publicIPv4 = publicip.NewChecker(cfg.PublicIPLookupURL)
	}

	var publicIPv6 syncer.PublicIPv6Source
	if cfg.PublicIPv6Enabled {
		if cfg.PublicIPv6Address.IsValid() {
			publicIPv6 = staticIPv6Source{addr: cfg.PublicIPv6Address}
		} else {
			publicIPv6 = publicip.NewChecker(cfg.PublicIPv6LookupURL)
		}
	}

	return syncer.New(ts, publicIPv4, publicIPv6, client, cfg), client
}

type staticIPv6Source struct {
	addr netip.Addr
}

func (s staticIPv6Source) IPv6(context.Context) (netip.Addr, error) {
	return s.addr, nil
}
