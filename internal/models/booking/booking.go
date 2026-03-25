package booking

import (
	"time"

	"github.com/google/uuid"
)

type Availability struct {
	ID         uuid.UUID `json:"id"`
	ArtisanID  uuid.UUID `json:"artisan_id"`
	CategoryID uuid.UUID `json:"category_id"`
	Weekday    int       `json:"weekday"`
	StartTime  string    `json:"start_time"`
	EndTime    string    `json:"end_time"`
	IsActive   bool      `json:"is_active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// AvailabilityOverride marks a specific calendar date as blocked or open.
type AvailabilityOverride struct {
	ID           uuid.UUID `json:"id"`
	ArtisanID    uuid.UUID `json:"artisan_id"`
	CategoryID   uuid.UUID `json:"category_id"`
	OverrideDate string    `json:"override_date"` // "YYYY-MM-DD"
	IsAvailable  bool      `json:"is_available"`
	Note         *string   `json:"note,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// AvailableSlot is returned when a client queries open slots for a date.
type AvailableSlot struct {
	Date      string `json:"date"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Available bool   `json:"available"`
}

// Booking is the core booking record.
type Booking struct {
	ID               uuid.UUID  `json:"id"`
	ClientID         uuid.UUID  `json:"client_id"`
	ArtisanID        uuid.UUID  `json:"artisan_id"`
	CategoryID       uuid.UUID  `json:"category_id"`
	ServiceID        *uuid.UUID `json:"service_id,omitempty"`
	ServiceOptionID  *uuid.UUID `json:"service_option_id,omitempty"`
	BookingDate      string     `json:"booking_date"`
	StartTime        string     `json:"start_time"`
	EndTime          string     `json:"end_time"`
	TotalPrice       float64    `json:"total_price"`
	Address          *string    `json:"address,omitempty"`
	Note             *string    `json:"note,omitempty"`
	Status           string     `json:"status"`
	PaymentMethod    *string    `json:"payment_method,omitempty"`
	PaymentStatus    string     `json:"payment_status"`
	PaymentReference *string    `json:"payment_reference,omitempty"`
	ConfirmedAt      *time.Time `json:"confirmed_at,omitempty"`
	DeclinedAt       *time.Time `json:"declined_at,omitempty"`
	CancelledAt      *time.Time `json:"cancelled_at,omitempty"`
	CancelledBy      *uuid.UUID `json:"cancelled_by,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}
