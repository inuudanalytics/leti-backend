package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"leti_server/internal/api/services/notifications"
	"leti_server/internal/models/chat"
	shortletModels "leti_server/internal/models/shortlet"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/cache"
	"leti_server/pkg/utils"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func IsEmpty(s *string) bool {
	return s == nil || *s == ""
}

func CheckFieldNames(model interface{}) []string {
	val := reflect.TypeOf(model)
	fields := []string{}

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldToAdd := strings.TrimSuffix(field.Tag.Get("json"), ",omitempty")
		fields = append(fields, fieldToAdd)
	}
	return fields
}

func CheckBlankFields(value interface{}) error {
	val := reflect.ValueOf(value)
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		if field.Kind() == reflect.String && field.String() == "" {
			return utils.ErrorHandler(errors.New("all fields are required"), "all fields are required")
		}
	}
	return nil
}

// UploadFilesConcurrently uploads image files in parallel.
// Returns: URLs, PublicIDs, and error.
func UploadFilesConcurrently(
	ctx context.Context,
	cld *utils.CloudinaryService,
	files []utils.UploadFile,
	folder string,
) ([]string, []string, error) {
	if len(files) == 0 {
		return []string{}, []string{}, nil
	}

	const maxConcurrency = 10
	semaphore := make(chan struct{}, maxConcurrency)

	type uploadResult struct {
		url      string
		publicID string
		err      error
		index    int
	}

	resultChan := make(chan uploadResult, len(files))

	for i, f := range files {
		fileIndex := i
		file := f

		go func() {
			semaphore <- struct{}{}
			defer func() {
				<-semaphore
				if r := recover(); r != nil {
					resultChan <- uploadResult{
						err:   fmt.Errorf("panic recovered: %v", r),
						index: fileIndex,
					}
				}
			}()

			if !utils.IsAllowedImageExt(file.Filename) {
				resultChan <- uploadResult{
					err:   fmt.Errorf("invalid image format for %q: allowed formats are jpg, jpeg, png, webp, heic, heif", file.Filename),
					index: fileIndex,
				}
				return
			}

			res, err := cld.GetCloudinary().Upload.Upload(ctx, file.Reader, uploader.UploadParams{
				Folder:       folder,
				ResourceType: "image",
			})
			if err != nil {
				resultChan <- uploadResult{
					err:   fmt.Errorf("upload failed for %q: %w", file.Filename, err),
					index: fileIndex,
				}
				return
			}

			resultChan <- uploadResult{
				url:      res.SecureURL,
				publicID: res.PublicID,
				index:    fileIndex,
			}
		}()
	}

	results := make([]uploadResult, len(files))
	for range files {
		select {
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("upload cancelled or timed out")
		case result := <-resultChan:
			results[result.index] = result
		}
	}

	var urls []string
	var publicIDs []string
	var firstErr error

	for _, result := range results {
		if result.err != nil && firstErr == nil {
			firstErr = result.err
		}
		if result.url != "" {
			urls = append(urls, result.url)
			publicIDs = append(publicIDs, result.publicID)
		}
	}

	if firstErr != nil {
		return urls, publicIDs, firstErr
	}

	return urls, publicIDs, nil
}

// CleanupUploads removes uploaded images from Cloudinary if a subsequent DB operation fails.
func CleanupUploads(ctx context.Context, cld *utils.CloudinaryService, publicIDs []string) {
	for _, publicID := range publicIDs {
		if err := cld.DeleteImage(ctx, publicID); err != nil {
			utils.Logger.Warnf("failed to cleanup image %q: %v", publicID, err)
		}
	}
}

// ExtractPublicIDFromURL extracts the Cloudinary public ID from a secure URL.
// Example: https://res.cloudinary.com/cloud/image/upload/v123/jobs/images/abc123.jpg → jobs/images/abc123
func ExtractPublicIDFromURL(url string) string {
	parts := strings.Split(url, "/")
	for i, part := range parts {
		if part == "upload" && i+2 < len(parts) {
			publicIDWithExt := strings.Join(parts[i+2:], "/")
			if lastDot := strings.LastIndex(publicIDWithExt, "."); lastDot > 0 {
				return publicIDWithExt[:lastDot]
			}
			return publicIDWithExt
		}
	}
	return ""
}

