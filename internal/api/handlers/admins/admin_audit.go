package admins

import (
	"context"
	"encoding/json"
	"net/http"

	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func logAudit(
	ctx context.Context,
	db *pgxpool.Pool,
	adminID uuid.UUID,
	action string,
	entityType string,
	entityID *uuid.UUID,
	metadata interface{},
	r *http.Request,
) {
	var metaBytes []byte
	if metadata != nil {
		metaBytes, _ = json.Marshal(metadata)
	}

	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}

	_, err := db.Exec(ctx,
		`INSERT INTO admin_audit_logs (admin_id, action, entity_type, entity_id, metadata, ip_address)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		adminID, action, entityType, entityID, metaBytes, ip,
	)
	if err != nil {
		utils.Logger.Errorf("audit log write failed [%s]: %v", action, err)
	}
}
