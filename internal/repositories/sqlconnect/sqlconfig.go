package sqlconnect

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var DB *pgxpool.Pool

func ConnectDb() error {
	if DB != nil {
		return nil
	}

	dsn := os.Getenv("DATABASE_URL")

	if dsn == "" {
		user := os.Getenv("DB_USER")
		password := os.Getenv("DB_PASSWORD")
		dbname := os.Getenv("DB_NAME")
		port := os.Getenv("DB_PORT")
		host := os.Getenv("DB_HOST")
		sslmode := os.Getenv("DB_SSLMODE")

		if sslmode == "" {
			sslmode = "disable"
		}

		dsn = fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=%s",
			user,
			password,
			host,
			port,
			dbname,
			sslmode,
		)
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("failed to parse DB config: %w", err)
	}

	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 30 * time.Minute
	config.HealthCheckPeriod = time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create DB pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping DB: %w", err)
	}

	// var currentDB string
	// err = pool.QueryRow(ctx, "SELECT current_database()").Scan(&currentDB)
	// if err == nil {
	// 	fmt.Println("Actually connected to:", currentDB)
	// }

	DB = pool
	fmt.Println("Connected to Database")
	return nil
}

func CloseDb() {
	if DB != nil {
		DB.Close()
		fmt.Println("🛑 Database connection closed")
	}
}