func ContainsPublicID(url, publicID string) bool {
	return strings.Contains(url, publicID)
}

func JoinStrings(strs []string, sep string) string {
	return strings.Join(strs, sep)
}

func IsPaystackNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "404") ||
		strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "already inactive")
}

// ============================================================================
// Helpers For Chats
// ============================================================================

// buildJobSummaryMessage formats the job details into a readable first message.
type AutoMessage struct {
	Content string
	MsgType string
}

// formatIssueType converts snake_case issue types to human readable.
func FormatIssueType(issue string) string {
	words := strings.Split(issue, "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func PgErrCode(err error) string {
	if pgErr, ok := err.(*pgconn.PgError); ok {
		return pgErr.Code
	}
	return ""
}

func RatingStars(rating int) string {
	stars := ""
	for i := 0; i < rating; i++ {
		stars += "★"
	}
	for i := rating; i < 5; i++ {
		stars += "☆"
	}
	return stars
}

// sendPushToUser fetches FCM tokens for a user and sends them a push notification.
func SendPushToUser(userID uuid.UUID, title, body string, data ...map[string]string) {
	tokens, err := GetUserFCMTokens(userID)
	if err != nil || len(tokens) == 0 {
		return
	}
	var d map[string]string
	if len(data) > 0 {
		d = data[0]
	}
	for _, token := range tokens {
		if err := notifications.SendPushNotification(token, title, body, d); err != nil {
			utils.Logger.Warnf("failed to send push notification to user %s: %v", userID, err)
		}
	}
}

// getUserFCMTokens fetches all FCM tokens registered for a artisan.
func GetUserFCMTokens(artisanID uuid.UUID) ([]string, error) {
	db := sqlconnect.DB
	if db == nil {
		return nil, fmt.Errorf("db not initialized")
	}

	rows, err := db.Query(context.Background(), `
		SELECT fcm_token FROM user_devices WHERE user_id = $1
	`, artisanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

// ParsePagination reads ?page=&per_page= from the request,
// returning safe defaults (page=1, perPage=20, max perPage=100).
func ParsePagination(r *http.Request) (page, perPage int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	perPage, _ = strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}

	return page, perPage
}

var AllowedDeviceTypes = map[string]bool{
	"android": true,
	"ios":     true,
}

var AllowedRoles = map[string]bool{
	"client":  true,
	"artisan": true,
	"owner":   true,
}

// BuildPaginatedResponse wraps a slice of data in the standard paginated envelope.
func BuildPaginatedResponse(data interface{}, total, page, perPage int) map[string]interface{} {
	pages := int(math.Ceil(float64(total) / float64(perPage)))
	return map[string]interface{}{
		"status":   "success",
		"data":     data,
		"total":    total,
		"page":     page,
		"per_page": perPage,
		"pages":    pages,
	}
}

// ParseUUIDFromPath extracts a UUID from the URL path by param name.
// Assumes the path segment sits after the last occurrence of "/<param>/value"
// OR simply after the last "/". Adjust to your router conventions as needed.
//
// Example path: /admin/users/550e8400-e29b-41d4-a716-446655440000/status
// ParseUUIDFromPath(r, "id") → parses the UUID segment.
//
// Since net/http's default mux doesn't provide named path params, this helper
// walks the segments and returns the first valid UUID found after the base path.
// If you're using gorilla/mux or chi, replace this with mux.Vars(r)["id"] etc.
func ParseUUIDFromPath(r *http.Request, _ string) (uuid.UUID, error) {
	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for _, seg := range segments {
		if id, err := uuid.Parse(seg); err == nil {
			return id, nil
		}
	}
	return uuid.Nil, &pathParamError{"no valid UUID found in path"}
}

// ParsePathParam returns the last non-empty path segment (used for key params like /admin/settings/maintenance_mode).
func ParsePathParam(r *http.Request, _ string) string {
	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for i := len(segments) - 1; i >= 0; i-- {
		if segments[i] != "" {
			return segments[i]
		}
	}
	return ""
}

// Itoa is a convenience alias so handler files don't need strconv imported just for this.
func Itoa(i int) string {
	return strconv.Itoa(i)
}

type pathParamError struct{ msg string }

func (e *pathParamError) Error() string { return e.msg }

type StoreReviewSummary struct {
	AvgRating    float64           `json:"avg_rating"`
	TotalReviews int               `json:"total_reviews"`
	Breakdown    map[string]int    `json:"breakdown"`
	Recent       []StoreReviewItem `json:"recent_reviews"`
}

type StoreReviewItem struct {
	ID             uuid.UUID        `json:"id"`
	Rating         int              `json:"rating"`
	Comment        *string          `json:"comment"`
	CreatedAt      time.Time        `json:"created_at"`
	ReviewerID     uuid.UUID        `json:"reviewer_id"`
	ReviewerName   string           `json:"reviewer_name"`
	ReviewerAvatar *string          `json:"reviewer_avatar"`
	Replies        []StoreReplyItem `json:"replies"`
}

type StoreReplyItem struct {
	ID           uuid.UUID `json:"id"`
	AuthorID     uuid.UUID `json:"author_id"`
	AuthorName   string    `json:"author_name"`
	AuthorAvatar *string   `json:"author_avatar"`
	AuthorRole   string    `json:"author_role"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
}

func FetchStoreReviewSummary(ctx context.Context, db *pgxpool.Pool, storeID uuid.UUID) StoreReviewSummary {
	summary := StoreReviewSummary{
		Breakdown: map[string]int{"5_star": 0, "4_star": 0, "3_star": 0, "2_star": 0, "1_star": 0},
		Recent:    []StoreReviewItem{},
	}

	var star1, star2, star3, star4, star5 int

	db.QueryRow(ctx, `
		SELECT
			COALESCE(ROUND(AVG(rating)::numeric, 1), 0),
			COUNT(*),
			COUNT(*) FILTER (WHERE rating = 1),
			COUNT(*) FILTER (WHERE rating = 2),
			COUNT(*) FILTER (WHERE rating = 3),
			COUNT(*) FILTER (WHERE rating = 4),
			COUNT(*) FILTER (WHERE rating = 5)
		FROM store_reviews
		WHERE store_id = $1
	`, storeID).Scan(
		&summary.AvgRating, &summary.TotalReviews,
		&star1, &star2, &star3, &star4, &star5,
	)

	summary.Breakdown["1_star"] = star1
	summary.Breakdown["2_star"] = star2
	summary.Breakdown["3_star"] = star3
	summary.Breakdown["4_star"] = star4
	summary.Breakdown["5_star"] = star5

	// Fetch 5 most recent reviews + their replies via LEFT JOIN
	rows, err := db.Query(ctx, `
		SELECT
			sr.id, sr.rating, sr.comment, sr.created_at,
			reviewer.id, reviewer.full_name, reviewer.avatar,
			rr.id, rr.author_role, rr.body, rr.created_at,
			author.id, author.full_name, author.avatar
		FROM store_reviews sr
		JOIN users reviewer ON reviewer.id = sr.buyer_id
		LEFT JOIN store_review_replies rr ON rr.review_id = sr.id
		LEFT JOIN users author ON author.id = rr.author_id
		WHERE sr.store_id = $1
		ORDER BY sr.created_at DESC, rr.created_at ASC
		LIMIT 20
	`, storeID)
	if err != nil {
		return summary
	}
	defer rows.Close()

	reviewMap := make(map[uuid.UUID]*StoreReviewItem)
	reviewOrder := make([]uuid.UUID, 0)

	for rows.Next() {
		var (
			reviewID       uuid.UUID
			rating         int
			comment        *string
			createdAt      time.Time
			reviewerID     uuid.UUID
			reviewerName   string
			reviewerAvatar []byte

			replyID           *uuid.UUID
			replyAuthorRole   *string
			replyBody         *string
			replyCreatedAt    *time.Time
			replyAuthorID     *uuid.UUID
			replyAuthorName   *string
			replyAuthorAvatar []byte
		)

		if err := rows.Scan(
			&reviewID, &rating, &comment, &createdAt,
			&reviewerID, &reviewerName, &reviewerAvatar,
			&replyID, &replyAuthorRole, &replyBody, &replyCreatedAt,
			&replyAuthorID, &replyAuthorName, &replyAuthorAvatar,
		); err != nil {
			continue
		}

		if _, exists := reviewMap[reviewID]; !exists {
			item := &StoreReviewItem{
				ID:           reviewID,
				Rating:       rating,
				Comment:      comment,
				CreatedAt:    createdAt,
				ReviewerID:   reviewerID,
				ReviewerName: reviewerName,
				Replies:      []StoreReplyItem{},
			}
			if len(reviewerAvatar) > 0 {
				var av struct {
					URL string `json:"url"`
				}
				if json.Unmarshal(reviewerAvatar, &av) == nil && av.URL != "" {
					item.ReviewerAvatar = &av.URL
				}
			}
			reviewMap[reviewID] = item
			reviewOrder = append(reviewOrder, reviewID)
		}

		if replyID != nil {
			reply := StoreReplyItem{
				ID:         *replyID,
				AuthorID:   *replyAuthorID,
				AuthorName: *replyAuthorName,
				AuthorRole: *replyAuthorRole,
				Body:       *replyBody,
				CreatedAt:  *replyCreatedAt,
			}
			if len(replyAuthorAvatar) > 0 {
				var av struct {
					URL string `json:"url"`
				}
				if json.Unmarshal(replyAuthorAvatar, &av) == nil && av.URL != "" {
					reply.AuthorAvatar = &av.URL
				}
			}
			// Only keep up to 5 unique reviews worth of rows
			if len(reviewOrder) <= 5 {
				reviewMap[reviewID].Replies = append(reviewMap[reviewID].Replies, reply)
			}
		}
	}

	for _, id := range reviewOrder {
		if len(summary.Recent) >= 5 {
			break
		}
		summary.Recent = append(summary.Recent, *reviewMap[id])
	}

	return summary
}

func IsUsernameAvailable(ctx context.Context, username string) (bool, error) {
	// Check bloom filter first
	mightExist, err := cache.UsernameBloomCheck(ctx, username)
	if err != nil {
		// Redis is down — go straight to DB
		return CheckUsernameInDB(ctx, username)
	}

	if !mightExist {
		// Bloom filter says definitely not in DB — skip the DB call
		return true, nil
	}

	// Probable match — confirm with DB
	return CheckUsernameInDB(ctx, username)
}

func CheckUsernameInDB(ctx context.Context, username string) (bool, error) {
	var exists bool
	err := sqlconnect.DB.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1 AND deleted_at IS NULL)`,
		username,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return !exists, nil
}

// ============================================================================
// Helpers
// ============================================================================

func MissingContact(hasEmail, hasPhone bool) string {
	if !hasEmail {
		return "email"
	}
	if !hasPhone {
		return "phone_number"
	}
	return ""
}

func ParseAdminUUID(id string) (uuid.UUID, error) {
	return uuid.Parse(id)
}

func RegistrationStatus(role string) string {
	if role == "artisan" {
		return "pending"
	}
	return "approved"
}

func NullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// slotIsBooked is a helper used in booking creation to check slot conflicts
// under a transaction lock.
func SlotIsBooked(ctx context.Context, tx pgx.Tx, artisanID uuid.UUID, categoryID uuid.UUID, date, startTime string) (bool, error) {
	var count int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM artisan_bookings
		WHERE artisan_id = $1 AND category_id = $2
		  AND booking_date = $3 AND start_time = $4
		  AND status IN ('pending', 'confirmed')
	`, artisanID, categoryID, date, startTime).Scan(&count)
	return count > 0, err
}

func ValidateOrderDates(checkInStr, checkOutStr string) (checkIn, checkOut time.Time, numNights int, errMsg string) {
	if checkInStr == "" || checkOutStr == "" {
		return checkIn, checkOut, 0, "check_in_date and check_out_date are required (YYYY-MM-DD)"
	}
	var err error
	checkIn, err = time.Parse("2006-01-02", checkInStr)
	if err != nil {
		return checkIn, checkOut, 0, "check_in_date must be YYYY-MM-DD"
	}
	checkOut, err = time.Parse("2006-01-02", checkOutStr)
	if err != nil {
		return checkIn, checkOut, 0, "check_out_date must be YYYY-MM-DD"
	}
	today := time.Now().Truncate(24 * time.Hour)
	if checkIn.Before(today) {
		return checkIn, checkOut, 0, "check_in_date cannot be in the past"
	}
	if !checkOut.After(checkIn) {
		return checkIn, checkOut, 0, "check_out_date must be after check_in_date"
	}
	numNights = int(checkOut.Sub(checkIn).Hours() / 24)
	return checkIn, checkOut, numNights, ""
}

type propertyForOrder struct {
	ID            uuid.UUID
	OwnerID       uuid.UUID
	Name          string
	PricePerNight float64
	CautionFee    float64
	MaxAdults     int
	MaxChildren   int
}

func FetchPropertyForOrder(ctx context.Context, db *pgxpool.Pool, propID uuid.UUID) (propertyForOrder, string, string, error) {
	var p propertyForOrder
	var checkIn, checkOut string
	err := db.QueryRow(ctx, `
		SELECT p.id, p.owner_id, p.name, p.price_per_night, p.caution_fee,
		       p.max_adults, p.max_children,
		       COALESCE((SELECT check_in_time::TEXT FROM property_availability WHERE property_id = p.id AND is_active = TRUE LIMIT 1), '14:00'),
		       COALESCE((SELECT check_out_time::TEXT FROM property_availability WHERE property_id = p.id AND is_active = TRUE LIMIT 1), '11:00')
		FROM properties p
		WHERE p.id = $1 AND p.status = 'active' AND p.deleted_at IS NULL
	`, propID).Scan(
		&p.ID, &p.OwnerID, &p.Name, &p.PricePerNight, &p.CautionFee,
		&p.MaxAdults, &p.MaxChildren, &checkIn, &checkOut,
	)
	return p, checkIn, checkOut, err
}

func CheckDateAvailability(ctx context.Context, db *pgxpool.Pool, propID uuid.UUID, checkIn, checkOut time.Time) (bool, error) {
	ci := checkIn.Format("2006-01-02")
	co := checkOut.Format("2006-01-02")

	// Must have an availability window covering the full range
	var hasWindow bool
	db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM property_availability
			WHERE property_id = $1 AND is_active = TRUE
			  AND available_from <= $2 AND available_to >= $3
		)
	`, propID, ci, co).Scan(&hasWindow)
	if !hasWindow {
		return false, nil
	}

	// Must not have a conflicting active order
	var hasConflict bool
	db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM orders
			WHERE property_id = $1 AND status IN ('pending','confirmed','checked_in')
			  AND check_in_date < $3 AND check_out_date > $2
		)
	`, propID, ci, co).Scan(&hasConflict)
	if hasConflict {
		return false, nil
	}

	// Must not have any blocked dates in range
	var hasBlock bool
	db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM property_availability_overrides
			WHERE property_id = $1
			  AND blocked_date >= $2 AND blocked_date < $3
		)
	`, propID, ci, co).Scan(&hasBlock)

	return !hasBlock, nil
}

