package utils

import (
	"fmt"
	"time"
)

// ============================================================================
// SendAdPausedEmail — campaign auto-paused due to insufficient wallet balance
// ============================================================================
func SendAdPausedEmail(email, name string, required, available float64, campaignID string) error {
	shortfall := required - available
	subject := "⚠️ Your Ad Campaign Has Been Paused"

	content := fmt.Sprintf(`
    <h2>Your Ad Campaign Was Paused ⚠️</h2>
    <p>Hello %s, your ad campaign could not be charged today because your wallet balance is too low. The campaign has been automatically paused until you top up.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">💳</span>
        <p class="feature-text"><strong>Today's Charge:</strong> &#8358;%.2f</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💰</span>
        <p class="feature-text"><strong>Current Balance:</strong> &#8358;%.2f</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">⬆️</span>
        <p class="feature-text"><strong>Amount Needed to Resume:</strong> &#8358;%.2f</p>
      </div>
    </div>
    <div class="tip-box">
      <p>🔄 <strong>To resume your campaign</strong>, top up your wallet with at least &#8358;%.2f, then go to <strong>Ads → My Campaigns</strong> and tap <em>Resume</em>.</p>
    </div>
    <div class="cta"><a href="https://leti.app">Top Up Wallet</a></div>
    <div class="divider"></div>
    <p class="note">Campaign ID: %s. Your ad will not run while paused — resume it before your end date to get full value from your campaign.</p>
  `, name, required, available, shortfall, shortfall, campaignID)

	body := letiEmailShell("Ad Campaign Paused", content, time.Now().Year())
	return SendEmail(email, subject, body)
}

// ============================================================================
// SendAdPausedSMS — SMS version of the auto-pause notification
// ============================================================================
func SendAdPausedSMS(phone, name string, required, available float64) error {
	shortfall := required - available
	msg := fmt.Sprintf(
		"Hi %s, your Leti ad campaign has been paused. Your wallet balance is too low for today's charge. Top up ₦%.2f via the app to resume. Leti > Wallet > Fund.",
		name, shortfall,
	)
	return sendTermiiSMS(phone, msg)
}

// ============================================================================
// SendAdCampaignStartedEmail — campaign activated and now live
// ============================================================================
func SendAdCampaignStartedEmail(email, name, campaignTitle, endDate string) error {
	subject := "🚀 Your Ad Campaign Is Now Live!"

	content := fmt.Sprintf(`
    <h2>Your Ad Is Live! 🚀</h2>
    <p>Hello %s, great news — your ad campaign is now active and being shown to potential clients on Leti. Sit back and let the visibility work for you.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">📢</span>
        <p class="feature-text"><strong>Campaign:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📅</span>
        <p class="feature-text"><strong>Runs Until:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📊</span>
        <p class="feature-text">Track views, clicks, and performance in real time from the Ads Center</p>
      </div>
    </div>
    <div class="tip-box">
      <p>💡 <strong>Pro Tip:</strong> Keep your profile and portfolio up to date while your ad is running — first impressions count!</p>
    </div>
    <div class="cta"><a href="https://leti.app">View Campaign</a></div>
    <div class="divider"></div>
    <p class="note">Your wallet will be charged the daily rate each day the campaign runs. Ensure your balance stays topped up to avoid interruptions.</p>
  `, name, campaignTitle, endDate)

	body := letiEmailShell("Ad Campaign Live", content, time.Now().Year())
	return SendEmail(email, subject, body)
}

// ============================================================================
// SendAdCampaignStartedSMS — SMS version of campaign-live notification
// ============================================================================
func SendAdCampaignStartedSMS(phone, name, campaignTitle string) error {
	msg := fmt.Sprintf(
		"Hi %s, your Leti ad campaign \"%s\" is now live! Clients can see your listing. Track performance in the app under Ads > My Campaigns.",
		name, campaignTitle,
	)
	return sendTermiiSMS(phone, msg)
}

// ============================================================================
// SendAdCampaignEndedEmail — one-time campaign completed its full run
// ============================================================================
func SendAdCampaignEndedEmail(email, name, campaignTitle string, totalViews, totalClicks int64, totalSpent float64) error {
	subject := "🏁 Your Ad Campaign Has Ended"

	ctr := 0.0
	if totalViews > 0 {
		ctr = float64(totalClicks) / float64(totalViews) * 100
	}

	content := fmt.Sprintf(`
    <h2>Campaign Completed 🏁</h2>
    <p>Hello %s, your ad campaign has finished its run. Here is a summary of how it performed.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">📢</span>
        <p class="feature-text"><strong>Campaign:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">👁️</span>
        <p class="feature-text"><strong>Total Views:</strong> %d</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">🖱️</span>
        <p class="feature-text"><strong>Total Clicks:</strong> %d</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📈</span>
        <p class="feature-text"><strong>Click-Through Rate:</strong> %.1f%%</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💳</span>
        <p class="feature-text"><strong>Total Spent:</strong> &#8358;%.2f</p>
      </div>
    </div>
    <div class="tip-box">
      <p>🔄 <strong>Keep the momentum going!</strong> Start a new campaign to stay visible to clients searching for your services on Leti.</p>
    </div>
    <div class="cta"><a href="https://leti.app">Start New Campaign</a></div>
    <div class="divider"></div>
    <p class="note">Thank you for advertising on Leti. Your detailed analytics are always available in the Ads Center.</p>
  `, name, campaignTitle, totalViews, totalClicks, ctr, totalSpent)

	body := letiEmailShell("Campaign Completed", content, time.Now().Year())
	return SendEmail(email, subject, body)
}

