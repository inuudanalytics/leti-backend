package cache

import (
	"context"
	"leti_server/pkg/utils"
)

const usernameBloomKey = "bloom:usernames"

func SeedUsernameBloom(ctx context.Context, usernames []string) error {
	if len(usernames) == 0 {
		return nil
	}

	pipe := RDB.Pipeline()
	for _, u := range usernames {
		pipe.Do(ctx, "BF.ADD", usernameBloomKey, u)
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		utils.Logger.Errorf("failed to seed username bloom filter: %v", err)
		return err
	}

	utils.Logger.Infof("Seeded %d usernames into bloom filter", len(usernames))
	return nil
}

// Returns true if username MIGHT exist, false if it definitely does not
func UsernameBloomCheck(ctx context.Context, username string) (bool, error) {
	result, err := RDB.Do(ctx, "BF.EXISTS", usernameBloomKey, username).Bool()
	if err != nil {
		utils.Logger.Warnf("bloom filter check failed, falling back to DB: %v", err)
		return true, err
	}
	return result, nil
}

// Call after successfully saving a new username to DB
func UsernameBloomAdd(ctx context.Context, username string) {
	if err := RDB.Do(ctx, "BF.ADD", usernameBloomKey, username).Err(); err != nil {
		utils.Logger.Warnf("failed to add username to bloom filter: %v", err)
	}
}
