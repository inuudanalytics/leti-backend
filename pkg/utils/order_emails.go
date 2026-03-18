package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// ============================================================================
// SendOrderPlacedEmail — to buyer after successful checkout
// ============================================================================
func SendOrderPlacedEmail(to, buyerName string, orderID string, totalAmount float64, itemCount int, fulfillmentType string, pickupCode string) error {
	subject := "🛒 Order Confirmed - BrodaMeko"

	// ── Fulfillment-specific copy ─────────────────────────────────────────────
	var (
		totalLabel  string
		warningHTML string
		actionHTML  string
		escrowNote  string
	)

	if fulfillmentType == "pickup" {
		totalLabel = "Total Amount"

		pickupCodeBlock := ""
		if pickupCode != "" {
			pickupCodeBlock = fmt.Sprintf(`
				<hr class="divider"/>
				<p class="order-card-label">Your Pickup Code</p>
				<p class="order-card-value" style="font-size: 28px; letter-spacing: 8px; font-weight: 700;">%s</p>
				<p style="font-size: 11px; color: #000033; margin: 0;">Show this code to the seller when you arrive to collect your order.</p>
			`, pickupCode)
		}

		warningHTML = fmt.Sprintf(`
			<div class="warning-box">
				<p>🏪 <strong style="color:#ff9900;">This is a pickup order.</strong></p>
				<p>The seller will contact you via the <strong style="color:#ff9900;">BrodaMeko app</strong>
				to confirm when your items are ready for collection.</p>
				<p>When you arrive at the store, <strong style="color:#ff9900;">show your 4-digit pickup code</strong>
				to the seller to complete the handover and release payment.</p>
				%s
			</div>
		`, pickupCodeBlock)

		actionHTML = `
			<div class="action-box">
				<p>The seller has been notified and is preparing your order.</p>
				<p>Open <span class="highlight">BrodaMeko</span> to track your order and receive the seller's call.</p>
			</div>
		`

		escrowNote = `Your payment is safely held in escrow and will only be released to the
					seller after you present your pickup code at the store.`
	} else {
		totalLabel = "Total Amount (excl. delivery)"

		warningHTML = `
			<div class="warning-box">
				<p>⚠️ <strong style="color:#ff9900;">Delivery fee is NOT included</strong> in your order total.</p>
				<p>📞 The seller will call you <strong style="color:#ff9900;">through the BrodaMeko app</strong>
				to discuss and agree on the delivery fee before dispatching your item.</p>
				<p>Please keep the app open and available to receive the seller's call.</p>
				<p>If you and the seller cannot agree on delivery terms, either party may cancel the order
				and you will receive a <strong style="color:#ff9900;">full refund</strong> to your wallet.</p>
			</div>
		`

		actionHTML = `
			<div class="action-box">
				<p>Sellers have been notified and are reviewing your order.</p>
				<p>Open <span class="highlight">BrodaMeko</span> to track your order status and receive the seller's call.</p>
			</div>
		`

		escrowNote = `Your payment is safely held in escrow and will only be released to the
					seller after you confirm receipt of your items.`
	}

	body := fmt.Sprintf(`
	<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8"/>
		<title>Order Confirmed</title>
		<link href="https://fonts.googleapis.com/css2?family=Poppins:wght@400;500;600;700&display=swap" rel="stylesheet">
		<style>
			body { font-family: 'Poppins', sans-serif; background-color: #0a0a1a; margin: 0; padding: 0; }
			.container { max-width: 560px; margin: 40px auto; background: #000033;
				border-radius: 16px; overflow: hidden; border-top: 5px solid #E2FF31; }
			.header { background: linear-gradient(135deg, #000033 0%%, #000055 100%%);
				color: #E2FF31; text-align: center; padding: 30px 20px; }
			.header h1 { margin: 0; font-size: 24px; font-weight: 700; }
			.content { padding: 35px 40px; color: #ffffff; }
			.greeting { font-size: 17px; font-weight: 600; margin-bottom: 15px; color: #E2FF31; }
			.message { font-size: 15px; line-height: 1.7; color: #d0d0e8; margin-bottom: 18px; }
			.order-card { background: linear-gradient(135deg, #E2FF31 0%%, #f0ff60 100%%);
				padding: 20px 24px; margin: 20px 0; border-radius: 12px; }
			.order-card-label { font-size: 12px; color: #000033; font-weight: 700;
				text-transform: uppercase; letter-spacing: 1px; }
			.order-card-value { font-size: 15px; color: #000033; font-weight: 600;
				margin-top: 2px; margin-bottom: 12px; }
			.divider { border: none; border-top: 1px solid rgba(0,0,51,0.15); margin: 10px 0; }
			.action-box { background: #00001a; border-left: 4px solid #E2FF31;
				padding: 15px 20px; margin: 20px 0; border-radius: 8px; }
			.action-box p { margin: 0 0 8px 0; font-size: 14px; color: #b8b8d8; line-height: 1.6; }
			.warning-box { background: #1a1000; border-left: 4px solid #ff9900;
				padding: 15px 20px; margin: 20px 0; border-radius: 8px; }
			.warning-box p { margin: 0 0 8px 0; font-size: 14px; color: #ffcc66; line-height: 1.6; }
			.highlight { color: #E2FF31; font-weight: 600; }
			.footer { background: #00001a; text-align: center; padding: 20px;
				font-size: 13px; color: #8888aa; border-top: 1px solid #1a1a4d; }
			.brand { color: #E2FF31; font-weight: 600; }
		</style>
	</head>
	<body>
		<div class="container">
			<div class="header"><h1>🛒 Order Confirmed!</h1></div>
			<div class="content">
				<p class="greeting">Hello %s,</p>
				<p class="message">Your order has been placed and payment is secured in escrow.</p>
				<div class="order-card">
					<p class="order-card-label">Order Reference</p>
					<p class="order-card-value">#%s</p>
					<hr class="divider"/>
					<p class="order-card-label">Fulfillment</p>
					<p class="order-card-value">%s</p>
					<hr class="divider"/>
					<p class="order-card-label">Items</p>
					<p class="order-card-value">%d item(s)</p>
					<hr class="divider"/>
					<p class="order-card-label">%s</p>
					<p class="order-card-value">₦%.2f</p>
				</div>
				%s
				%s
				<p class="message" style="font-size: 13px; color: #9999bb; margin-top: 25px;">%s</p>
			</div>
			<div class="footer">
				&copy; %d <span class="brand">BrodaMeko</span> — Your Car's Best Friend, Anywhere.
				<br><small>Powered by iNuud Analytics</small>
			</div>
		</div>
	</body>
	</html>
	`,
		buyerName,
		orderID[:8],
		fulfillmentTypeLabel(fulfillmentType),
		itemCount,
		totalLabel,
		totalAmount,
		warningHTML,
		actionHTML,
		escrowNote,
		time.Now().Year(),
	)

	return SendEmail(to, subject, body)
}