func CheckDateAvailabilityTx(ctx context.Context, tx pgx.Tx, propID uuid.UUID, checkIn, checkOut time.Time) (bool, error) {
	ci := checkIn.Format("2006-01-02")
	co := checkOut.Format("2006-01-02")

	var hasWindow bool
	tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM property_availability
			WHERE property_id = $1 AND is_active = TRUE
			  AND available_from <= $2 AND available_to >= $3
		)
	`, propID, ci, co).Scan(&hasWindow)
	if !hasWindow {
		return false, nil
	}

	var hasConflict bool
	tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM orders
			WHERE property_id = $1 AND status IN ('pending','confirmed','checked_in')
			  AND check_in_date < $3 AND check_out_date > $2
		)
	`, propID, ci, co).Scan(&hasConflict)
	if hasConflict {
		return false, nil
	}

	var hasBlock bool
	tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM property_availability_overrides
			WHERE property_id = $1 AND blocked_date >= $2 AND blocked_date < $3
		)
	`, propID, ci, co).Scan(&hasBlock)

	return !hasBlock, nil
}

func FetchPlatformFeePct(ctx context.Context, db *pgxpool.Pool) float64 {
	var valStr string
	if err := db.QueryRow(ctx, `SELECT value FROM platform_settings WHERE key = 'platform_service_charge'`).Scan(&valStr); err != nil {
		return 5.0 // default
	}
	var pct float64
	fmt.Sscanf(valStr, "%f", &pct)
	if pct <= 0 {
		return 5.0
	}
	return pct
}

func CalculateOrderSummary(pricePerNight, cautionFee float64, numNights int, platformFeePct float64) shortletModels.OrderSummary {
	subtotal := pricePerNight * float64(numNights)
	platformFeeAmount := subtotal * platformFeePct / 100
	totalAmount := subtotal + cautionFee
	return shortletModels.OrderSummary{
		PricePerNight:     pricePerNight,
		NumNights:         numNights,
		Subtotal:          subtotal,
		CautionFee:        cautionFee,
		PlatformFeePct:    platformFeePct,
		PlatformFeeAmount: platformFeeAmount,
		TotalAmount:       totalAmount,
	}
}

func RefundOrderEscrow(ctx context.Context, tx pgx.Tx, orderID, clientID, ownerID uuid.UUID) error {
	var escrowID uuid.UUID
	var amount float64
	err := tx.QueryRow(ctx, `
		SELECT id, amount FROM order_escrow WHERE order_id = $1 AND status = 'held'
	`, orderID).Scan(&escrowID, &amount)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("fetch escrow: %w", err)
	}

	tx.Exec(ctx, `UPDATE order_escrow SET status = 'refunded', released_at = NOW() WHERE id = $1`, escrowID)

	var walletID uuid.UUID
	tx.QueryRow(ctx, `
		UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW()
		WHERE user_id = $2 AND is_active = TRUE RETURNING id
	`, amount, clientID).Scan(&walletID)

	tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id) VALUES ($1, $2, 'refund', $3)`, walletID, amount, escrowID)
	refundRef := fmt.Sprintf("ORDER-REFUND-%s", escrowID)
	tx.Exec(ctx, `
		INSERT INTO transactions (user_id, transaction_type, reference, amount, description, status)
		VALUES ($1, 'credit', $2, $3, $4, 'success')
	`, clientID, refundRef, amount, fmt.Sprintf("Refund for cancelled order %s", orderID))

	return nil
}

