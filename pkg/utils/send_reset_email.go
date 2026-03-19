package utils

import (
	"fmt"
	"time"
)

func SendPasswordResetEmail(to, username, otp string, expiresAt time.Time) error {
	subject := "Reset your Leti password"

	body := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Reset Password – Leti</title>
  <link href="https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;500;600&display=swap" rel="stylesheet">
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: 'DM Sans', sans-serif;
      background-color: #f7f5fb;
      color: #1a1a2e;
    }
    .wrap {
      max-width: 520px;
      margin: 48px auto;
      padding: 0 16px;
      width: 100%%;
    }
    .logo {
      text-align: center;
      margin-bottom: 28px;
    }
    .logo img {
      height: 200px;
      width: auto;
      display: inline-block;
    }
    .card {
      background: #ffffff;
      border-radius: 16px;
      overflow: hidden;
      border: 1px solid #ede8f5;
      width: 100%%;
    }
    .accent-bar {
      height: 4px;
      background: linear-gradient(90deg, #C103FF, #D901F7, #2F0261);
    }
    .body {
      padding: 32px 28px;
    }
    h2 {
      font-size: 18px;
      font-weight: 600;
      color: #1a1a2e;
      margin-bottom: 10px;
    }
    p {
      font-size: 14px;
      line-height: 1.7;
      color: #5a5a7a;
      margin-bottom: 24px;
    }
    .otp-wrap {
      background: #faf8ff;
      border: 1px solid #e8dff7;
      border-radius: 12px;
      padding: 24px 16px;
      text-align: center;
      margin-bottom: 20px;
    }
    .otp-label {
      font-size: 11px;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 1.2px;
      color: #9b8ab4;
      margin-bottom: 12px;
    }
    .otp-code {
      font-size: 38px;
      font-weight: 600;
      letter-spacing: 10px;
      color: #2F0261;
      font-family: 'Courier New', monospace;
      word-break: break-all;
    }
    .expiry-row {
      display: flex;
      align-items: center;
      gap: 8px;
      background: #faf8ff;
      border: 1px solid #e8dff7;
      border-radius: 8px;
      padding: 12px 16px;
      margin-bottom: 24px;
    }
    .expiry-row span {
      font-size: 13px;
      color: #5a5a7a;
    }
    .expiry-row strong {
      color: #2F0261;
    }
    .divider {
      height: 1px;
      background: #f0ebfa;
      margin-bottom: 20px;
    }
    .note {
      font-size: 12px;
      color: #aaa0be;
      line-height: 1.6;
      margin: 0;
    }
    .footer {
      padding: 20px 28px;
      text-align: center;
      border-top: 1px solid #f0ebfa;
    }
    .footer p {
      font-size: 12px;
      color: #c0b8d4;
      margin: 0;
    }

    @media only screen and (max-width: 480px) {
      .wrap { margin: 16px auto; padding: 0 8px; }
      .body { padding: 24px 16px; }
      .footer { padding: 16px; }
      .logo img { height: 200px; }
      .otp-code { font-size: 28px; letter-spacing: 6px; }
    }
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
        <h2>Hello %s,</h2>
        <p>We received a request to reset your password. Use the code below to proceed.</p>
        <div class="otp-wrap">
          <div class="otp-label">Reset Code</div>
          <div class="otp-code">%s</div>
        </div>
        <div class="expiry-row">
          <span>⏰ Expires at <strong>%s</strong></span>
        </div>
        <div class="divider"></div>
        <p class="note">If you didn't request a password reset, ignore this email — your password will remain unchanged and your account is safe.</p>
      </div>
      <div class="footer">
        <p>&copy; %d Leti. All rights reserved.</p>
      </div>
    </div>
  </div>
</body>
</html>`, username, otp, expiresAt.Format("3:04 PM, Jan 2 2006"), time.Now().Year())

	return SendEmail(to, subject, body)
}
