package utils

import (
	"fmt"
	"math"
	"time"
)

func SendOTPEmail(to, username, otp string, expiry time.Time) error {
	subject := "Your Leti verification code"
	minutesLeft := int(math.Ceil(time.Until(expiry).Minutes()))

	body := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Verification Code – Leti</title>
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
    }
    .logo {
      text-align: center;
      margin-bottom: 28px;
    }
    .logo img {
      height: 36px;
      width: auto;
      display: inline-block;
    }
    .card {
      background: #ffffff;
      border-radius: 16px;
      overflow: hidden;
      border: 1px solid #ede8f5;
    }
    .accent-bar {
      height: 4px;
      background: linear-gradient(90deg, #C103FF, #D901F7, #2F0261);
    }
    .body {
      padding: 36px 40px 32px;
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
      padding: 24px;
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
      letter-spacing: 12px;
      color: #2F0261;
      font-family: 'Courier New', monospace;
    }
    .expiry {
      font-size: 13px;
      color: #9b8ab4;
      text-align: center;
      margin-bottom: 24px;
    }
    .expiry strong {
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
      padding: 20px 40px;
      text-align: center;
      border-top: 1px solid #f0ebfa;
    }
    .footer p {
      font-size: 12px;
      color: #c0b8d4;
      margin: 0;
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
        <p>Use the code below to verify your account. Do not share this with anyone.</p>
        <div class="otp-wrap">
          <div class="otp-label">Verification Code</div>
          <div class="otp-code">%s</div>
        </div>
        <p class="expiry">Expires in <strong>%d minutes</strong></p>
        <div class="divider"></div>
        <p class="note">If you didn't request this, you can safely ignore this email. Your account has not been compromised.</p>
      </div>
      <div class="footer">
        <p>&copy; %d Leti. All rights reserved.</p>
      </div>
    </div>
  </div>
</body>
</html>`, username, otp, minutesLeft, time.Now().Year())

	return SendEmail(to, subject, body)
}