func ReleaseOrderEscrow(ctx context.Context, tx pgx.Tx, orderID, ownerID, clientID uuid.UUID) (float64, error) {
	var escrowID uuid.UUID
	var amount, commission, netPayout, cautionFee float64

	err := tx.QueryRow(ctx, `
		SELECT e.id, e.amount, e.commission, e.net_payout,
		       o.caution_fee
		FROM order_escrow e
		JOIN orders o ON o.id = e.order_id
		WHERE e.order_id = $1 AND e.status = 'held'
	`, orderID).Scan(&escrowID, &amount, &commission, &netPayout, &cautionFee)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("fetch escrow: %w", err)
	}

	tx.Exec(ctx, `UPDATE order_escrow SET status = 'released', released_at = NOW() WHERE id = $1`, escrowID)

	// Credit owner: net payout (subtotal - platform fee)
	var ownerWalletID uuid.UUID
	tx.QueryRow(ctx, `
		INSERT INTO wallets (user_id, balance, last_transaction_at) VALUES ($1, $2, NOW())
		ON CONFLICT (user_id) DO UPDATE SET balance = wallets.balance + $2, last_transaction_at = NOW()
		RETURNING id
	`, ownerID, netPayout).Scan(&ownerWalletID)

	tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id) VALUES ($1, $2, 'escrow_release', $3)`, ownerWalletID, netPayout, escrowID)
	releaseRef := fmt.Sprintf("ORDER-RELEASE-%s", escrowID)
	tx.Exec(ctx, `
		INSERT INTO transactions (user_id, transaction_type, reference, amount, description, status)
		VALUES ($1, 'credit', $2, $3, $4, 'success')
	`, ownerID, releaseRef, netPayout, fmt.Sprintf("Payout for order %s (after %.0f%% platform fee)", orderID, commission))

	// Refund caution fee to client if applicable
	if cautionFee > 0 {
		var clientWalletID uuid.UUID
		tx.QueryRow(ctx, `
			UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW()
			WHERE user_id = $2 AND is_active = TRUE RETURNING id
		`, cautionFee, clientID).Scan(&clientWalletID)
		tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id) VALUES ($1, $2, 'refund', $3)`, clientWalletID, cautionFee, escrowID)
	}

	return netPayout, nil
}

