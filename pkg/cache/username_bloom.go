package cache

import (
	"context"
	"leti_server/pkg/utils"
)

const usernameSetKey = "set:usernames"

func SeedUsernameBloom(ctx context.Context, usernames []string) error {
	if len(usernames) == 0 {
		return nil
	}

	pipe := RDB.Pipeline()
	for _, u := range usernames {
		pipe.SAdd(ctx, usernameSetKey, u)
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		utils.Logger.Errorf("failed to seed username set: %v", err)
		return err
	}

	utils.Logger.Infof("Seeded %d usernames into set", len(usernames))
	return nil
}

// Returns true if username MIGHT exist, false if it definitely does not
func UsernameBloomCheck(ctx context.Context, username string) (bool, error) {
	result, err := RDB.SIsMember(ctx, usernameSetKey, username).Result()
	if err != nil {
		utils.Logger.Warnf("username set check failed, falling back to DB: %v", err)
		return true, err
	}
	return result, nil
}

// Call after successfully saving a new username to DB
func UsernameBloomAdd(ctx context.Context, username string) {
	if err := RDB.SAdd(ctx, usernameSetKey, username).Err(); err != nil {
		utils.Logger.Warnf("failed to add username to set: %v", err)
	}
}
