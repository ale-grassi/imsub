package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

type dumpFunc func(ctx context.Context, creator Creator) (int, error)

const subscriberReconcileConcurrency = 4

var (
	// ErrListActiveCreators reports that listing active creators failed.
	ErrListActiveCreators = errors.New("list active creators failed")
	// ErrPartialReconcile reports that at least one creator reconciliation failed.
	ErrPartialReconcile = errors.New("partial reconcile failure")
)

type reconcilerStore interface {
	ListActiveCreators(ctx context.Context) ([]Creator, error)
}

// ReconcilerService periodically refreshes subscriber caches for active creators.
type ReconcilerService struct {
	store   reconcilerStore
	dump    dumpFunc
	log     *slog.Logger
	timeout time.Duration
}

// NewReconcilerService creates a reconciler service with default timeout settings.
func NewReconcilerService(store reconcilerStore, dump dumpFunc, logger *slog.Logger) *ReconcilerService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReconcilerService{
		store:   store,
		dump:    dump,
		log:     logger,
		timeout: 3 * time.Minute,
	}
}

// ReconcileSubscribersOnce refreshes subscriber caches for all active creators once.
func (r *ReconcilerService) ReconcileSubscribersOnce(ctx context.Context) error {
	creators, err := r.store.ListActiveCreators(ctx)
	if err != nil {
		r.log.Warn("reconciler ListActiveCreators failed", "error", err)
		return fmt.Errorf("list active creators: %w", errors.Join(ErrListActiveCreators, err))
	}
	var partialErr error
	var partialMu sync.Mutex
	g, runCtx := errgroup.WithContext(ctx)
	g.SetLimit(subscriberReconcileConcurrency)
	for _, creator := range creators {
		g.Go(func() error {
			creatorCtx, cancel := context.WithTimeout(runCtx, r.timeout)
			defer cancel()
			_, err := r.dump(creatorCtx, creator)
			if err == nil {
				return nil
			}
			r.log.Warn("reconciler dumpCurrentSubscribers failed", "creator_id", creator.ID, "error", err)
			partialMu.Lock()
			partialErr = errors.Join(partialErr, fmt.Errorf("creator %s: %w", creator.ID, err))
			partialMu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("reconcile subscriber dumps: %w", err)
	}
	if partialErr != nil {
		return fmt.Errorf("reconcile subscribers: %w", errors.Join(ErrPartialReconcile, partialErr))
	}
	return nil
}
