package cache

import (
	"context"
	"os"
	"time"

	"leti_server/pkg/utils"

	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client

func InitRedis() error {
	redisURL := os.Getenv("REDIS_URL")

	var opts *redis.Options
	var err error

	if redisURL != "" {
		opts, err = redis.ParseURL(redisURL)
		if err != nil {
			return err
		}
	} else {
		opts = &redis.Options{
			Addr:     os.Getenv("REDIS_ADDR"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       0,
		}
	}

	RDB = redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := RDB.Ping(ctx).Err(); err != nil {
		return err
	}

	utils.Logger.Infof("Connected to Redis (%s)", opts.Addr)
	return nil
}