// handlers/order_notifications.go

func SendOrderConfirmationNotifications(order shortletModels.Order, db *pgxpool.Pool) {
	bgCtx := context.Background()

	// Fetch client contact info
	var clientEmail, clientPhone, clientFirstName string
	if err := db.QueryRow(bgCtx,
		`SELECT COALESCE(email,''), COALESCE(phone_number,''), first_name FROM users WHERE id = $1`,
		order.ClientID,
	).Scan(&clientEmail, &clientPhone, &clientFirstName); err != nil {
		utils.Logger.Errorf("SendOrderConfirmationNotifications: fetch client %s: %v", order.ClientID, err)
	}

	// Fetch owner contact info
	var ownerEmail, ownerPhone, ownerFirstName string
	if err := db.QueryRow(bgCtx,
		`SELECT COALESCE(email,''), COALESCE(phone_number,''), first_name FROM users WHERE id = $1`,
		order.OwnerID,
	).Scan(&ownerEmail, &ownerPhone, &ownerFirstName); err != nil {
		utils.Logger.Errorf("SendOrderConfirmationNotifications: fetch owner %s: %v", order.OwnerID, err)
	}

	// Fetch property name
	var propName string
	if err := db.QueryRow(bgCtx,
		`SELECT name FROM properties WHERE id = $1`, order.PropertyID,
	).Scan(&propName); err != nil {
		utils.Logger.Errorf("SendOrderConfirmationNotifications: fetch property %s: %v", order.PropertyID, err)
	}

	utils.Logger.Infof("SendOrderConfirmationNotifications: order=%s client_email=%q client_phone=%q owner_email=%q prop=%q",
		order.ID, clientEmail, clientPhone, ownerEmail, propName)

	// In-app notifications
	utils.CreateNotification(bgCtx, order.ClientID,
		utils.NotifBookingConfirmed,
		"Booking Confirmed! 🏠",
		fmt.Sprintf("Your booking at %s from %s to %s is confirmed.", propName, order.CheckInDate, order.CheckOutDate),
		map[string]interface{}{"order_id": order.ID},
	)
	utils.CreateNotification(bgCtx, order.OwnerID,
		utils.NotifBookingConfirmed,
		"New Booking Received! 🎉",
		fmt.Sprintf("%s has booked your property from %s to %s.", clientFirstName, order.CheckInDate, order.CheckOutDate),
		map[string]interface{}{"order_id": order.ID},
	)

	// Push notifications
	SendPushToUser(order.ClientID, "Booking Confirmed",
		fmt.Sprintf("Your stay at %s is confirmed!", propName),
		map[string]string{"screen": "OrderDetails", "order_id": order.ID.String()})
	SendPushToUser(order.OwnerID, "New Booking!",
		fmt.Sprintf("%s booked your property", clientFirstName),
		map[string]string{"screen": "OwnerOrders", "order_id": order.ID.String()})

	// Client email + SMS
	if clientEmail != "" {
		if err := utils.SendOrderConfirmedEmail(clientEmail, clientFirstName, propName, order.CheckInDate, order.CheckOutDate, order.TotalAmount); err != nil {
			utils.Logger.Errorf("SendOrderConfirmedEmail client %s: %v", order.ClientID, err)
		}
	} else {
		utils.Logger.Warnf("SendOrderConfirmationNotifications: no email for client %s", order.ClientID)
	}
	if clientPhone != "" {
		if err := utils.SendOrderConfirmedSMS(clientPhone, clientFirstName, propName, order.CheckInDate, order.CheckOutDate); err != nil {
			utils.Logger.Errorf("SendOrderConfirmedSMS client %s: %v", order.ClientID, err)
		}
	} else {
		utils.Logger.Warnf("SendOrderConfirmationNotifications: no phone for client %s", order.ClientID)
	}

	// Owner email + SMS — only if different from client (avoids double-notifying in tests)
	if order.OwnerID != order.ClientID {
		if ownerEmail != "" {
			clientFullName := clientFirstName // already have first name; enough for the owner email
			if err := utils.SendNewBookingOwnerEmail(ownerEmail, ownerFirstName, clientFullName, propName, order.CheckInDate, order.CheckOutDate, order.TotalAmount); err != nil {
				utils.Logger.Errorf("SendNewBookingOwnerEmail owner %s: %v", order.OwnerID, err)
			}
		} else {
			utils.Logger.Warnf("SendOrderConfirmationNotifications: no email for owner %s", order.OwnerID)
		}
		if ownerPhone != "" {
			if err := utils.SendNewBookingSMS(ownerPhone, ownerFirstName, clientFirstName, propName, order.CheckInDate); err != nil {
				utils.Logger.Errorf("SendNewBookingSMS owner %s: %v", order.OwnerID, err)
			}
		} else {
			utils.Logger.Warnf("SendOrderConfirmationNotifications: no phone for owner %s", order.OwnerID)
		}
	}
}