// ============================================================================
// SendOrderShippedEmail — to buyer when seller ships
// ============================================================================
func SendOrderShippedEmail(to, buyerName, partName, storeName string) error {
	subject := "📦 Your Item Has Been Shipped - BrodaMeko"

	body := fmt.Sprintf(`
	<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8"/>
		<title>Item Shipped</title>
		<link href="https://fonts.googleapis.com/css2?family=Poppins:wght@400;500;600;700&display=swap" rel="stylesheet">
		<style>
			body { font-family: 'Poppins', sans-serif; background-color: #0a0a1a; margin: 0; padding: 0; }
			.container { max-width: 560px; margin: 40px auto; background: #000033;
				border-radius: 16px; overflow: hidden; border-top: 5px solid #E2FF31; }
			.header { background: linear-gradient(135deg, #000033 0%%, #000055 100%%);
				color: #E2FF31; text-align: center; padding: 30px 20px; }
			.header h1 { margin: 0; font-size: 24px; font-weight: 700; }
			.content { padding: 35px 40px; color: #ffffff; }
			.greeting { font-size: 17px; font-weight: 600; margin-bottom: 15px; color: #E2FF31; }
			.message { font-size: 15px; line-height: 1.7; color: #d0d0e8; }
			.item-card { background: linear-gradient(135deg, #E2FF31 0%%, #f0ff60 100%%);
				padding: 20px 24px; margin: 20px 0; border-radius: 12px; }
			.item-card-label { font-size: 12px; color: #000033; font-weight: 700; text-transform: uppercase; }
			.item-card-value { font-size: 15px; color: #000033; font-weight: 600; margin-top: 2px; }
			.action-box { background: #00001a; border-left: 4px solid #E2FF31;
				padding: 15px 20px; margin: 20px 0; border-radius: 8px; }
			.action-box p { margin: 0 0 8px 0; font-size: 14px; color: #b8b8d8; line-height: 1.6; }
			.highlight { color: #E2FF31; font-weight: 600; }
			.footer { background: #00001a; text-align: center; padding: 20px;
				font-size: 13px; color: #8888aa; border-top: 1px solid #1a1a4d; }
			.brand { color: #E2FF31; font-weight: 600; }
		</style>
	</head>
	<body>
		<div class="container">
			<div class="header"><h1>📦 Item Shipped!</h1></div>
			<div class="content">
				<p class="greeting">Hello %s,</p>
				<p class="message">Great news! Your item is on its way.</p>
				<div class="item-card">
					<p class="item-card-label">Item</p>
					<p class="item-card-value">%s</p>
					<p class="item-card-label" style="margin-top:10px;">Seller</p>
					<p class="item-card-value">%s</p>
				</div>
				<div class="action-box">
					<p>Once you receive the item, open <span class="highlight">BrodaMeko</span>
					and confirm receipt to release payment to the seller.</p>
					<p>Do not confirm receipt until you have physically received and inspected your item.</p>
				</div>
			</div>
			<div class="footer">
				&copy; %d <span class="brand">BrodaMeko</span>
				<br><small>Powered by iNuud Analytics</small>
			</div>
		</div>
	</body>
	</html>
	`, buyerName, partName, storeName, time.Now().Year())

	return SendEmail(to, subject, body)
}

