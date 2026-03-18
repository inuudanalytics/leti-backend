DB_URL ?= $(DATABASE_URL)

MIGRATIONS_PATH=./migrations

.PHONY: migrate-up migrate-down migrate-create migrate-force migrate-version

migrate-up:
	migrate -path $(MIGRATIONS_PATH) -database "$(DB_URL)" up

migrate-down:
	migrate -path $(MIGRATIONS_PATH) -database "$(DB_URL)" down

migrate-down-1:
	migrate -path $(MIGRATIONS_PATH) -database "$(DB_URL)" down 1

migrate-force:
	migrate -path $(MIGRATIONS_PATH) -database "$(DB_URL)" force $(VERSION)

migrate-version:
	migrate -path $(MIGRATIONS_PATH) -database "$(DB_URL)" version

migrate-create:
	migrate create -ext sql -dir $(MIGRATIONS_PATH) -seq $(NAME)