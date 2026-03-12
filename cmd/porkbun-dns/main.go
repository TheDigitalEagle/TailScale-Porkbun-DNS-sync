package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"porkbun-dns/internal/config"
	"porkbun-dns/internal/porkbun"
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

	ts := tailscale.NewCLI(cfg.TailscaleBinary)
	client := porkbun.NewClient(cfg.APIKey, cfg.SecretAPIKey, cfg.BaseURL)
	svc := syncer.New(ts, client, cfg)

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
