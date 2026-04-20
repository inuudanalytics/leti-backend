package adscenter

import (
	"time"

	"github.com/google/uuid"
)

type DurationType string

const (
	DurationDaily    DurationType = "daily"
	DurationWeekly   DurationType = "weekly"
	DurationBiweekly DurationType = "biweekly"
	DurationMonthly  DurationType = "monthly"
)

type TargetType string

const (
	TargetArtisan TargetType = "artisan"
	TargetOwner   TargetType = "owner"
)

type CampaignStatus string

const (
	StatusPending    CampaignStatus = "pending"
	StatusActive     CampaignStatus = "active"
	StatusPaused     CampaignStatus = "paused"
	StatusAutoPaused CampaignStatus = "auto_paused"
	StatusCompleted  CampaignStatus = "completed"
	StatusCancelled  CampaignStatus = "cancelled"
)

type CampaignMode string

const (
	ModeOneTime   CampaignMode = "one_time"
	ModeRecurring CampaignMode = "recurring"
)

// Campaign is the core ad campaign model.
type Campaign struct {
	ID               uuid.UUID      `json:"id"`
	UserID           uuid.UUID      `json:"user_id"`
	TargetType       TargetType     `json:"target_type"`
	PropertyID       *uuid.UUID     `json:"property_id,omitempty"`
	ArtisanServiceID *uuid.UUID     `json:"artisan_service_id,omitempty"`
	Title            string         `json:"title"`
	Description      *string        `json:"description,omitempty"`
	ImageURL         string         `json:"image_url"`
	ImagePublicID    string         `json:"-"`
	DurationType     DurationType   `json:"duration_type"`
	NumDays          int            `json:"num_days"`
	StartDate        string         `json:"start_date"` // YYYY-MM-DD
	EndDate          string         `json:"end_date"`
	Mode             CampaignMode   `json:"mode"`
	DailyPrice       float64        `json:"daily_price"`
	TotalBudget      float64        `json:"total_budget"`
	AmountSpent      float64        `json:"amount_spent"`
	PaymentMethod    string         `json:"payment_method"`
	PaymentStatus    string         `json:"payment_status"`
	PaymentReference *string        `json:"payment_reference,omitempty"`
	Status           CampaignStatus `json:"status"`
	TotalViews       int64          `json:"total_views"`
	TotalClicks      int64          `json:"total_clicks"`
	ConversionRate   float64        `json:"conversion_rate"`
	PausedReason     *string        `json:"paused_reason,omitempty"`
	LastChargedDate  *string        `json:"last_charged_date,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// CampaignWithDetail enriches Campaign with advertised entity details.
type CampaignWithDetail struct {
	Campaign
	// For owners
	PropertyName  *string  `json:"property_name,omitempty"`
	RentPrice     *float64 `json:"rent_price,omitempty"`
	PropertyCity  *string  `json:"property_city,omitempty"`
	PropertyState *string  `json:"property_state,omitempty"`
	// For artisans
	ArtisanName  *string `json:"artisan_name,omitempty"`
	ServiceName  *string `json:"service_name,omitempty"`
	CategoryName *string `json:"category_name,omitempty"`
	ArtisanCity  *string `json:"artisan_city,omitempty"`
}

// DashboardMetrics is the top-level summary returned to a user.
type DashboardMetrics struct {
	TotalViews      int64   `json:"total_views"`
	TotalClicks     int64   `json:"total_clicks"`
	TotalSpent      float64 `json:"total_spent"`
	ConversionRate  float64 `json:"conversion_rate"`
	ActiveCampaigns int     `json:"active_campaigns"`
}

// Dashboard bundles metrics + active campaigns.
type Dashboard struct {
	Metrics         DashboardMetrics     `json:"metrics"`
	ActiveCampaigns []CampaignWithDetail `json:"active_campaigns"`
}

// CreateCampaignRequest is the JSON body for POST /ads/campaigns.
// Image is handled separately via multipart.
type CreateCampaignRequest struct {
	Title            string       `json:"title"`
	Description      string       `json:"description"`
	DurationType     DurationType `json:"duration_type"`
	Mode             CampaignMode `json:"mode"`
	StartDate        string       `json:"start_date"`     // YYYY-MM-DD; "now" = today
	PaymentMethod    string       `json:"payment_method"` // wallet | paystack
	PropertyID       string       `json:"property_id"`
	ArtisanServiceID string       `json:"artisan_service_id"`
}

// AdPricing holds the current admin-configured rates.
type AdPricing struct {
	ArtisanDailyPrice float64 `json:"artisan_daily_price"`
	OwnerDailyPrice   float64 `json:"owner_daily_price"`
}

// DailyCharge represents one daily deduction log entry.
type DailyCharge struct {
	ID            uuid.UUID `json:"id"`
	CampaignID    uuid.UUID `json:"campaign_id"`
	ChargeDate    string    `json:"charge_date"`
	Amount        float64   `json:"amount"`
	Status        string    `json:"status"`
	FailureReason *string   `json:"failure_reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}
