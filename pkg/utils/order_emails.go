package utils

import (
	"fmt"
	"time"

	shortletModels "leti_server/internal/models/shortlet"
)

// ============================================================================
// 1. Order Confirmed  →  client
// ============================================================================
func SendOrderConfirmedEmail(to, clientName, propName, checkIn, checkOut string, totalAmount float64) error {
	subject := "🏠 Booking Confirmed – Your Stay is Secured!"

	content := fmt.Sprintf(`
    <h2>Booking Confirmed 🏠</h2>
    <p>Hello %s, your shortlet booking has been confirmed and your payment is securely held. Your stay is locked in!</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">🏡</span>
        <p class="feature-text"><strong>Property:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📅</span>
        <p class="feature-text"><strong>Check-in:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📅</span>
        <p class="feature-text"><strong>Check-out:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💳</span>
        <p class="feature-text"><strong>Total Paid:</strong> &#8358;%.2f</p>
      </div>
    </div>
    <div class="tip-box">
      <p>🔒 Your payment is held <strong>securely in escrow</strong> and released to the property owner after your check-out.</p>
    </div>
    <div class="cta"><a href="https://leti.app">View Booking</a></div>
    <div class="divider"></div>
    <p class="note">Please arrive on time for check-in. Contact the owner through the Leti app if you have any questions.</p>
  `, clientName, propName, checkIn, checkOut, totalAmount)

	body := letiEmailShell("Booking Confirmed", content, time.Now().Year())
	return SendEmail(to, subject, body)
}

// ============================================================================
// 2. Order Cancelled  →  the non-cancelling party
// ============================================================================
func SendOrderCancelledEmail(to, recipientName, cancellerName, propName, checkIn string, refundProcessed bool) error {
	subject := "🚫 Booking Cancelled"

	refundNote := "No payment was made, so no refund is required."
	if refundProcessed {
		refundNote = "A <strong>full refund</strong> has been processed to your wallet and should be available immediately."
	}

	content := fmt.Sprintf(`
    <h2>Booking Cancelled 🚫</h2>
    <p>Hello %s, <strong>%s</strong> has cancelled the following booking.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">🏡</span>
        <p class="feature-text"><strong>Property:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📅</span>
        <p class="feature-text"><strong>Check-in Date:</strong> %s</p>
      </div>
    </div>
    <div class="tip-box">
      <p>💳 %s</p>
    </div>
    <div class="cta"><a href="https://leti.app">Open App</a></div>
    <div class="divider"></div>
    <p class="note">If you believe this cancellation was made in error, please contact support through the app.</p>
  `, recipientName, cancellerName, propName, checkIn, refundNote)

	body := letiEmailShell("Booking Cancelled", content, time.Now().Year())
	return SendEmail(to, subject, body)
}

// ============================================================================
// 3. Check-in Reminder  →  client (sent 24h before check-in)
// ============================================================================
func SendCheckinReminderEmail(to, clientName, propName, checkIn, checkInTime, ownerName string) error {
	subject := "⏰ Check-in Tomorrow – Get Ready!"

	content := fmt.Sprintf(`
    <h2>Check-in Reminder ⏰</h2>
    <p>Hello %s, your stay at <strong>%s</strong> begins <strong>tomorrow</strong>. Here's what you need to know.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">🏡</span>
        <p class="feature-text"><strong>Property:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📅</span>
        <p class="feature-text"><strong>Check-in Date:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">🕒</span>
        <p class="feature-text"><strong>Check-in Time:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">👤</span>
        <p class="feature-text"><strong>Host:</strong> %s</p>
      </div>
    </div>
    <div class="tip-box">
      <p>📱 <strong>Tip:</strong> You can message your host directly through the Leti app if you need directions or have special requests.</p>
    </div>
    <div class="cta"><a href="https://leti.app">View Booking Details</a></div>
    <div class="divider"></div>
    <p class="note">Safe travels! We hope you have a wonderful stay.</p>
  `, clientName, propName, propName, checkIn, checkInTime, ownerName)

	body := letiEmailShell("Check-in Reminder", content, time.Now().Year())
	return SendEmail(to, subject, body)
}