// ============================================================================
// SendNewOrderSellerEmail — to seller when a new order comes in
// NO buyer phone number exposed — seller must call via in-app only
// ============================================================================
func SendNewOrderSellerEmail(to, sellerName, orderRef, buyerName string, itemSubtotal float64, itemCount int, fulfillmentType string) error {
	subject := "🔔 New Order Received - BrodaMeko"

	// ── Fulfillment-specific copy ─────────────────────────────────────────────
	var (
		contactHeading string
		contactBody    string
		warningBody    string
		actionInstruct string
	)

	if fulfillmentType == "pickup" {
		contactHeading = "📞 Call the buyer to confirm their pickup"
		contactBody = `Open the app, go to this order, and tap <strong>"Call Buyer"</strong>
					to confirm pickup timing and any other details securely through our platform.`
		warningBody = `⚠️ <strong style="color:#ff9900;">Do NOT mark this order as ready</strong> until you
					have prepared the items. Once ready, mark it in the app and the buyer will be notified
					to come in with their <strong>4-digit pickup code</strong>.
					<br><br>If you and the buyer cannot reach an agreement, either party may cancel the order.
					The buyer will receive a full refund and no penalty applies to you.`
		actionInstruct = `Open <span class="highlight">BrodaMeko</span> to view the full order details,
					call the buyer, prepare the items, and mark as ready when they can collect.`
	} else {
		contactHeading = "📞 You need to call the buyer to discuss delivery fee"
		contactBody = `Open the app, go to this order, and tap <strong>"Call Buyer"</strong>
					to reach them securely through our platform.`
		warningBody = `⚠️ <strong style="color:#ff9900;">Do NOT process this order</strong> until you have
					spoken with the buyer via the in-app call and agreed on the delivery fee and arrangement.
					<br><br>If you and the buyer cannot reach an agreement, either party may cancel the order.
					The buyer will receive a full refund and no penalty applies to you.`
		actionInstruct = `Open <span class="highlight">BrodaMeko</span> to view the full order details,
					call the buyer, and mark items as shipped once dispatched.`
	}

	body := fmt.Sprintf(`
	<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8"/>
		<title>New Order</title>
		<link href="https://fonts.googleapis.com/css2?family=Poppins:wght@400;500;600;700&display=swap" rel="stylesheet">
		<style>
			body { font-family: 'Poppins', sans-serif; background-color: #0a0a1a; margin: 0; padding: 0; }
			.container { max-width: 560px; margin: 40px auto; background: #000033;
				border-radius: 16px; overflow: hidden; border-top: 5px solid #E2FF31; }
			.header { background: linear-gradient(135deg, #000033 0%%, #000055 100%%);
				color: #E2FF31; text-align: center; padding: 30px 20px; }
			.header h1 { margin: 0; font-size: 24px; font-weight: 700; }
			.content { padding: 35px 40px; color: #ffffff; }
			.greeting { font-size: 17px; font-weight: 600; margin-bottom: 15px; color: #E2FF31; }
			.message { font-size: 15px; line-height: 1.7; color: #d0d0e8; }
			.order-card { background: linear-gradient(135deg, #E2FF31 0%%, #f0ff60 100%%);
				padding: 20px 24px; margin: 20px 0; border-radius: 12px; }
			.order-card-label { font-size: 12px; color: #000033; font-weight: 700; text-transform: uppercase; }
			.order-card-value { font-size: 15px; color: #000033; font-weight: 600;
				margin-top: 2px; margin-bottom: 12px; }
			.divider { border: none; border-top: 1px solid rgba(0,0,51,0.15); margin: 10px 0; }
			.contact-box { background: #002200; border-left: 4px solid #00cc44;
				padding: 15px 20px; margin: 20px 0; border-radius: 8px; }
			.contact-box p { margin: 0 0 8px 0; font-size: 14px; color: #99ffbb; line-height: 1.6; }
			.warning-box { background: #1a1000; border-left: 4px solid #ff9900;
				padding: 15px 20px; margin: 20px 0; border-radius: 8px; }
			.warning-box p { margin: 0 0 8px 0; font-size: 14px; color: #ffcc66; line-height: 1.6; }
			.action-box { background: #00001a; border-left: 4px solid #E2FF31;
				padding: 15px 20px; margin: 20px 0; border-radius: 8px; }
			.action-box p { margin: 0 0 8px 0; font-size: 14px; color: #b8b8d8; line-height: 1.6; }
			.highlight { color: #E2FF31; font-weight: 600; }
			.footer { background: #00001a; text-align: center; padding: 20px;
				font-size: 13px; color: #8888aa; border-top: 1px solid #1a1a4d; }
			.brand { color: #E2FF31; font-weight: 600; }
		</style>
	</head>
	<body>
		<div class="container">
			<div class="header"><h1>🔔 New Order!</h1></div>
			<div class="content">
				<p class="greeting">Hello %s,</p>
				<p class="message">You have a new order! Payment is secured in escrow and will be
				released once the order is completed.</p>

				<div class="order-card">
					<p class="order-card-label">Order Reference</p>
					<p class="order-card-value">#%s</p>
					<hr class="divider"/>
					<p class="order-card-label">Buyer</p>
					<p class="order-card-value">%s</p>
					<hr class="divider"/>
					<p class="order-card-label">Fulfillment</p>
					<p class="order-card-value">%s</p>
					<hr class="divider"/>
					<p class="order-card-label">Your Items</p>
					<p class="order-card-value">%d item(s)</p>
					<hr class="divider"/>
					<p class="order-card-label">Your Earnings (after 8%% platform fee)</p>
					<p class="order-card-value">₦%.2f</p>
				</div>

				<div class="contact-box">
					<p>%s</p>
					<p>Buyer: <strong>%s</strong></p>
					<p style="margin-top: 10px;">To protect both parties, calls must be made
					<strong>through the BrodaMeko app only.</strong></p>
					<p>%s</p>
				</div>

				<div class="warning-box">
					<p>%s</p>
				</div>

				<div class="action-box">
					<p>%s</p>
				</div>
			</div>
			<div class="footer">
				&copy; %d <span class="brand">BrodaMeko</span> — Your Car's Best Friend, Anywhere.
				<br><small>Powered by iNuud Analytics</small>
			</div>
		</div>
	</body>
	</html>
	`,
		sellerName,
		orderRef[:8],
		buyerName,
		fulfillmentTypeLabel(fulfillmentType),
		itemCount,
		itemSubtotal*0.92,
		contactHeading,
		buyerName,
		contactBody,
		warningBody,
		actionInstruct,
		time.Now().Year(),
	)

	return SendEmail(to, subject, body)
}

