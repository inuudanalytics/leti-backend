package sqlconnect

import (
	"context"
	"time"
)

func GetAllUsernames(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rows, err := DB.Query(ctx, `SELECT username FROM users WHERE username IS NOT NULL AND deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var usernames []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			continue
		}
		usernames = append(usernames, u)
	}
	return usernames, rows.Err()
}
