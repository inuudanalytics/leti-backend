package utils

import (
	"fmt"
	"time"
)

// ============================================================================
// 1. Booking Request  →  artisan
// ============================================================================
func SendBookingRequestEmail(to, artisanName, clientName, serviceName, bookingDate, startTime string) error {
	subject := fmt.Sprintf("📅 New Booking Request – %s", bookingDate)

	content := fmt.Sprintf(`
    <h2>New Booking Request 📅</h2>
    <p>Hello %s, you have a new booking request on Leti. Review the details below and accept or decline from the app.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">👤</span>
        <p class="feature-text"><strong>Client:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">🛠️</span>
        <p class="feature-text"><strong>Service:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📆</span>
        <p class="feature-text"><strong>Date:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">🕐</span>
        <p class="feature-text"><strong>Time:</strong> %s</p>
      </div>
    </div>
    <div class="tip-box">
      <p>⏰ <strong>Respond quickly</strong> — booking requests are time-sensitive. Clients may hire someone else if there is no response.</p>
    </div>
    <div class="cta"><a href="https://leti.app">View Request</a></div>
    <div class="divider"></div>
    <p class="note">If you are unavailable for this slot, simply decline from the app and the client will be notified immediately.</p>
  `, artisanName, clientName, serviceName, bookingDate, startTime)

	body := letiEmailShell("New Booking Request", content, time.Now().Year())
	return SendEmail(to, subject, body)
}

// ============================================================================
// 2. Booking Confirmed  →  client  (artisan accepted; payment now required)
// ============================================================================
func SendBookingConfirmedEmail(to, clientName, artisanName, serviceName, bookingDate, startTime, totalPrice string) error {
	subject := "✅ Booking Confirmed – Complete Your Payment"

	content := fmt.Sprintf(`
    <h2>Booking Confirmed ✅</h2>
    <p>Hello %s, great news — <strong>%s</strong> has accepted your booking. Complete your payment now to lock in the slot.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">🛠️</span>
        <p class="feature-text"><strong>Service:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📆</span>
        <p class="feature-text"><strong>Date:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">🕐</span>
        <p class="feature-text"><strong>Time:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💳</span>
        <p class="feature-text"><strong>Amount Due:</strong> &#8358;%s</p>
      </div>
    </div>
    <div class="tip-box">
      <p>🔒 Your payment is held <strong>securely in escrow</strong> and is only released to the artisan after the service is marked as completed.</p>
    </div>
    <div class="cta"><a href="https://leti.app">Pay Now</a></div>
    <div class="divider"></div>
    <p class="note">Your slot is not fully secured until payment is completed. Please pay promptly to avoid losing the booking.</p>
  `, clientName, artisanName, serviceName, bookingDate, startTime, totalPrice)

	body := letiEmailShell("Booking Confirmed", content, time.Now().Year())
	return SendEmail(to, subject, body)
}

// ============================================================================
// 3. Booking Declined  →  client
// ============================================================================
func SendBookingDeclinedEmail(to, clientName, artisanName, serviceName, bookingDate string) error {
	subject := "❌ Booking Request Declined"

	content := fmt.Sprintf(`
    <h2>Booking Request Declined ❌</h2>
    <p>Hello %s, unfortunately <strong>%s</strong> is unavailable and has declined your booking request. Don't worry — there are plenty of other artisans ready to help.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">🛠️</span>
        <p class="feature-text"><strong>Service:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📆</span>
        <p class="feature-text"><strong>Requested Date:</strong> %s</p>
      </div>
    </div>
    <div class="tip-box">
      <p>💡 <strong>Tip:</strong> Check artisan ratings and availability before sending your next request to improve your chances of a quick response.</p>
    </div>
    <div class="cta"><a href="https://leti.app">Find Another Artisan</a></div>
    <div class="divider"></div>
    <p class="note">No payment was taken. You can browse and book another artisan at any time from the app.</p>
  `, clientName, artisanName, serviceName, bookingDate)

	body := letiEmailShell("Booking Declined", content, time.Now().Year())
	return SendEmail(to, subject, body)
}

// ============================================================================
// 4. Booking Cancelled  →  the other party
// cancelledByRole: "client" or "artisan" — determines the message wording.
// ============================================================================
func SendBookingCancelledEmail(to, recipientName, otherPartyName, serviceName, bookingDate, cancelledByRole string) error {
	subject := "🚫 Booking Cancelled"

	cancelledByLabel := "The client"
	if cancelledByRole == "artisan" {
		cancelledByLabel = "The artisan"
	}

	content := fmt.Sprintf(`
    <h2>Booking Cancelled 🚫</h2>
    <p>Hello %s, %s (<strong>%s</strong>) has cancelled the following booking.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">🛠️</span>
        <p class="feature-text"><strong>Service:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📆</span>
        <p class="feature-text"><strong>Date:</strong> %s</p>
      </div>
    </div>
    <div class="tip-box">
      <p>💳 If a payment was made, a <strong>full refund</strong> will be processed back to your original payment method within 1–3 business days.</p>
    </div>
    <div class="cta"><a href="https://leti.app">Open App</a></div>
    <div class="divider"></div>
    <p class="note">If you believe this cancellation was made in error, please contact our support team through the app.</p>
  `, recipientName, cancelledByLabel, otherPartyName, serviceName, bookingDate)

	body := letiEmailShell("Booking Cancelled", content, time.Now().Year())
	return SendEmail(to, subject, body)
}

// ============================================================================
// 5. Booking Completed  →  client
// ============================================================================
func SendBookingCompletedEmail(to, clientName, artisanName, serviceName, bookingDate string) error {
	subject := "🎉 Service Completed – Leave a Review"

	content := fmt.Sprintf(`
    <h2>Service Completed 🎉</h2>
    <p>Hello %s, <strong>%s</strong> has marked your booking as completed. We hope everything went smoothly!</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">🛠️</span>
        <p class="feature-text"><strong>Service:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📆</span>
        <p class="feature-text"><strong>Date:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">⭐</span>
        <p class="feature-text">Share your experience to help other clients make better decisions</p>
      </div>
    </div>
    <div class="tip-box">
      <p>💳 Your escrow payment has been <strong>released to the artisan</strong>. If there is any issue with the service, please open a dispute from the app within 24 hours.</p>
    </div>
    <div class="cta"><a href="https://leti.app">Leave a Review</a></div>
    <div class="divider"></div>
    <p class="note">Thank you for using Leti. We hope to see you again soon.</p>
  `, clientName, artisanName, serviceName, bookingDate)

	body := letiEmailShell("Service Completed", content, time.Now().Year())
	return SendEmail(to, subject, body)
}