// fulfillmentTypeLabel returns a human-readable label for the fulfillment type.
func fulfillmentTypeLabel(ft string) string {
	if ft == "pickup" {
		return "🏪 Pickup"
	}
	return "🚚 Delivery"
}

// ============================================================================
// SendOrderPlacedSMS — to buyer
// ============================================================================
func SendOrderPlacedSMS(phoneNumber, buyerName, orderRef string, totalAmount float64, fulfillmentType string, pickupCode string) error {
	apiKey := os.Getenv("TERMII_API_KEY")
	baseURL := os.Getenv("TERMII_BASE_URL")
	senderID := os.Getenv("TERMII_SENDER_ID")

	if apiKey == "" || baseURL == "" || senderID == "" {
		return fmt.Errorf("missing Termii configuration")
	}

	var message string
	if fulfillmentType == "pickup" {
		if pickupCode != "" {
			message = fmt.Sprintf(
				"Hi %s, your pickup order #%s of ₦%.2f is confirmed. Your pickup code is: %s. The seller will notify you when ready. Bring this code to collect your order.",
				buyerName, orderRef[:8], totalAmount, pickupCode,
			)
		} else {
			message = fmt.Sprintf(
				"Hi %s, your pickup order #%s of ₦%.2f is confirmed. The seller will contact you via BrodaMeko when your items are ready to collect.",
				buyerName, orderRef[:8], totalAmount,
			)
		}
	} else {
		message = fmt.Sprintf(
			"Hi %s, your order #%s of ₦%.2f is confirmed (delivery fee excluded). The seller will call you via BrodaMeko app to agree on delivery. Keep the app open.",
			buyerName, orderRef[:8], totalAmount,
		)
	}

	return sendTermiiSMS(phoneNumber, senderID, message, apiKey, baseURL)
}