func SendOrderReceipt(order shortletModels.Order, db *pgxpool.Pool, summary shortletModels.OrderSummary, paymentMethod string) {
	bgCtx := context.Background()

	var clientEmail, clientFirstName, clientLastName string
	if err := db.QueryRow(bgCtx,
		`SELECT COALESCE(email,''), first_name, last_name FROM users WHERE id = $1`,
		order.ClientID,
	).Scan(&clientEmail, &clientFirstName, &clientLastName); err != nil {
		utils.Logger.Errorf("SendOrderReceipt: fetch client %s: %v", order.ClientID, err)
	}

	var ownerFirstName, ownerLastName string
	if err := db.QueryRow(bgCtx,
		`SELECT first_name, last_name FROM users WHERE id = $1`, order.OwnerID,
	).Scan(&ownerFirstName, &ownerLastName); err != nil {
		utils.Logger.Errorf("SendOrderReceipt: fetch owner %s: %v", order.OwnerID, err)
	}

	var prop shortletModels.Property
	var imagesRaw []byte
	if err := db.QueryRow(bgCtx,
		`SELECT id, name, state, city, street, images FROM properties WHERE id = $1`, order.PropertyID,
	).Scan(&prop.ID, &prop.Name, &prop.State, &prop.City, &prop.Street, &imagesRaw); err != nil {
		utils.Logger.Errorf("SendOrderReceipt: fetch property %s: %v", order.PropertyID, err)
	}
	json.Unmarshal(imagesRaw, &prop.Images)

	utils.Logger.Infof("SendOrderReceipt: order=%s client_email=%q prop=%q", order.ID, clientEmail, prop.Name)

	receipt := shortletModels.OrderReceipt{
		ReceiptRef:    fmt.Sprintf("LETI-RCP-%s", order.ID.String()[:8]),
		Order:         order,
		Property:      prop,
		OwnerName:     ownerFirstName + " " + ownerLastName,
		ClientName:    clientFirstName + " " + clientLastName,
		Summary:       summary,
		PaidAt:        time.Now(),
		PaymentMethod: paymentMethod,
	}

	if clientEmail != "" {
		if err := utils.SendOrderReceiptEmail(clientEmail, receipt); err != nil {
			utils.Logger.Errorf("SendOrderReceiptEmail client %s: %v", order.ClientID, err)
		}
	} else {
		utils.Logger.Warnf("SendOrderReceipt: no email for client %s", order.ClientID)
	}

	utils.CreateNotification(bgCtx, order.ClientID,
		utils.NotifPaymentReceived,
		"Payment Receipt",
		fmt.Sprintf("Your payment of ₦%.2f for %s has been received. Receipt: %s", order.TotalAmount, prop.Name, receipt.ReceiptRef),
		map[string]interface{}{"order_id": order.ID, "receipt_ref": receipt.ReceiptRef},
	)

	if _, err := db.Exec(bgCtx, `UPDATE orders SET receipt_sent_at = NOW() WHERE id = $1`, order.ID); err != nil {
		utils.Logger.Warnf("SendOrderReceipt: failed to set receipt_sent_at for order %s: %v", order.ID, err)
	}
}

func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return fmt.Sprintf("%v", err) != "" && (ContainsStr(err.Error(), "23505") || ContainsStr(err.Error(), "unique"))
}

func ContainsStr(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) >= len(substr) &&
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}

// buildWSPayload marshals an outgoing WebSocket message.
func BuildWSPayload(msgType string, payload interface{}) []byte {
	b, _ := json.Marshal(chat.OutgoingWS{Type: msgType, Payload: payload})
	return b
}