// ============================================================================
// 4. Order Receipt  →  client
// ============================================================================
func SendOrderReceiptEmail(to string, receipt shortletModels.OrderReceipt) error {
	subject := fmt.Sprintf("🧾 Payment Receipt – %s", receipt.ReceiptRef)

	content := fmt.Sprintf(`
    <h2>Payment Receipt 🧾</h2>
    <p>Hello %s, thank you for booking with Leti. Here is your payment receipt for your records.</p>

    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">🧾</span>
        <p class="feature-text"><strong>Receipt No:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">🏡</span>
        <p class="feature-text"><strong>Property:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">👤</span>
        <p class="feature-text"><strong>Host:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📅</span>
        <p class="feature-text"><strong>Check-in:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📅</span>
        <p class="feature-text"><strong>Check-out:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">🌙</span>
        <p class="feature-text"><strong>Nights:</strong> %d</p>
      </div>
    </div>

    <h3 style="margin-top:24px; color:#1a1a2e;">Payment Breakdown</h3>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">💵</span>
        <p class="feature-text"><strong>Price per Night:</strong> &#8358;%.2f</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">🧮</span>
        <p class="feature-text"><strong>Subtotal (%d nights):</strong> &#8358;%.2f</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">🛡️</span>
        <p class="feature-text"><strong>Caution Fee (refundable):</strong> &#8358;%.2f</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💳</span>
        <p class="feature-text"><strong>Total Paid:</strong> &#8358;%.2f</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💼</span>
        <p class="feature-text"><strong>Payment Method:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">🕒</span>
        <p class="feature-text"><strong>Paid At:</strong> %s</p>
      </div>
    </div>

    <div class="tip-box">
      <p>🛡️ Your payment is held in escrow and released to the host after check-out. The caution fee is refunded to your wallet once your stay is confirmed completed without damages.</p>
    </div>
    <div class="cta"><a href="https://leti.app">View Booking</a></div>
    <div class="divider"></div>
    <p class="note">Keep this receipt for your records. Booking ID: %s</p>
  `,
		receipt.ClientName,
		receipt.ReceiptRef,
		receipt.Property.Name,
		receipt.OwnerName,
		receipt.Order.CheckInDate,
		receipt.Order.CheckOutDate,
		receipt.Summary.NumNights,
		receipt.Summary.PricePerNight,
		receipt.Summary.NumNights,
		receipt.Summary.Subtotal,
		receipt.Summary.CautionFee,
		receipt.Summary.TotalAmount,
		receipt.PaymentMethod,
		receipt.PaidAt.Format("02 Jan 2006, 03:04 PM"),
		receipt.Order.ID,
	)

	emailBody := letiEmailShell("Payment Receipt", content, time.Now().Year())
	return SendEmail(to, subject, emailBody)
}

// ============================================================================
// 5. New Booking Notification  →  owner
// ============================================================================
func SendNewBookingOwnerEmail(to, ownerName, clientName, propName, checkIn, checkOut string, totalAmount float64) error {
	subject := "🎉 New Booking Received!"

	content := fmt.Sprintf(`
    <h2>New Booking! 🎉</h2>
    <p>Hello %s, great news — <strong>%s</strong> has booked your property. Payment has been received and held in escrow.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">🏡</span>
        <p class="feature-text"><strong>Property:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">👤</span>
        <p class="feature-text"><strong>Guest:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📅</span>
        <p class="feature-text"><strong>Check-in:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📅</span>
        <p class="feature-text"><strong>Check-out:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💰</span>
        <p class="feature-text"><strong>Total Booking Value:</strong> &#8358;%.2f</p>
      </div>
    </div>
    <div class="tip-box">
      <p>💳 Payment is held in escrow and will be released to your wallet after the guest checks out.</p>
    </div>
    <div class="cta"><a href="https://leti.app">View Booking</a></div>
    <div class="divider"></div>
    <p class="note">Please be ready to welcome your guest on their check-in date. Mark check-in and check-out through the Leti app.</p>
  `, ownerName, clientName, propName, clientName, checkIn, checkOut, totalAmount)

	emailBody := letiEmailShell("New Booking Received", content, time.Now().Year())
	return SendEmail(to, subject, emailBody)
}

// ============================================================================
// 6. Payment Released  →  owner (post check-out)
// ============================================================================
func SendPaymentReleasedEmail(to, ownerName, propName string, netPayout float64) error {
	subject := "💰 Your Payout Has Been Released"

	content := fmt.Sprintf(`
    <h2>Payout Released 💰</h2>
    <p>Hello %s, your guest has checked out and your payout has been released to your Leti wallet.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">🏡</span>
        <p class="feature-text"><strong>Property:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💵</span>
        <p class="feature-text"><strong>Net Payout:</strong> &#8358;%.2f</p>
      </div>
    </div>
    <div class="tip-box">
      <p>🏦 You can withdraw your earnings to your bank account from the Wallet section of the app.</p>
    </div>
    <div class="cta"><a href="https://leti.app">Go to Wallet</a></div>
    <div class="divider"></div>
    <p class="note">Thank you for hosting on Leti. We appreciate your contribution to our community.</p>
  `, ownerName, propName, netPayout)

	emailBody := letiEmailShell("Payout Released", content, time.Now().Year())
	return SendEmail(to, subject, emailBody)
}
