package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	vercelupdater "github.com/kota-yata/clifford/vercel-updater"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go vercelupdater.Watch(ctx)
	if err := vercelupdater.Update(ctx); err != nil {
		log.Fatal(err)
	}
}
