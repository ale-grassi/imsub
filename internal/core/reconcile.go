package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type dumpFunc func(ctx context.Context, creator Creator) (int, error)

var (
	// ErrListActiveCreators reports that listing active creators failed.
	ErrListActiveCreators = errors.New("list active creators failed")
	// ErrPartialReconcile reports that at least one creator reconciliation failed.
	ErrPartialReconcile = errors.New("partial reconcile failure")
)

type reconcilerStore interface {
	ListActiveCreators(ctx context.Context) ([]Creator, error)
}

// Reconciler periodically refreshes subscriber caches for active creators.
type Reconciler struct {
	store   reconcilerStore
	dump    dumpFunc
	log     *slog.Logger
	timeout time.Duration
}

// NewReconciler creates a Reconciler with default timeout settings.
func NewReconciler(store reconcilerStore, dump dumpFunc, logger *slog.Logger) *Reconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{
		store:   store,
		dump:    dump,
		log:     logger,
		timeout: 3 * time.Minute,
	}
}

// ReconcileSubscribersOnce refreshes subscriber caches for all active creators once.
func (r *Reconciler) ReconcileSubscribersOnce(ctx context.Context) error {
	creators, err := r.store.ListActiveCreators(ctx)
	if err != nil {
		r.log.Warn("reconciler ListActiveCreators failed", "error", err)
		return fmt.Errorf("list active creators: %w", errors.Join(ErrListActiveCreators, err))
	}
	var partialErr error
	for _, creator := range creators {
		runCtx, cancel := context.WithTimeout(ctx, r.timeout)
		_, err := r.dump(runCtx, creator)
		cancel()
		if err != nil {
			r.log.Warn("reconciler dumpCurrentSubscribers failed", "creator_id", creator.ID, "error", err)
			partialErr = errors.Join(partialErr, fmt.Errorf("creator %s: %w", creator.ID, err))
		}
	}
	if partialErr != nil {
		return fmt.Errorf("reconcile subscribers: %w", errors.Join(ErrPartialReconcile, partialErr))
	}
	return nil
}
