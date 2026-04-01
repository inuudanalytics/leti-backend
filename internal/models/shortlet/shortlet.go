package shortlet

import (
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// PropertyImage — element of the JSONB images array
// ---------------------------------------------------------------------------

type PropertyImage struct {
	URL       string    `json:"url"`
	PublicID  string    `json:"public_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// Property
// ---------------------------------------------------------------------------

type Property struct {
	ID            uuid.UUID       `json:"id"`
	OwnerID       uuid.UUID       `json:"owner_id"`
	Name          string          `json:"name"`
	Description   *string         `json:"description,omitempty"`
	PropertyType  string          `json:"property_type"`
	Status        string          `json:"status"`
	PricePerNight float64         `json:"price_per_night"`
	CautionFee    float64         `json:"caution_fee"`
	Images        []PropertyImage `json:"images"`
	Amenities     []string        `json:"amenities"`
	HouseRules    []string        `json:"house_rules"`
	MaxAdults     int             `json:"max_adults"`
	MaxChildren   int             `json:"max_children"`
	State         string          `json:"state"`
	City          string          `json:"city"`
	Street        string          `json:"street"`
	Latitude      *float64        `json:"latitude,omitempty"`
	Longitude     *float64        `json:"longitude,omitempty"`
	AvgRating     float64         `json:"avg_rating"`
	ReviewCount   int             `json:"review_count"`
	DeletedAt     *time.Time      `json:"deleted_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// PropertyAvailability
// ---------------------------------------------------------------------------

type PropertyAvailability struct {
	ID            uuid.UUID `json:"id"`
	PropertyID    uuid.UUID `json:"property_id"`
	AvailableFrom string    `json:"available_from"` // YYYY-MM-DD
	AvailableTo   string    `json:"available_to"`
	CheckInTime   string    `json:"check_in_time"` // HH:MM
	CheckOutTime  string    `json:"check_out_time"`
	IsActive      bool      `json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// PropertyAvailabilityOverride
// ---------------------------------------------------------------------------

type PropertyAvailabilityOverride struct {
	ID          uuid.UUID `json:"id"`
	PropertyID  uuid.UUID `json:"property_id"`
	BlockedDate string    `json:"blocked_date"` // YYYY-MM-DD
	Reason      *string   `json:"reason,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// CalendarDay — returned to frontend for calendar rendering
// ---------------------------------------------------------------------------

type CalendarDay struct {
	Date         string `json:"date"` // YYYY-MM-DD
	Available    bool   `json:"available"`
	Blocked      bool   `json:"blocked"` // owner override
	Booked       bool   `json:"booked"`  // has confirmed order
	CheckInTime  string `json:"check_in_time,omitempty"`
	CheckOutTime string `json:"check_out_time,omitempty"`
}

// ---------------------------------------------------------------------------
// SavedListing
// ---------------------------------------------------------------------------

type SavedListing struct {
	ID         uuid.UUID `json:"id"`
	ClientID   uuid.UUID `json:"client_id"`
	PropertyID uuid.UUID `json:"property_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Order  (shortlet booking)
// ---------------------------------------------------------------------------

type Order struct {
	ID                uuid.UUID  `json:"id"`
	PropertyID        uuid.UUID  `json:"property_id"`
	ClientID          uuid.UUID  `json:"client_id"`
	OwnerID           uuid.UUID  `json:"owner_id"`
	CheckInDate       string     `json:"check_in_date"`
	CheckOutDate      string     `json:"check_out_date"`
	NumNights         int        `json:"num_nights"`
	NumAdults         int        `json:"num_adults"`
	NumChildren       int        `json:"num_children"`
	PricePerNight     float64    `json:"price_per_night"`
	CautionFee        float64    `json:"caution_fee"`
	PlatformFeePct    float64    `json:"platform_fee_pct"`
	Subtotal          float64    `json:"subtotal"`
	PlatformFeeAmount float64    `json:"platform_fee_amount"`
	TotalAmount       float64    `json:"total_amount"`
	Status            string     `json:"status"`
	PaymentMethod     *string    `json:"payment_method,omitempty"`
	PaymentStatus     string     `json:"payment_status"`
	PaymentReference  *string    `json:"payment_reference,omitempty"`
	ConfirmedAt       *time.Time `json:"confirmed_at,omitempty"`
	CheckedInAt       *time.Time `json:"checked_in_at,omitempty"`
	CheckedOutAt      *time.Time `json:"checked_out_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	CancelledAt       *time.Time `json:"cancelled_at,omitempty"`
	CancelledBy       *uuid.UUID `json:"cancelled_by,omitempty"`
	ReceiptSentAt     *time.Time `json:"receipt_sent_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// OrderSummary is returned at booking-creation time before payment.
type OrderSummary struct {
	PricePerNight     float64 `json:"price_per_night"`
	NumNights         int     `json:"num_nights"`
	Subtotal          float64 `json:"subtotal"`
	CautionFee        float64 `json:"caution_fee"`
	PlatformFeePct    float64 `json:"platform_fee_pct"`
	PlatformFeeAmount float64 `json:"platform_fee_amount"`
	TotalAmount       float64 `json:"total_amount"`
}

// ---------------------------------------------------------------------------
// OrderEscrow
// ---------------------------------------------------------------------------

type OrderEscrow struct {
	ID            uuid.UUID  `json:"id"`
	OrderID       uuid.UUID  `json:"order_id"`
	PayerID       uuid.UUID  `json:"payer_id"`
	PayeeID       uuid.UUID  `json:"payee_id"`
	Amount        float64    `json:"amount"`
	Commission    float64    `json:"commission"`
	NetPayout     float64    `json:"net_payout"`
	Status        string     `json:"status"`
	PaymentMethod *string    `json:"payment_method,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	ReleasedAt    *time.Time `json:"released_at,omitempty"`
}

// ---------------------------------------------------------------------------
// PropertyReview
// ---------------------------------------------------------------------------

type PropertyReview struct {
	ID         uuid.UUID             `json:"id"`
	PropertyID uuid.UUID             `json:"property_id"`
	OrderID    uuid.UUID             `json:"order_id"`
	ClientID   uuid.UUID             `json:"client_id"`
	Rating     int                   `json:"rating"`
	Comment    *string               `json:"comment,omitempty"`
	Replies    []PropertyReviewReply `json:"replies,omitempty"`
	CreatedAt  time.Time             `json:"created_at"`
	UpdatedAt  time.Time             `json:"updated_at"`
}

type PropertyReviewReply struct {
	ID         uuid.UUID `json:"id"`
	ReviewID   uuid.UUID `json:"review_id"`
	AuthorID   uuid.UUID `json:"author_id"`
	AuthorName string    `json:"author_name"`
	AuthorRole string    `json:"author_role"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Receipt
// ---------------------------------------------------------------------------

type OrderReceipt struct {
	ReceiptRef    string       `json:"receipt_ref"`
	Order         Order        `json:"order"`
	Property      Property     `json:"property"`
	OwnerName     string       `json:"owner_name"`
	ClientName    string       `json:"client_name"`
	Summary       OrderSummary `json:"summary"`
	PaidAt        time.Time    `json:"paid_at"`
	PaymentMethod string       `json:"payment_method"`
}
