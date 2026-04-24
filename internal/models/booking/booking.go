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

type AvailabilityOverride struct {
	ID           uuid.UUID `json:"id"`
	ArtisanID    uuid.UUID `json:"artisan_id"`
	CategoryID   uuid.UUID `json:"category_id"`
	OverrideDate string    `json:"override_date"` // "YYYY-MM-DD"
	IsAvailable  bool      `json:"is_available"`
	Note         *string   `json:"note,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type AvailableSlot struct {
	Date      string `json:"date"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Available bool   `json:"available"`
}

type Booking struct {
	ID               uuid.UUID  `json:"id"`
	ClientID         uuid.UUID  `json:"client_id"`
	ArtisanID        uuid.UUID  `json:"artisan_id"`
	CategoryID       uuid.UUID  `json:"category_id"`
	ServiceID        *uuid.UUID `json:"service_id,omitempty"`
	ServiceOptionID  *uuid.UUID `json:"service_option_id,omitempty"`
	ServiceName      string     `json:"service_name"`
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

type BookingListItem struct {
	Booking
	ArtisanUsername string      `json:"artisan_username"`
	ArtisanAvatar   interface{} `json:"artisan_avatar"`
}

type BookingServiceOption struct {
	OptionID           uuid.UUID `json:"option_id"`
	OptionLabel        string    `json:"option_label"`
	PriceModifier      float64   `json:"price_modifier"`
	VariationTypeID    uuid.UUID `json:"variation_type_id"`
	VariationTypeLabel string    `json:"variation_type_label"`
}

type BookingDetail struct {
	ID               uuid.UUID             `json:"id"`
	ClientID         uuid.UUID             `json:"client_id"`
	ArtisanID        uuid.UUID             `json:"artisan_id"`
	ArtisanUsername  string                `json:"artisan_username"`
	ArtisanAvatar    interface{}           `json:"artisan_avatar"`
	CategoryID       uuid.UUID             `json:"category_id"`
	CategoryName     string                `json:"category_name"`
	ServiceID        *uuid.UUID            `json:"service_id,omitempty"`
	ServiceName      string                `json:"service_name"`
	ServiceBasePrice *float64              `json:"service_base_price,omitempty"`
	ServiceOptionID  *uuid.UUID            `json:"service_option_id,omitempty"`
	ServiceOption    *BookingServiceOption `json:"service_option,omitempty"`
	BookingDate      string                `json:"booking_date"`
	StartTime        string                `json:"start_time"`
	EndTime          string                `json:"end_time"`
	TotalPrice       float64               `json:"total_price"`
	Address          *string               `json:"address,omitempty"`
	Note             *string               `json:"note,omitempty"`
	Status           string                `json:"status"`
	PaymentMethod    *string               `json:"payment_method,omitempty"`
	PaymentStatus    string                `json:"payment_status"`
	PaymentReference *string               `json:"payment_reference,omitempty"`
	ConfirmedAt      *time.Time            `json:"confirmed_at,omitempty"`
	DeclinedAt       *time.Time            `json:"declined_at,omitempty"`
	CancelledAt      *time.Time            `json:"cancelled_at,omitempty"`
	CancelledBy      *uuid.UUID            `json:"cancelled_by,omitempty"`
	CompletedAt      *time.Time            `json:"completed_at,omitempty"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
}
