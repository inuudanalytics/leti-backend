package profilesettings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"leti_server/internal/api/handlers"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// Shared types
// ============================================================================

type Address struct {
	ID          uuid.UUID `json:"id"`
	OwnerID     uuid.UUID `json:"owner_id"`
	AddressType string    `json:"address_type"`
	Label       *string   `json:"label,omitempty"`
	Street      string    `json:"street"`
	City        string    `json:"city"`
	State       string    `json:"state"`
	Country     string    `json:"country"`
	Latitude    *float64  `json:"latitude,omitempty"`
	Longitude   *float64  `json:"longitude,omitempty"`
	IsPrimary   bool      `json:"is_primary"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// addrConfig holds the table name and column name for a role's address table.
type addrConfig struct {
	table          string // e.g. "artisan_address"
	ownerCol       string // e.g. "artisan_id"
	role           string // e.g. "artisan"
	homeConstraint string // unique constraint name for duplicate home check
	allowedTypes   []string
	// allowWorkPrimary bool // only artisan "work" addresses support a separate primary flag
}

var addrConfigs = map[string]addrConfig{
	"artisan": {
		table:          "artisan_address",
		ownerCol:       "artisan_id",
		role:           "artisan",
		homeConstraint: "one_home_address_per_artisan",
		allowedTypes:   []string{"home", "work"},
	},
	"client": {
		table:          "client_address",
		ownerCol:       "client_id",
		role:           "client",
		homeConstraint: "one_home_address_per_client",
		allowedTypes:   []string{"home", "work"},
	},
	"owner": {
		table:          "owner_address",
		ownerCol:       "owner_id",
		role:           "owner",
		homeConstraint: "one_home_address_per_owner",
		allowedTypes:   []string{"home", "work"},
	},
}

// ============================================================================
// POST /profile/artisan/address
// ============================================================================

// AddArtisanAddress godoc
// @Summary      Add artisan address
// @Description  Adds a home or work address for the authenticated artisan. Only one home address is allowed (enforced by DB constraint). Multiple work addresses are allowed; the first work address is automatically set as primary.
// @Tags         Artisan Address
// @Accept       json
// @Produce      json
// @Param        body  body  object{address_type=string,label=string,street=string,city=string,state=string,country=string,latitude=number,longitude=number,is_primary=bool}  true  "Address payload. address_type must be 'home' or 'work'. is_primary only applies to work addresses."
// @Success      201   {object}  object{status=string,message=string,data=Address}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /profile/artisan/address [post]
// @Security     BearerAuth
func AddArtisanAddress(w http.ResponseWriter, r *http.Request) {
	addAddress(w, r, addrConfigs["artisan"])
}

// ============================================================================
// GET /profile/artisan/address
// ============================================================================

// GetArtisanAddresses godoc
// @Summary      Get artisan addresses
// @Description  Returns all addresses for the authenticated artisan.
// @Tags         Artisan Address
// @Produce      json
// @Success      200  {object}  object{status=string,count=int,data=[]Address}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /profile/artisan/address [get]
// @Security     BearerAuth
func GetArtisanAddresses(w http.ResponseWriter, r *http.Request) {
	getAddresses(w, r, addrConfigs["artisan"])
}

// ============================================================================
// PATCH /profile/artisan/address/{id}
// ============================================================================

// UpdateArtisanAddress godoc
// @Summary      Update artisan address
// @Description  Partially updates a specific address for the authenticated artisan. Only provided fields are updated. Coordinates are updated together — both latitude and longitude must be provided if updating location.
// @Tags         Artisan Address
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Address UUID"
// @Param        body  body  object{label=string,street=string,city=string,state=string,country=string,latitude=number,longitude=number}  true  "Fields to update (all optional)"
// @Success      200  {object}  object{status=string,message=string,data=Address}
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /profile/artisan/address/{id} [patch]
// @Security     BearerAuth
func UpdateArtisanAddress(w http.ResponseWriter, r *http.Request) {
	updateAddress(w, r, addrConfigs["artisan"])
}

// ============================================================================
// DELETE /profile/artisan/address/{id}
// ============================================================================

// DeleteArtisanAddress godoc
// @Summary      Delete artisan address
// @Description  Deletes a specific address for the authenticated artisan. Cannot delete a primary work address if other work addresses exist — set another as primary first.
// @Tags         Artisan Address
// @Produce      json
// @Param        id  path  string  true  "Address UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /profile/artisan/address/{id} [delete]
// @Security     BearerAuth
func DeleteArtisanAddress(w http.ResponseWriter, r *http.Request) {
	deleteAddress(w, r, addrConfigs["artisan"])
}

// ============================================================================
// PATCH /profile/artisan/address/{id}/primary
// ============================================================================

// SetArtisanPrimaryAddress godoc
// @Summary      Set artisan primary work address
// @Description  Sets a work address as the primary work address for the authenticated artisan. Only work addresses can be set as primary.
// @Tags         Artisan Address
// @Produce      json
// @Param        id  path  string  true  "Address UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /profile/artisan/address/{id}/primary [patch]
// @Security     BearerAuth
func SetArtisanPrimaryAddress(w http.ResponseWriter, r *http.Request) {
	setPrimaryAddress(w, r, addrConfigs["artisan"])
}

// ============================================================================
// POST /profile/client/address
// ============================================================================

// AddClientAddress godoc
// @Summary      Add client address
// @Description  Adds a home or work address for the authenticated client. Only one home address is allowed (enforced by DB constraint). Multiple work addresses are allowed; the first work address is automatically set as primary.
// @Tags         Client Address
// @Accept       json
// @Produce      json
// @Param        body  body  object{address_type=string,label=string,street=string,city=string,state=string,country=string,latitude=number,longitude=number,is_primary=bool}  true  "Address payload. address_type must be 'home' or 'work'. is_primary only applies to work addresses."
// @Success      201   {object}  object{status=string,message=string,data=Address}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /profile/client/address [post]
// @Security     BearerAuth
func AddClientAddress(w http.ResponseWriter, r *http.Request) {
	addAddress(w, r, addrConfigs["client"])
}

// ============================================================================
// GET /profile/client/address
// ============================================================================

// GetClientAddresses godoc
// @Summary      Get client addresses
// @Description  Returns all addresses for the authenticated client.
// @Tags         Client Address
// @Produce      json
// @Success      200  {object}  object{status=string,count=int,data=[]Address}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /profile/client/address [get]
// @Security     BearerAuth
func GetClientAddresses(w http.ResponseWriter, r *http.Request) {
	getAddresses(w, r, addrConfigs["client"])
}

// ============================================================================
// PATCH /profile/client/address/{id}
// ============================================================================

// UpdateClientAddress godoc
// @Summary      Update client address
// @Description  Partially updates a specific address for the authenticated client.
// @Tags         Client Address
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Address UUID"
// @Param        body  body  object{label=string,street=string,city=string,state=string,country=string,latitude=number,longitude=number}  true  "Fields to update (all optional)"
// @Success      200  {object}  object{status=string,message=string,data=Address}
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /profile/client/address/{id} [patch]
// @Security     BearerAuth
func UpdateClientAddress(w http.ResponseWriter, r *http.Request) {
	updateAddress(w, r, addrConfigs["client"])
}

// ============================================================================
// DELETE /profile/client/address/{id}
// ============================================================================

// DeleteClientAddress godoc
// @Summary      Delete client address
// @Description  Deletes a specific address for the authenticated client. Cannot delete a primary work address if other work addresses exist — set another as primary first.
// @Tags         Client Address
// @Produce      json
// @Param        id  path  string  true  "Address UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /profile/client/address/{id} [delete]
// @Security     BearerAuth
func DeleteClientAddress(w http.ResponseWriter, r *http.Request) {
	deleteAddress(w, r, addrConfigs["client"])
}

// ============================================================================
// PATCH /profile/client/address/{id}/primary
// ============================================================================

// SetClientPrimaryAddress godoc
// @Summary      Set client primary work address
// @Description  Sets a work address as the primary work address for the authenticated client. Only work addresses can be set as primary.
// @Tags         Client Address
// @Produce      json
// @Param        id  path  string  true  "Address UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /profile/client/address/{id}/primary [patch]
// @Security     BearerAuth
func SetClientPrimaryAddress(w http.ResponseWriter, r *http.Request) {
	setPrimaryAddress(w, r, addrConfigs["client"])
}

// ============================================================================
// POST /profile/owner/address
// ============================================================================

// AddOwnerAddress godoc
// @Summary      Add owner address
// @Description  Adds a home or work address for the authenticated owner. Only one home address is allowed (enforced by DB constraint). Multiple work addresses are allowed; the first work address is automatically set as primary.
// @Tags         Owner Address
// @Accept       json
// @Produce      json
// @Param        body  body  object{address_type=string,label=string,street=string,city=string,state=string,country=string,latitude=number,longitude=number,is_primary=bool}  true  "Address payload. address_type must be 'home' or 'work'. is_primary only applies to work addresses."
// @Success      201   {object}  object{status=string,message=string,data=Address}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /profile/owner/address [post]
// @Security     BearerAuth
func AddOwnerAddress(w http.ResponseWriter, r *http.Request) {
	addAddress(w, r, addrConfigs["owner"])
}

// ============================================================================
// GET /profile/owner/address
// ============================================================================

// GetOwnerAddresses godoc
// @Summary      Get owner addresses
// @Description  Returns all addresses for the authenticated owner.
// @Tags         Owner Address
// @Produce      json
// @Success      200  {object}  object{status=string,count=int,data=[]Address}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /profile/owner/address [get]
// @Security     BearerAuth
func GetOwnerAddresses(w http.ResponseWriter, r *http.Request) {
	getAddresses(w, r, addrConfigs["owner"])
}

// ============================================================================
// PATCH /profile/owner/address/{id}
// ============================================================================

// UpdateOwnerAddress godoc
// @Summary      Update owner address
// @Description  Partially updates a specific address for the authenticated owner.
// @Tags         Owner Address
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Address UUID"
// @Param        body  body  object{label=string,street=string,city=string,state=string,country=string,latitude=number,longitude=number}  true  "Fields to update (all optional)"
// @Success      200  {object}  object{status=string,message=string,data=Address}
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /profile/owner/address/{id} [patch]
// @Security     BearerAuth
func UpdateOwnerAddress(w http.ResponseWriter, r *http.Request) {
	updateAddress(w, r, addrConfigs["owner"])
}

// ============================================================================
// DELETE /profile/owner/address/{id}
// ============================================================================

// DeleteOwnerAddress godoc
// @Summary      Delete owner address
// @Description  Deletes a specific address for the authenticated owner. Cannot delete a primary work address if other work addresses exist — set another as primary first.
// @Tags         Owner Address
// @Produce      json
// @Param        id  path  string  true  "Address UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /profile/owner/address/{id} [delete]
// @Security     BearerAuth
func DeleteOwnerAddress(w http.ResponseWriter, r *http.Request) {
	deleteAddress(w, r, addrConfigs["owner"])
}

// ============================================================================
// PATCH /profile/owner/address/{id}/primary
// ============================================================================

// SetOwnerPrimaryAddress godoc
// @Summary      Set owner primary work address
// @Description  Sets a work address as the primary work address for the authenticated owner. Only work addresses can be set as primary.
// @Tags         Owner Address
// @Produce      json
// @Param        id  path  string  true  "Address UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /profile/owner/address/{id}/primary [patch]
// @Security     BearerAuth
func SetOwnerPrimaryAddress(w http.ResponseWriter, r *http.Request) {
	setPrimaryAddress(w, r, addrConfigs["owner"])
}

// ============================================================================
// Internal shared implementations
// ============================================================================

func addAddress(w http.ResponseWriter, r *http.Request, cfg addrConfig) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != cfg.role {
		utils.WriteError(w, fmt.Sprintf("only %ss can manage addresses", cfg.role), http.StatusForbidden)
		return
	}

	type request struct {
		AddressType string   `json:"address_type"`
		Label       string   `json:"label"`
		Street      string   `json:"street"`
		City        string   `json:"city"`
		State       string   `json:"state"`
		Country     string   `json:"country"`
		Latitude    *float64 `json:"latitude"`
		Longitude   *float64 `json:"longitude"`
		IsPrimary   bool     `json:"is_primary"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	req.AddressType = strings.TrimSpace(req.AddressType)
	req.Street = strings.TrimSpace(req.Street)
	req.City = strings.TrimSpace(req.City)
	req.State = strings.TrimSpace(req.State)

	if req.AddressType != "home" && req.AddressType != "work" {
		utils.WriteError(w, "address_type must be 'home' or 'work'", http.StatusBadRequest)
		return
	}
	if req.Street == "" {
		utils.WriteError(w, "street is required", http.StatusBadRequest)
		return
	}
	if req.City == "" {
		utils.WriteError(w, "city is required", http.StatusBadRequest)
		return
	}
	if req.State == "" {
		utils.WriteError(w, "state is required", http.StatusBadRequest)
		return
	}
	if req.Country == "" {
		req.Country = "Nigeria"
	}
	if req.Latitude != nil && (*req.Latitude < -90 || *req.Latitude > 90) {
		utils.WriteError(w, "latitude must be between -90 and 90", http.StatusBadRequest)
		return
	}
	if req.Longitude != nil && (*req.Longitude < -180 || *req.Longitude > 180) {
		utils.WriteError(w, "longitude must be between -180 and 180", http.StatusBadRequest)
		return
	}

	// Home addresses never get the primary flag — primary only applies to work
	if req.AddressType == "home" {
		req.IsPrimary = false
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	// If adding a primary work address, demote all existing work addresses first
	if req.AddressType == "work" && req.IsPrimary {
		_, err = tx.Exec(ctx, fmt.Sprintf(`
			UPDATE %s SET is_primary = FALSE
			WHERE %s = $1 AND address_type = 'work'
		`, cfg.table, cfg.ownerCol), userID)
		if err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	// Auto-set primary if this is the first work address
	if req.AddressType == "work" && !req.IsPrimary {
		var workCount int
		_ = tx.QueryRow(ctx, fmt.Sprintf(`
			SELECT COUNT(*) FROM %s WHERE %s = $1 AND address_type = 'work'
		`, cfg.table, cfg.ownerCol), userID).Scan(&workCount)
		if workCount == 0 {
			req.IsPrimary = true
		}
	}

	var saved Address
	err = tx.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO %s
			(%s, address_type, label, street, city, state, country, is_primary, location)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			CASE
				WHEN $9::float8 IS NOT NULL AND $10::float8 IS NOT NULL
				THEN ST_SetSRID(ST_MakePoint($9, $10), 4326)
				ELSE NULL
			END
		)
		RETURNING id, %s, address_type, label, street, city, state, country,
		          ST_Y(location::geometry) AS latitude,
		          ST_X(location::geometry) AS longitude,
		          is_primary, created_at, updated_at
	`, cfg.table, cfg.ownerCol, cfg.ownerCol),
		userID,
		req.AddressType,
		handlers.NullableString(strings.TrimSpace(req.Label)),
		req.Street,
		req.City,
		req.State,
		req.Country,
		req.IsPrimary,
		req.Longitude, // ST_MakePoint(lng, lat) — X axis first
		req.Latitude,
	).Scan(
		&saved.ID, &saved.OwnerID, &saved.AddressType, &saved.Label,
		&saved.Street, &saved.City, &saved.State, &saved.Country,
		&saved.Latitude, &saved.Longitude,
		&saved.IsPrimary, &saved.CreatedAt, &saved.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), cfg.homeConstraint) {
			utils.WriteError(w, "you already have a home address — update it instead", http.StatusConflict)
			return
		}
		utils.Logger.Errorf("failed to add %s address: %v", cfg.role, err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "address added successfully",
		"data":    saved,
	})
}

func getAddresses(w http.ResponseWriter, r *http.Request, cfg addrConfig) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != cfg.role {
		utils.WriteError(w, fmt.Sprintf("only %ss can view their addresses", cfg.role), http.StatusForbidden)
		return
	}

	rows, err := db.Query(r.Context(), fmt.Sprintf(`
		SELECT id, %s, address_type, label, street, city, state, country,
		       ST_Y(location::geometry) AS latitude,
		       ST_X(location::geometry) AS longitude,
		       is_primary, created_at, updated_at
		FROM %s
		WHERE %s = $1
		ORDER BY address_type ASC, is_primary DESC, created_at ASC
	`, cfg.ownerCol, cfg.table, cfg.ownerCol), userID)
	if err != nil {
		utils.Logger.Errorf("failed to fetch %s addresses: %v", cfg.role, err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	addresses := make([]Address, 0)
	for rows.Next() {
		var a Address
		if err := rows.Scan(
			&a.ID, &a.OwnerID, &a.AddressType, &a.Label,
			&a.Street, &a.City, &a.State, &a.Country,
			&a.Latitude, &a.Longitude,
			&a.IsPrimary, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		addresses = append(addresses, a)
	}
	if err := rows.Err(); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(addresses),
		"data":   addresses,
	})
}

func updateAddress(w http.ResponseWriter, r *http.Request, cfg addrConfig) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != cfg.role {
		utils.WriteError(w, fmt.Sprintf("only %ss can manage addresses", cfg.role), http.StatusForbidden)
		return
	}

	addressID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid address id", http.StatusBadRequest)
		return
	}

	type request struct {
		Label     *string  `json:"label"`
		Street    *string  `json:"street"`
		City      *string  `json:"city"`
		State     *string  `json:"state"`
		Country   *string  `json:"country"`
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Street != nil && strings.TrimSpace(*req.Street) == "" {
		utils.WriteError(w, "street cannot be empty", http.StatusBadRequest)
		return
	}
	if req.City != nil && strings.TrimSpace(*req.City) == "" {
		utils.WriteError(w, "city cannot be empty", http.StatusBadRequest)
		return
	}
	if req.State != nil && strings.TrimSpace(*req.State) == "" {
		utils.WriteError(w, "state cannot be empty", http.StatusBadRequest)
		return
	}
	if req.Latitude != nil && (*req.Latitude < -90 || *req.Latitude > 90) {
		utils.WriteError(w, "latitude must be between -90 and 90", http.StatusBadRequest)
		return
	}
	if req.Longitude != nil && (*req.Longitude < -180 || *req.Longitude > 180) {
		utils.WriteError(w, "longitude must be between -180 and 180", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var updated Address
	err = db.QueryRow(ctx, fmt.Sprintf(`
		UPDATE %s
		SET
			label      = COALESCE($1, label),
			street     = COALESCE($2, street),
			city       = COALESCE($3, city),
			state      = COALESCE($4, state),
			country    = COALESCE($5, country),
			location   = CASE
							WHEN $6::float8 IS NOT NULL AND $7::float8 IS NOT NULL
							THEN ST_SetSRID(ST_MakePoint($6, $7), 4326)
							ELSE location
						 END,
			updated_at = NOW()
		WHERE id = $8 AND %s = $9
		RETURNING id, %s, address_type, label, street, city, state, country,
		          ST_Y(location::geometry) AS latitude,
		          ST_X(location::geometry) AS longitude,
		          is_primary, created_at, updated_at
	`, cfg.table, cfg.ownerCol, cfg.ownerCol),
		req.Label,
		req.Street,
		req.City,
		req.State,
		req.Country,
		req.Longitude, // ST_MakePoint(lng, lat) — X axis first
		req.Latitude,
		addressID,
		userID,
	).Scan(
		&updated.ID, &updated.OwnerID, &updated.AddressType, &updated.Label,
		&updated.Street, &updated.City, &updated.State, &updated.Country,
		&updated.Latitude, &updated.Longitude,
		&updated.IsPrimary, &updated.CreatedAt, &updated.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "address not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to update %s address: %v", cfg.role, err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "address updated successfully",
		"data":    updated,
	})
}

func deleteAddress(w http.ResponseWriter, r *http.Request, cfg addrConfig) {
	if r.Method != http.MethodDelete {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != cfg.role {
		utils.WriteError(w, fmt.Sprintf("only %ss can manage addresses", cfg.role), http.StatusForbidden)
		return
	}

	addressID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid address id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var addressType string
	var isPrimary bool
	err = db.QueryRow(ctx, fmt.Sprintf(`
		SELECT address_type, is_primary FROM %s WHERE id = $1 AND %s = $2
	`, cfg.table, cfg.ownerCol), addressID, userID).Scan(&addressType, &isPrimary)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "address not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Block deleting a primary work address if other work addresses exist
	if addressType == "work" && isPrimary {
		var workCount int
		_ = db.QueryRow(ctx, fmt.Sprintf(`
			SELECT COUNT(*) FROM %s WHERE %s = $1 AND address_type = 'work'
		`, cfg.table, cfg.ownerCol), userID).Scan(&workCount)
		if workCount > 1 {
			utils.WriteError(w,
				"set another work address as primary before deleting this one",
				http.StatusConflict,
			)
			return
		}
	}

	_, err = db.Exec(ctx, fmt.Sprintf(`
		DELETE FROM %s WHERE id = $1 AND %s = $2
	`, cfg.table, cfg.ownerCol), addressID, userID)
	if err != nil {
		utils.Logger.Errorf("failed to delete %s address: %v", cfg.role, err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "address deleted successfully",
	})
}

func setPrimaryAddress(w http.ResponseWriter, r *http.Request, cfg addrConfig) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != cfg.role {
		utils.WriteError(w, fmt.Sprintf("only %ss can manage addresses", cfg.role), http.StatusForbidden)
		return
	}

	addressID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid address id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var addressType string
	err = db.QueryRow(ctx, fmt.Sprintf(`
		SELECT address_type FROM %s WHERE id = $1 AND %s = $2
	`, cfg.table, cfg.ownerCol), addressID, userID).Scan(&addressType)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "address not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if addressType != "work" {
		utils.WriteError(w, "only work addresses can be set as primary", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, fmt.Sprintf(`
		UPDATE %s SET is_primary = FALSE WHERE %s = $1 AND address_type = 'work'
	`, cfg.table, cfg.ownerCol), userID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(ctx, fmt.Sprintf(`
		UPDATE %s SET is_primary = TRUE, updated_at = NOW() WHERE id = $1 AND %s = $2
	`, cfg.table, cfg.ownerCol), addressID, userID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "primary work address updated",
	})
}