// ============================================================================
// SendAdCampaignEndedSMS — SMS version of campaign-ended notification
// ============================================================================
func SendAdCampaignEndedSMS(phone, name, campaignTitle string, totalViews, totalClicks int64) error {
	msg := fmt.Sprintf(
		"Hi %s, your Leti ad campaign \"%s\" has ended. It got %d views and %d clicks. Start a new campaign in the app to stay visible!",
		name, campaignTitle, totalViews, totalClicks,
	)
	return sendTermiiSMS(phone, msg)
}

// ============================================================================
// SendAdPaymentConfirmedEmail — Paystack payment received, campaign scheduled
// ============================================================================
func SendAdPaymentConfirmedEmail(email, name, campaignTitle, startDate, endDate string, totalBudget float64) error {
	subject := "✅ Ad Payment Confirmed – Campaign Scheduled"

	content := fmt.Sprintf(`
    <h2>Payment Confirmed ✅</h2>
    <p>Hello %s, your ad campaign payment has been received successfully. Your campaign is scheduled and will go live on its start date.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">📢</span>
        <p class="feature-text"><strong>Campaign:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">🗓️</span>
        <p class="feature-text"><strong>Start Date:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📅</span>
        <p class="feature-text"><strong>End Date:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💳</span>
        <p class="feature-text"><strong>Total Paid:</strong> &#8358;%.2f</p>
      </div>
    </div>
    <div class="tip-box">
      <p>📊 You can monitor your campaign's views and clicks in real time from <strong>Ads → My Campaigns</strong> once it goes live.</p>
    </div>
    <div class="cta"><a href="https://leti.app">View My Campaigns</a></div>
    <div class="divider"></div>
    <p class="note">You can suspend or cancel your campaign at any time from the app before its end date.</p>
  `, name, campaignTitle, startDate, endDate, totalBudget)

	body := letiEmailShell("Ad Payment Confirmed", content, time.Now().Year())
	return SendEmail(email, subject, body)
}

// ============================================================================
// SendAdPaymentConfirmedSMS — SMS version of Paystack payment confirmation
// ============================================================================
func SendAdPaymentConfirmedSMS(phone, name, campaignTitle, startDate string, totalBudget float64) error {
	msg := fmt.Sprintf(
		"Hi %s, your Leti ad payment of ₦%.2f for \"%s\" was confirmed. Your campaign starts on %s. Track it in the app under Ads.",
		name, totalBudget, campaignTitle, startDate,
	)
	return sendTermiiSMS(phone, msg)
}

// ============================================================================
// SendAdLowBalanceEmail — proactive warning when balance is getting low
// ============================================================================
func SendAdLowBalanceEmail(email, name string, currentBalance, dailyCharge float64, daysRemaining int, campaignTitle string) error {
	subject := "⚠️ Low Wallet Balance – Your Ad May Be Paused Soon"

	content := fmt.Sprintf(`
    <h2>Low Wallet Balance ⚠️</h2>
    <p>Hello %s, your wallet balance is running low. Based on your current balance and daily ad charge, your campaign may be paused soon if you do not top up.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">📢</span>
        <p class="feature-text"><strong>Campaign:</strong> %s</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💰</span>
        <p class="feature-text"><strong>Current Balance:</strong> &#8358;%.2f</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💳</span>
        <p class="feature-text"><strong>Daily Charge:</strong> &#8358;%.2f</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📅</span>
        <p class="feature-text"><strong>Estimated Days Remaining:</strong> %d day(s)</p>
      </div>
    </div>
    <div class="tip-box">
      <p>💡 Top up your wallet now to ensure your campaign runs without interruption. A paused campaign loses its daily slot and visibility.</p>
    </div>
    <div class="cta"><a href="https://leti.app">Top Up Wallet</a></div>
    <div class="divider"></div>
    <p class="note">If your balance reaches zero before the next daily charge, your campaign will be automatically paused. You can always resume it after topping up.</p>
  `, name, campaignTitle, currentBalance, dailyCharge, daysRemaining)

	body := letiEmailShell("Low Wallet Balance", content, time.Now().Year())
	return SendEmail(email, subject, body)
}

// ============================================================================
// SendAdLowBalanceSMS — SMS version of low balance warning
// ============================================================================
func SendAdLowBalanceSMS(phone, name string, daysRemaining int, campaignTitle string) error {
	msg := fmt.Sprintf(
		"Hi %s, your Leti wallet is running low. Your ad campaign \"%s\" has about %d day(s) before it may pause. Top up now via the app to keep it running.",
		name, campaignTitle, daysRemaining,
	)
	return sendTermiiSMS(phone, msg)
}
