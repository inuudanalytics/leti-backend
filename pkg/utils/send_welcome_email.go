package utils

import (
	"fmt"
	"time"
)

// shared HTML shell — keeps all three emails consistent
func letiEmailShell(title, bodyContent string, year int) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>%s – Leti</title>
  <link href="https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;500;600&display=swap" rel="stylesheet">
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: 'DM Sans', sans-serif; background-color: #f7f5fb; color: #1a1a2e; }
    .wrap { max-width: 520px; margin: 48px auto; padding: 0 16px; }
    .logo { text-align: center; margin-bottom: 28px; }
    .logo img { height: 200px; width: auto; display: inline-block; }
    .card { background: #ffffff; border-radius: 16px; overflow: hidden; border: 1px solid #ede8f5; }
    .accent-bar { height: 4px; background: linear-gradient(90deg, #C103FF, #D901F7, #2F0261); }
    .body { padding: 36px 40px 32px; }
    h2 { font-size: 20px; font-weight: 600; color: #1a1a2e; margin-bottom: 12px; }
    p { font-size: 14px; line-height: 1.8; color: #5a5a7a; margin-bottom: 20px; }
    .features { border: 1px solid #ede8f5; border-radius: 12px; overflow: hidden; margin-bottom: 28px; }
    .feature-item { display: flex; align-items: flex-start; gap: 14px; padding: 14px 18px; border-bottom: 1px solid #f5f0fc; }
    .feature-item:last-child { border-bottom: none; }
    .feature-icon { font-size: 16px; flex-shrink: 0; margin-top: 1px; }
    .feature-text { font-size: 13px; color: #4a4a6a; line-height: 1.5; margin: 0; }
    .cta { text-align: center; margin-bottom: 28px; }
    .cta a {
      display: inline-block;
      background: linear-gradient(135deg, #C103FF, #2F0261);
      color: #ffffff; text-decoration: none;
      padding: 13px 36px; border-radius: 10px;
      font-size: 14px; font-weight: 600; letter-spacing: 0.2px;
    }
    .tip-box {
      background: #faf8ff; border: 1px solid #e8dff7;
      border-radius: 10px; padding: 14px 18px; margin-bottom: 24px;
    }
    .tip-box p { font-size: 13px; color: #6a5a8a; margin: 0; line-height: 1.6; }
    .tip-box strong { color: #2F0261; }
    .divider { height: 1px; background: #f0ebfa; margin-bottom: 20px; }
    .note { font-size: 12px; color: #aaa0be; line-height: 1.6; margin: 0; text-align: center; }
    .footer { padding: 20px 40px; text-align: center; border-top: 1px solid #f0ebfa; }
    .footer p { font-size: 12px; color: #c0b8d4; margin: 0; }
  </style>
</head>
<body>
  <div class="wrap">
    <div class="logo">
      <img src="https://res.cloudinary.com/dya9hsgjs/image/upload/v1773944719/letilogo_g5xf5i.png" alt="Leti" />
    </div>
    <div class="card">
      <div class="accent-bar"></div>
      <div class="body">
        %s
      </div>
      <div class="footer">
        <p>&copy; %d Leti. All rights reserved.</p>
      </div>
    </div>
  </div>
</body>
</html>`, title, bodyContent, year)
}

// ============================================================================
// SendWelcomeEmailClient
// ============================================================================
func SendWelcomeEmailClient(to, username string) error {
	subject := fmt.Sprintf("Welcome to Leti, %s", username)

	content := fmt.Sprintf(`
    <h2>Welcome, %s 👋</h2>
    <p>You're all set to start exploring on Leti. Find shortlets, book stays, and hire service providers — all in one place.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">🏠</span>
        <p class="feature-text">Browse and book verified shortlets near you</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">🔧</span>
        <p class="feature-text">Hire trusted artisans for any job you need done</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💬</span>
        <p class="feature-text">Message hosts and providers directly in the app</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">⭐</span>
        <p class="feature-text">Leave reviews to help others make better decisions</p>
      </div>
    </div>
    <div class="tip-box">
      <p>💡 Did you know? You can switch to an <strong>Owner</strong> or <strong>Artisan</strong> account anytime — no new signup needed.</p>
    </div>
    <div class="cta"><a href="https://leti.app">Start exploring</a></div>
    <div class="divider"></div>
    <p class="note">Questions? Reply to this email and our team will get back to you.</p>
  `, username)

	body := letiEmailShell("Welcome", content, time.Now().Year())
	return SendEmail(to, subject, body)
}

// ============================================================================
// SendWelcomeEmailOwner
// ============================================================================
func SendWelcomeEmailOwner(to, username string) error {
	subject := fmt.Sprintf("Welcome to Leti, %s — your listing awaits", username)

	content := fmt.Sprintf(`
    <h2>Welcome, %s 👋</h2>
    <p>You're ready to list your space on Leti and start receiving bookings from verified guests across the platform.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">📋</span>
        <p class="feature-text">Create your shortlet listing with photos, pricing, and availability</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📅</span>
        <p class="feature-text">Manage bookings and your calendar from one dashboard</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💳</span>
        <p class="feature-text">Receive payments securely and track your earnings</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">🛡️</span>
        <p class="feature-text">Every guest is verified before they can make a booking</p>
      </div>
    </div>
    <div class="tip-box">
      <p>💡 You can also switch to a <strong>Client</strong> account to book other shortlets or hire artisans — using the same login.</p>
    </div>
    <div class="cta"><a href="https://leti.app">Create your listing</a></div>
    <div class="divider"></div>
    <p class="note">Questions? Reply to this email and our team will get back to you.</p>
  `, username)

	body := letiEmailShell("Welcome Owner", content, time.Now().Year())
	return SendEmail(to, subject, body)
}

// ============================================================================
// SendWelcomeEmailArtisan
// ============================================================================
func SendWelcomeEmailArtisan(to, username string) error {
	subject := fmt.Sprintf("Welcome to Leti, %s — clients are looking for you", username)

	content := fmt.Sprintf(`
    <h2>Welcome, %s 👋</h2>
    <p>Your artisan profile is live on Leti. Clients nearby can now find and hire you for the services you offer.</p>
    <div class="features">
      <div class="feature-item">
        <span class="feature-icon">🛠️</span>
        <p class="feature-text">Set up your profile with your skills, rates, and availability</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">📍</span>
        <p class="feature-text">Get discovered by clients in your area looking for your expertise</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💬</span>
        <p class="feature-text">Chat with clients before accepting a job request</p>
      </div>
      <div class="feature-item">
        <span class="feature-icon">💳</span>
        <p class="feature-text">Get paid securely through the platform after every job</p>
      </div>
    </div>
    <div class="tip-box">
      <p>💡 You can also switch to a <strong>Client</strong> account to book shortlets or hire other artisans — no separate account needed.</p>
    </div>
    <div class="cta"><a href="https://leti.app">Complete your profile</a></div>
    <div class="divider"></div>
    <p class="note">Questions? Reply to this email and our team will get back to you.</p>
  `, username)

	body := letiEmailShell("Welcome Artisan", content, time.Now().Year())
	return SendEmail(to, subject, body)
}
