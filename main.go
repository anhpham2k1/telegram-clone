package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[clone] ")

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	dispatcher := tg.NewUpdateDispatcher()
	var active atomic.Pointer[forwarder]
	dispatcher.OnNewChannelMessage(func(ctx context.Context, _ tg.Entities, update *tg.UpdateNewChannelMessage) error {
		f := active.Load()
		if f == nil {
			return nil
		}
		return f.Handle(ctx, update)
	})

	client := telegram.NewClient(cfg.apiID, cfg.apiHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: cfg.sessionPath},
		UpdateHandler:  dispatcher,
	})

	// Start health server immediately so Render's port check passes.
	healthSrv := newHealthServer(cfg.healthPort, &active)
	healthSrv.Start()

	authenticator := newTerminalAuth(cfg.phone, cfg.password)
	err = client.Run(ctx, func(ctx context.Context) error {
		flow := auth.NewFlow(authenticator, auth.SendCodeOptions{})
		if err := client.Auth().IfNecessary(ctx, flow); err != nil {
			return fmt.Errorf("authenticate: %w", err)
		}

		self, err := client.Self(ctx)
		if err != nil {
			return fmt.Errorf("get current account: %w", err)
		}
		name := strings.TrimSpace(self.FirstName + " " + self.LastName)
		log.Printf("logged in as %s (id=%d)", name, self.ID)
		healthSrv.SetSelf(name)

		source, err := resolveChannel(ctx, client.API(), cfg.source)
		if err != nil {
			return err
		}
		if source.Left {
			if !cfg.autoJoin {
				return fmt.Errorf("account is not in %s; join it or set TG_AUTO_JOIN=true", channelName(source))
			}
			if err := joinChannel(ctx, client.API(), source); err != nil {
				return err
			}
			log.Printf("joined source channel %s", channelName(source))
		}

		target, err := resolveChannel(ctx, client.API(), cfg.target)
		if err != nil {
			return err
		}
		if err := ensureCanPost(ctx, client.API(), target); err != nil {
			return err
		}

		f := newForwarder(ctx, client.API(), source, target, cfg.keepAuthor)
		defer f.Close()
		active.Store(f)

		log.Printf("bridging %s -> %s", channelName(source), channelName(target))

		if cfg.backfill > 0 {
			if err := f.Backfill(ctx, cfg.backfill); err != nil {
				log.Printf("backfill: %v", err)
			}
		}

		<-ctx.Done()
		return nil
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("run: %v", err)
	}
}
