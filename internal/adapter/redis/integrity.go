package redis

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"imsub/internal/core"

	"github.com/redis/go-redis/v9"
)

const integrityReverseIndexScanCount = 128

// RepairTrackedGroupReverseIndex audits and repairs tracked-group reverse-index sets.
func (s *Store) RepairTrackedGroupReverseIndex(ctx context.Context) (indexUsers int, repairedUsers int, missingLinks int, staleLinks int, err error) {
	groups, err := s.ListManagedGroups(ctx)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("list managed groups: %w", err)
	}
	if len(groups) == 0 {
		return 0, 0, 0, 0, nil
	}

	groupIDs := make([]int64, 0, len(groups))
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ChatID)
	}
	slices.Sort(groupIDs)

	candidateUsers := make(map[string]struct{})
	wantedByUser := make(map[string]map[string]struct{})

	for _, groupID := range groupIDs {
		groupIDRaw := strconv.FormatInt(groupID, 10)
		groupCursor := uint64(0)
		for {
			memberIDs, nextCursor, scanErr := s.rdb.SScan(ctx, keyTrackedGroupMembers(groupID), groupCursor, "*", integrityReverseIndexScanCount).Result()
			if scanErr != nil {
				return 0, 0, 0, 0, fmt.Errorf("redis sscan tracked group members: %w", scanErr)
			}
			for _, userID := range memberIDs {
				candidateUsers[userID] = struct{}{}
				wanted := wantedByUser[userID]
				if wanted == nil {
					wanted = make(map[string]struct{})
					wantedByUser[userID] = wanted
				}
				wanted[groupIDRaw] = struct{}{}
			}

			groupCursor = nextCursor
			if groupCursor == 0 {
				break
			}
		}
	}

	userCursor := uint64(0)
	for {
		userIDs, nextCursor, scanErr := s.rdb.SScan(ctx, keyUsersSet(), userCursor, "*", integrityReverseIndexScanCount).Result()
		if scanErr != nil {
			return 0, 0, 0, 0, fmt.Errorf("redis sscan users set: %w", scanErr)
		}
		for _, userID := range userIDs {
			candidateUsers[userID] = struct{}{}
		}
		userCursor = nextCursor
		if userCursor == 0 {
			break
		}
	}

	rawUserIDs := make([]string, 0, len(candidateUsers))
	for userID := range candidateUsers {
		rawUserIDs = append(rawUserIDs, userID)
	}
	slices.Sort(rawUserIDs)

	for start := 0; start < len(rawUserIDs); start += integrityReverseIndexScanCount {
		end := min(start+integrityReverseIndexScanCount, len(rawUserIDs))
		batchUsers, batchRepaired, batchMissing, batchStale, batchErr := s.repairTrackedGroupReverseIndexUsers(ctx, rawUserIDs[start:end], wantedByUser)
		if batchErr != nil {
			return 0, 0, 0, 0, batchErr
		}
		indexUsers += batchUsers
		repairedUsers += batchRepaired
		missingLinks += batchMissing
		staleLinks += batchStale
	}

	return indexUsers, repairedUsers, missingLinks, staleLinks, nil
}

// repairTrackedGroupReverseIndexUsers reconciles the reverse-index for a batch of users.
func (s *Store) repairTrackedGroupReverseIndexUsers(ctx context.Context, rawUserIDs []string, wantedByUser map[string]map[string]struct{}) (indexUsers int, repairedUsers int, missingLinks int, staleLinks int, err error) {
	if len(rawUserIDs) == 0 {
		return 0, 0, 0, 0, nil
	}

	validUserIDs := make([]int64, 0, len(rawUserIDs))
	validUserIDRaw := make([]string, 0, len(rawUserIDs))
	for _, userID := range rawUserIDs {
		tgID, parseErr := strconv.ParseInt(userID, 10, 64)
		if parseErr != nil {
			s.log().Warn("integrity audit skipping non-numeric user id", "user_id_raw", userID, "error", parseErr)
			continue
		}
		validUserIDs = append(validUserIDs, tgID)
		validUserIDRaw = append(validUserIDRaw, userID)
	}
	if len(validUserIDs) == 0 {
		return 0, 0, 0, 0, nil
	}

	readPipe := s.rdb.Pipeline()
	reverseCmds := make([]*redis.StringSliceCmd, len(validUserIDs))
	for i, tgID := range validUserIDs {
		reverseCmds[i] = readPipe.SMembers(ctx, keyUserTrackedGroups(tgID))
	}
	if _, err := readPipe.Exec(ctx); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("redis exec integrity audit tracked reverse index batch: %w", err)
	}

	writePipe := s.rdb.TxPipeline()
	needsWrite := false
	for i := range validUserIDRaw {
		currentGroupIDs, cmdErr := reverseCmds[i].Result()
		if cmdErr != nil {
			return 0, 0, 0, 0, fmt.Errorf("redis result integrity audit tracked reverse index batch: %w", cmdErr)
		}

		current := make(map[string]struct{}, len(currentGroupIDs))
		for _, groupID := range currentGroupIDs {
			current[groupID] = struct{}{}
		}

		wanted := wantedByUser[validUserIDRaw[i]]

		userNeedsRepair := false
		for groupID := range wanted {
			if _, ok := current[groupID]; !ok {
				missingLinks++
				userNeedsRepair = true
			}
		}
		for groupID := range current {
			if _, ok := wanted[groupID]; !ok {
				staleLinks++
				userNeedsRepair = true
			}
		}
		if !userNeedsRepair {
			continue
		}

		repairedUsers++
		needsWrite = true
		key := keyUserTrackedGroups(validUserIDs[i])
		writePipe.Del(ctx, key)
		if len(wanted) == 0 {
			continue
		}
		wantedGroupIDs := make([]string, 0, len(wanted))
		for groupID := range wanted {
			wantedGroupIDs = append(wantedGroupIDs, groupID)
		}
		slices.Sort(wantedGroupIDs)
		args := make([]any, 0, len(wantedGroupIDs))
		for _, groupID := range wantedGroupIDs {
			args = append(args, groupID)
		}
		writePipe.SAdd(ctx, key, args...)
	}
	if needsWrite {
		if _, err := writePipe.Exec(ctx); err != nil {
			return 0, 0, 0, 0, fmt.Errorf("redis exec integrity audit tracked reverse-index repair: %w", err)
		}
	}

	return len(validUserIDs), repairedUsers, missingLinks, staleLinks, nil
}

// ActiveCreatorIDsWithoutGroup counts creators that have no managed groups.
func (s *Store) ActiveCreatorIDsWithoutGroup(ctx context.Context, creators []core.Creator) (int, error) {
	groups, err := s.ListManagedGroups(ctx)
	if err != nil {
		return 0, fmt.Errorf("list managed groups: %w", err)
	}
	managedByCreator := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		managedByCreator[group.CreatorID] = struct{}{}
	}

	count := 0
	for _, creator := range creators {
		if _, ok := managedByCreator[creator.ID]; !ok {
			count++
		}
	}
	return count, nil
}
