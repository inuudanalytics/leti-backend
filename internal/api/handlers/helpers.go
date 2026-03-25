package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"leti_server/internal/api/services/notifications"
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
func SendPushToUser(userID uuid.UUID, title, body string) {
	tokens, err := GetUserFCMTokens(userID)
	if err != nil || len(tokens) == 0 {
		return
	}
	for _, token := range tokens {
		if err := notifications.SendPushNotification(token, title, body); err != nil {
			utils.Logger.Warnf("failed to send push notification to user %s: %v", userID, err)
		}
	}
}

// getUserFCMTokens fetches all FCM tokens registered for a mechanic.
func GetUserFCMTokens(mechanicID uuid.UUID) ([]string, error) {
	db := sqlconnect.DB
	if db == nil {
		return nil, fmt.Errorf("db not initialized")
	}

	rows, err := db.Query(context.Background(), `
		SELECT fcm_token FROM user_devices WHERE user_id = $1
	`, mechanicID)
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
