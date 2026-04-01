package shortletcache

import (
	"context"

	shortletcache "leti_server/internal/api/handlers/shortlet/shortletcache"
)

// InvalidateOnNewOrder should be called whenever an order is created,
// confirmed, cancelled, checked-in, or checked-out — any event that
// changes the calendar view or the availability filter on the list page.
func InvalidateOnNewOrder(propertyID string) {
	ctx := context.Background()
	// Calendar keys for this property become stale
	shortletcache.InvalidateProperty(ctx, propertyID)
	// The list page has availability filters — bust all list pages too
	shortletcache.InvalidateListings(ctx)
}