// ============================================================================
// SendOrderShippedSMS — to buyer
// ============================================================================
func SendOrderShippedSMS(phoneNumber, buyerName, partName string) error {
	apiKey := os.Getenv("TERMII_API_KEY")
	baseURL := os.Getenv("TERMII_BASE_URL")
	senderID := os.Getenv("TERMII_SENDER_ID")

	if apiKey == "" || baseURL == "" || senderID == "" {
		return fmt.Errorf("missing Termii configuration")
	}

	message := fmt.Sprintf(
		"Hi %s, '%s' has been shipped and is on its way! Open BrodaMeko to confirm receipt when it arrives. Do NOT confirm until you have received it.",
		buyerName, partName,
	)

	return sendTermiiSMS(phoneNumber, senderID, message, apiKey, baseURL)
}

// ============================================================================
// SendNewOrderSMS — to seller
// NO buyer phone number — seller must use in-app call only
// ============================================================================
func SendNewOrderSMS(phoneNumber, sellerName, buyerName string, itemCount int, earnings float64, fulfillmentType string) error {
	apiKey := os.Getenv("TERMII_API_KEY")
	baseURL := os.Getenv("TERMII_BASE_URL")
	senderID := os.Getenv("TERMII_SENDER_ID")

	if apiKey == "" || baseURL == "" || senderID == "" {
		return fmt.Errorf("missing Termii configuration")
	}

	var message string
	if fulfillmentType == "pickup" {
		message = fmt.Sprintf(
			"Hi %s, new pickup order from %s on BrodaMeko! %d item(s), ₦%.2f earnings. Prepare the items and open the app to notify the buyer when ready to collect.",
			sellerName, buyerName, itemCount, earnings,
		)
	} else {
		message = fmt.Sprintf(
			"Hi %s, new order from %s on BrodaMeko! %d item(s), ₦%.2f earnings. Open the app and use 'Call Buyer' to discuss delivery fee before processing.",
			sellerName, buyerName, itemCount, earnings,
		)
	}

	return sendTermiiSMS(phoneNumber, senderID, message, apiKey, baseURL)
}

// ============================================================================
// sendTermiiSMS — shared helper
// ============================================================================
func sendTermiiSMS(to, senderID, message, apiKey, baseURL string) error {
	payload := TermiiSMSPayload{
		To:      to,
		From:    senderID,
		Sms:     message,
		Type:    "plain",
		Channel: "generic",
		ApiKey:  apiKey,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal SMS payload: %w", err)
	}

	resp, err := http.Post(fmt.Sprintf("%s/api/sms/send", baseURL), "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to send SMS via Termii: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("termii API returned status: %d", resp.StatusCode)
	}

	return nil
}
