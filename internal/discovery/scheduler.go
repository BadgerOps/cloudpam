package discovery

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func (s *SyncService) logError(message string, err error) {
	slog.Default().Error(message, "error", err)
}

// RunScheduler runs until canceled; callers should join before closing storage.
func (s *SyncService) RunScheduler(ctx context.Context, accounts storage.Store) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.runScheduled(ctx, accounts, time.Now().UTC()); err != nil && ctx.Err() == nil {
			s.logError("scheduled discovery", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *SyncService) runScheduled(ctx context.Context, accounts storage.Store, now time.Time) error {
	schedules, ok := s.store.(storage.SyncScheduleStore)
	if !ok {
		return errors.New("discovery schedules unavailable")
	}
	due, err := schedules.DueSyncSchedules(ctx, now)
	if err != nil {
		return err
	}
	for _, schedule := range due {
		if err := ctx.Err(); err != nil {
			return err
		}
		account, found, err := accounts.GetAccount(ctx, schedule.AccountID)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if _, ok := s.collectors[account.Provider]; !ok {
			continue
		}
		agents, err := s.store.ListAgents(ctx, 0)
		if err != nil {
			return err
		}
		if agentOwnsAccount(agents, account, schedule.LastAgentID, time.Now().UTC()) {
			continue
		}
		if _, err := s.sync(ctx, account, true); err != nil && !errors.Is(err, ErrSyncBusy) && ctx.Err() == nil {
			s.logError("scheduled sync failed", err)
		}
	}
	return nil
}
func agentOwnsAccount(agents []domain.DiscoveryAgent, account domain.Account, lastAgentID string, now time.Time) bool {
	for _, a := range agents {
		if a.ApprovalStatus != "" && a.ApprovalStatus != domain.AgentApprovalApproved {
			continue
		}
		if now.Sub(a.LastSeenAt) >= 5*time.Minute {
			continue
		}
		if a.ID.String() == lastAgentID || a.AccountID == account.ID {
			return true
		}
		if account.Provider == "aws" && a.AccountID > 0 && (account.Key == "aws:"+strconv.FormatInt(a.AccountID, 10) || account.ExternalID == strconv.FormatInt(a.AccountID, 10)) {
			return true
		}
	}
	return false
}
