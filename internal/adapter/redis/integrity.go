package redis

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"imsub/internal/core"

	"github.com/redis/go-redis/v9"
)

const integrityReverseIndexScanCount = 128
const integrityProcessedUsersTTL = 15 * time.Minute

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

	processedKey := keyIntegrityTrackedReverseIndexProcessed(strconv.FormatInt(time.Now().UnixNano(), 10))
	cleanupCtx := context.WithoutCancel(ctx)
	defer func() {
		if delErr := s.rdb.Del(cleanupCtx, processedKey).Err(); delErr != nil {
			s.log().Warn("integrity audit cleanup processed users key failed", "key", processedKey, "error", delErr)
		}
	}()

	userCursor := uint64(0)
	for {
		userIDs, nextCursor, scanErr := s.rdb.SScan(ctx, keyUsersSet(), userCursor, "*", integrityReverseIndexScanCount).Result()
		if scanErr != nil {
			return 0, 0, 0, 0, fmt.Errorf("redis sscan users set: %w", scanErr)
		}
		batchUsers, batchRepaired, batchMissing, batchStale, batchErr := s.repairTrackedGroupReverseIndexUsers(ctx, groupIDs, userIDs, processedKey)
		if batchErr != nil {
			return 0, 0, 0, 0, batchErr
		}
		indexUsers += batchUsers
		repairedUsers += batchRepaired
		missingLinks += batchMissing
		staleLinks += batchStale

		userCursor = nextCursor
		if userCursor == 0 {
			break
		}
	}

	for _, groupID := range groupIDs {
		groupCursor := uint64(0)
		for {
			memberIDs, nextCursor, scanErr := s.rdb.SScan(ctx, keyTrackedGroupMembers(groupID), groupCursor, "*", integrityReverseIndexScanCount).Result()
			if scanErr != nil {
				return 0, 0, 0, 0, fmt.Errorf("redis sscan tracked group members: %w", scanErr)
			}
			batchUsers, batchRepaired, batchMissing, batchStale, batchErr := s.repairTrackedGroupReverseIndexUsers(ctx, groupIDs, memberIDs, processedKey)
			if batchErr != nil {
				return 0, 0, 0, 0, batchErr
			}
			indexUsers += batchUsers
			repairedUsers += batchRepaired
			missingLinks += batchMissing
			staleLinks += batchStale

			groupCursor = nextCursor
			if groupCursor == 0 {
				break
			}
		}
	}

	return indexUsers, repairedUsers, missingLinks, staleLinks, nil
}

// repairTrackedGroupReverseIndexUsers reconciles the reverse-index for a batch
// of users. For each user it reads the reverse-index and checks forward-set
// membership for every managed group, then repairs any mismatches. Users are
// processed in streaming batches via SScan to keep memory bounded rather than
// loading all members at once.
func (s *Store) repairTrackedGroupReverseIndexUsers(ctx context.Context, groupIDs []int64, rawUserIDs []string, processedKey string) (indexUsers int, repairedUsers int, missingLinks int, staleLinks int, err error) {
	if len(rawUserIDs) == 0 {
		return 0, 0, 0, 0, nil
	}

	processedPipe := s.rdb.Pipeline()
	processedCmds := make([]*redis.IntCmd, len(rawUserIDs))
	for i, userID := range rawUserIDs {
		processedCmds[i] = processedPipe.SAdd(ctx, processedKey, userID)
	}
	processedPipe.Expire(ctx, processedKey, integrityProcessedUsersTTL)
	if _, err := processedPipe.Exec(ctx); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("redis exec integrity audit processed users: %w", err)
	}

	validUserIDs := make([]int64, 0, len(rawUserIDs))
	validUserIDRaw := make([]string, 0, len(rawUserIDs))
	for i, userID := range rawUserIDs {
		added, cmdErr := processedCmds[i].Result()
		if cmdErr != nil {
			return 0, 0, 0, 0, fmt.Errorf("redis result integrity audit processed users: %w", cmdErr)
		}
		if added == 0 {
			continue
		}

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

	// Read each user's reverse-index and check forward-set membership in one
	// pipelined round-trip.
	readPipe := s.rdb.Pipeline()
	reverseCmds := make([]*redis.StringSliceCmd, len(validUserIDs))
	memberCmds := make([][]*redis.BoolCmd, len(validUserIDs))
	for i, tgID := range validUserIDs {
		reverseCmds[i] = readPipe.SMembers(ctx, keyUserTrackedGroups(tgID))
		memberCmds[i] = make([]*redis.BoolCmd, len(groupIDs))
		for j, groupID := range groupIDs {
			memberCmds[i][j] = readPipe.SIsMember(ctx, keyTrackedGroupMembers(groupID), validUserIDRaw[i])
		}
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

		wanted := make(map[string]struct{})
		for j, groupID := range groupIDs {
			isMember, memberErr := memberCmds[i][j].Result()
			if memberErr != nil {
				return 0, 0, 0, 0, fmt.Errorf("redis result integrity audit tracked members batch: %w", memberErr)
			}
			if isMember {
				wanted[strconv.FormatInt(groupID, 10)] = struct{}{}
			}
		}

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
		args := make([]any, 0, len(wanted))
		for groupID := range wanted {
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
