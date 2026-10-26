package main

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

const albumWait = 1500 * time.Millisecond

type albumBuffer struct {
	ids   []int
	timer *time.Timer
}

type forwarder struct {
	ctx        context.Context
	api        *tg.Client
	sourceID   int64
	source     tg.InputPeerClass
	target     tg.InputPeerClass
	keepAuthor bool

	// channel names for health reporting
	sourceName string
	targetName string

	mu     sync.Mutex
	seen   map[int]struct{}
	albums map[int64]*albumBuffer

	jobs      chan []int
	done      chan struct{}
	closeOnce sync.Once

	// stats
	startedAt       time.Time
	totalForwarded  atomic.Int64
	lastForwardedAt atomic.Int64 // unix seconds, 0 = never
}

func newForwarder(ctx context.Context, api *tg.Client, source, target *tg.Channel, keepAuthor bool) *forwarder {
	f := &forwarder{
		ctx:        ctx,
		api:        api,
		sourceID:   source.ID,
		source:     source.AsInputPeer(),
		target:     target.AsInputPeer(),
		keepAuthor: keepAuthor,
		sourceName: channelName(source),
		targetName: channelName(target),
		seen:       make(map[int]struct{}),
		albums:     make(map[int64]*albumBuffer),
		jobs:       make(chan []int, 100),
		done:       make(chan struct{}),
		startedAt:  time.Now(),
	}
	go f.worker()
	return f
}

func (f *forwarder) Close() {
	f.closeOnce.Do(func() {
		close(f.done)
	})
}

func (f *forwarder) Handle(_ context.Context, update *tg.UpdateNewChannelMessage) error {
	message, ok := update.Message.(*tg.Message)
	if !ok {
		return nil
	}

	peer, ok := message.PeerID.(*tg.PeerChannel)
	if !ok || peer.ChannelID != f.sourceID {
		return nil
	}

	if message.Noforwards {
		log.Printf("skip post %d: source channel forbids forwarding", message.ID)
		return nil
	}

	if !f.claim(message.ID) {
		return nil
	}

	if groupedID, ok := message.GetGroupedID(); ok && groupedID != 0 {
		f.addAlbum(groupedID, message.ID)
		return nil
	}

	f.enqueue([]int{message.ID})
	return nil
}

func (f *forwarder) worker() {
	for {
		select {
		case <-f.done:
			return
		case <-f.ctx.Done():
			return
		case ids := <-f.jobs:
			if err := f.forward(f.ctx, ids); err != nil && f.ctx.Err() == nil {
				log.Printf("forward: %v", err)
			}
		}
	}
}

func (f *forwarder) enqueue(ids []int) {
	if len(ids) == 0 {
		return
	}
	select {
	case f.jobs <- ids:
	case <-f.done:
	case <-f.ctx.Done():
	}
}

func (f *forwarder) claim(id int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.seen[id]; exists {
		return false
	}
	f.seen[id] = struct{}{}
	if len(f.seen) > 100000 {
		f.seen = map[int]struct{}{id: {}}
	}
	return true
}

func (f *forwarder) addAlbum(groupedID int64, id int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	album := f.albums[groupedID]
	if album == nil {
		album = &albumBuffer{}
		f.albums[groupedID] = album
	}
	album.ids = append(album.ids, id)
	if album.timer != nil {
		album.timer.Stop()
	}
	album.timer = time.AfterFunc(albumWait, func() {
		f.flushAlbum(groupedID)
	})
}

func (f *forwarder) flushAlbum(groupedID int64) {
	f.mu.Lock()
	album := f.albums[groupedID]
	delete(f.albums, groupedID)
	f.mu.Unlock()

	if album == nil {
		return
	}
	f.enqueue(album.ids)
}

func (f *forwarder) forward(ctx context.Context, ids []int) error {
	ids = compactIDs(ids)
	if len(ids) == 0 {
		return nil
	}

	for {
		randomIDs := make([]int64, len(ids))
		for i := range randomIDs {
			randomIDs[i] = rand.Int64()
		}

		_, err := f.api.MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
			FromPeer:   f.source,
			ID:         ids,
			RandomID:   randomIDs,
			ToPeer:     f.target,
			DropAuthor: !f.keepAuthor,
		})
		if err == nil {
			log.Printf("forwarded %d post(s): %v", len(ids), ids)
			f.totalForwarded.Add(int64(len(ids)))
			f.lastForwardedAt.Store(time.Now().Unix())
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		waited, waitErr := tgerr.FloodWait(ctx, err)
		if waited {
			continue
		}
		return fmt.Errorf("forward %v: %w", ids, waitErr)
	}
}

func (f *forwarder) Backfill(ctx context.Context, limit int) error {
	if limit <= 0 {
		return nil
	}
	if limit > 100 {
		limit = 100
	}

	history, err := f.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:  f.source,
		Limit: limit,
	})
	if err != nil {
		return fmt.Errorf("get source history: %w", err)
	}

	var messages []*tg.Message
	switch h := history.(type) {
	case *tg.MessagesChannelMessages:
		messages = collectMessages(h.Messages)
	case *tg.MessagesMessages:
		messages = collectMessages(h.Messages)
	case *tg.MessagesMessagesSlice:
		messages = collectMessages(h.Messages)
	default:
		return fmt.Errorf("unexpected source history type %T", history)
	}

	sort.Slice(messages, func(i, j int) bool {
		return messages[i].ID < messages[j].ID
	})

	for i := 0; i < len(messages); {
		message := messages[i]
		if message.Noforwards {
			i++
			continue
		}

		ids := []int{message.ID}
		if groupedID, ok := message.GetGroupedID(); ok && groupedID != 0 {
			j := i + 1
			for j < len(messages) {
				nextGroupedID, hasGroup := messages[j].GetGroupedID()
				if !hasGroup || nextGroupedID != groupedID {
					break
				}
				ids = append(ids, messages[j].ID)
				j++
			}
			i = j
		} else {
			i++
		}

		fresh := ids[:0]
		for _, id := range ids {
			if f.claim(id) {
				fresh = append(fresh, id)
			}
		}
		if len(fresh) == 0 {
			continue
		}

		if err := f.forward(ctx, fresh); err != nil {
			return err
		}

		select {
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func collectMessages(messages []tg.MessageClass) []*tg.Message {
	result := make([]*tg.Message, 0, len(messages))
	for _, message := range messages {
		if value, ok := message.(*tg.Message); ok {
			result = append(result, value)
		}
	}
	return result
}

func compactIDs(ids []int) []int {
	result := append([]int(nil), ids...)
	sort.Ints(result)

	n := 0
	for _, id := range result {
		if n == 0 || result[n-1] != id {
			result[n] = id
			n++
		}
	}
	return result[:n]
}
