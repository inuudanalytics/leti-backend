package shortletcache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"leti_server/pkg/cache"
	"leti_server/pkg/utils"
)

// ---------------------------------------------------------------------------
// TTLs
// ---------------------------------------------------------------------------

const (
	TTLPropertyDetail = 10 * time.Minute
	TTLPropertyList   = 3 * time.Minute
	TTLCalendar       = 5 * time.Minute
	TTLReviews        = 10 * time.Minute
	TTLSavedListings  = 5 * time.Minute
	TTLMyProperties   = 3 * time.Minute
)

// ---------------------------------------------------------------------------
// Key builders
// ---------------------------------------------------------------------------

func KeyPropertyDetail(propID string) string {
	return fmt.Sprintf("shortlet:property:%s", propID)
}

// KeyPropertyList captures all query-string filters so different filter
// combinations get separate cache entries.
func KeyPropertyList(queryString string) string {
	return fmt.Sprintf("shortlet:properties:list:%s", queryString)
}

func KeyCalendar(propID, from, to string) string {
	return fmt.Sprintf("shortlet:calendar:%s:%s:%s", propID, from, to)
}

func KeyReviews(propID string, page, limit int) string {
	return fmt.Sprintf("shortlet:reviews:%s:p%d:l%d", propID, page, limit)
}

func KeySavedListings(clientID string, page, limit int) string {
	return fmt.Sprintf("shortlet:saved:%s:p%d:l%d", clientID, page, limit)
}

func KeyMyProperties(ownerID, status string, page, limit int) string {
	return fmt.Sprintf("shortlet:myprops:%s:s%s:p%d:l%d", ownerID, status, page, limit)
}

// ---------------------------------------------------------------------------
// Generic get / set helpers
// ---------------------------------------------------------------------------

// GetCached unmarshals a cached JSON value into dest.
// Returns (true, nil) on hit, (false, nil) on miss, (false, err) on error.
func GetCached(ctx context.Context, key string, dest interface{}) (bool, error) {
	rdb := cache.RDB
	if rdb == nil {
		return false, nil
	}

	val, err := rdb.Get(ctx, key).Result()
	if err != nil {
		return false, nil
	}

	if err := json.Unmarshal([]byte(val), dest); err != nil {
		utils.Logger.Warnf("shortlet cache unmarshal error key=%s: %v", key, err)
		return false, nil
	}
	return true, nil
}

// SetCached marshals v and stores it with the given TTL. Errors are logged
// but never returned — a cache write failure must not break the request.
func SetCached(ctx context.Context, key string, v interface{}, ttl time.Duration) {
	rdb := cache.RDB
	if rdb == nil {
		return
	}

	b, err := json.Marshal(v)
	if err != nil {
		utils.Logger.Warnf("shortlet cache marshal error key=%s: %v", key, err)
		return
	}

	if err := rdb.Set(ctx, key, b, ttl).Err(); err != nil {
		utils.Logger.Warnf("shortlet cache set error key=%s: %v", key, err)
	}
}

// ---------------------------------------------------------------------------
// Invalidation helpers
// ---------------------------------------------------------------------------

// InvalidateProperty removes all cache keys related to a single property.
// Call after any write that touches a property (update, delete, new order,
// availability change, new review, etc.).
func InvalidateProperty(ctx context.Context, propID string) {
	rdb := cache.RDB
	if rdb == nil {
		return
	}

	keys := []string{
		KeyPropertyDetail(propID),
	}

	// Scan & delete calendar keys for this property (wildcard)
	pattern := fmt.Sprintf("shortlet:calendar:%s:*", propID)
	calendarKeys := scanKeys(ctx, pattern)
	keys = append(keys, calendarKeys...)

	// Scan & delete review keys for this property (wildcard)
	reviewPattern := fmt.Sprintf("shortlet:reviews:%s:*", propID)
	reviewKeys := scanKeys(ctx, reviewPattern)
	keys = append(keys, reviewKeys...)

	if len(keys) > 0 {
		if err := rdb.Del(ctx, keys...).Err(); err != nil {
			utils.Logger.Warnf("shortlet cache invalidate error prop=%s: %v", propID, err)
		}
	}

	// Also bust all listing pages — simpler than tracking which pages a
	// property appears on.
	InvalidateListings(ctx)
}

// InvalidateListings deletes all property-list cache entries.
func InvalidateListings(ctx context.Context) {
	rdb := cache.RDB
	if rdb == nil {
		return
	}

	keys := scanKeys(ctx, "shortlet:properties:list:*")
	if len(keys) > 0 {
		rdb.Del(ctx, keys...)
	}
}

// InvalidateSavedListings deletes saved-listing cache for a specific client.
func InvalidateSavedListings(ctx context.Context, clientID string) {
	rdb := cache.RDB
	if rdb == nil {
		return
	}

	keys := scanKeys(ctx, fmt.Sprintf("shortlet:saved:%s:*", clientID))
	if len(keys) > 0 {
		rdb.Del(ctx, keys...)
	}
}

// InvalidateMyProperties deletes all cached "my properties" pages for an owner.
func InvalidateMyProperties(ctx context.Context, ownerID string) {
	rdb := cache.RDB
	if rdb == nil {
		return
	}

	keys := scanKeys(ctx, fmt.Sprintf("shortlet:myprops:%s:*", ownerID))
	if len(keys) > 0 {
		rdb.Del(ctx, keys...)
	}
}

// ---------------------------------------------------------------------------
// scanKeys — SCAN-based key discovery (safe for production Redis)
// ---------------------------------------------------------------------------

func scanKeys(ctx context.Context, pattern string) []string {
	rdb := cache.RDB
	if rdb == nil {
		return nil
	}

	var keys []string
	var cursor uint64

	for {
		var batch []string
		var err error
		batch, cursor, err = rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			utils.Logger.Warnf("shortlet cache scan error pattern=%s: %v", pattern, err)
			break
		}
		keys = append(keys, batch...)
		if cursor == 0 {
			break
		}
	}

	return keys
}
