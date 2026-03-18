package utils

import (
	"fmt"
	"time"
)

// ============================================================================
// SendJobRequestEmail
// ============================================================================
func SendJobRequestEmail(to, mechanicName, ownerName, issueLabel, carMake string) error {
	subject := "🔧 New Job Request - BrodaMeko"

	body := fmt.Sprintf(`
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1.0" />
	<title>New Job Request - BrodaMeko</title>
	<link href="https://fonts.googleapis.com/css2?family=Poppins:wght@400;500;600;700&display=swap" rel="stylesheet">
	<style>
		:root { color-scheme: light dark; }
		body { font-family: 'Poppins', Arial, sans-serif; margin: 0; padding: 0; background-color: #f4f4f4; }
		@media (prefers-color-scheme: dark) {
			body          { background-color: #0f0f0f !important; }
			.wrapper      { background-color: #0f0f0f !important; }
			.card         { background-color: #1a1a2e !important; border-color: #2a2a4a !important; }
			.greeting     { color: #ffffff !important; }
			.body-text    { color: #ccccdd !important; }
			.job-card     { background-color: #E6C714 !important; }
			.action-box   { background-color: #12122a !important; border-color: #E6C714 !important; }
			.action-text  { color: #bbbbcc !important; }
			.action-text strong { color: #ffffff !important; }
			.urgency-bar  { background-color: #12122a !important; color: #E6C714 !important; border-color: #2a2a4a !important; }
			.footnote     { color: #888899 !important; }
			.footer       { background-color: #111122 !important; border-color: #2a2a4a !important; }
			.footer-text  { color: #777788 !important; }
		}
		.wrapper      { background-color: #f4f4f4; padding: 40px 16px; }
		.card         { max-width: 560px; margin: 0 auto; background-color: #ffffff; border-radius: 16px; overflow: hidden; border: 1px solid #e0e0e0; box-shadow: 0 4px 24px rgba(0,0,0,0.08); }
		.header       { background-color: #E6C714; padding: 28px 24px; text-align: center; }
		.header h1    { margin: 0; font-size: 22px; font-weight: 700; color: #0a0a1a; }
		.header p     { margin: 6px 0 0; font-size: 13px; color: #1a1a1a; font-weight: 500; }
		.body         { padding: 32px 36px; }
		.greeting     { font-size: 17px; font-weight: 600; color: #111122; margin: 0 0 12px; }
		.body-text    { font-size: 15px; line-height: 1.7; color: #444455; margin: 0 0 24px; }
		.job-card     { background-color: #E6C714; border-radius: 12px; padding: 20px 24px; margin-bottom: 24px; }
		.meta-label   { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 1px; color: #333300; margin: 0 0 2px; }
		.meta-value   { font-size: 15px; font-weight: 600; color: #0a0a1a; margin: 0 0 14px; }
		.meta-value:last-child { margin-bottom: 0; }
		.meta-divider { border: none; border-top: 1px solid rgba(0,0,0,0.12); margin: 12px 0; }
		.action-box   { background-color: #fffef0; border-left: 4px solid #E6C714; border-radius: 8px; padding: 14px 18px; margin-bottom: 20px; }
		.action-text  { font-size: 14px; color: #555566; margin: 0; line-height: 1.6; }
		.action-text strong { color: #111122; }
		.urgency-bar  { background-color: #fffbea; border: 1px solid #e8d800; border-radius: 8px; padding: 10px 16px; margin-bottom: 24px; text-align: center; font-size: 13px; color: #7a6a00; font-weight: 600; }
		.footnote     { font-size: 12px; color: #9999aa; line-height: 1.6; margin: 0; }
		.footer       { background-color: #f9f9f9; border-top: 1px solid #eeeeee; text-align: center; padding: 20px 24px; }
		.footer-text  { font-size: 12px; color: #999999; margin: 0; }
		.brand        { color: #b89a00; font-weight: 600; }
	</style>
</head>
<body>
	<div class="wrapper">
		<div class="card">
			<div class="header">
				<h1>🔧 New Job Request</h1>
				<p>A car owner needs your help</p>
			</div>
			<div class="body">
				<p class="greeting">Hello %s,</p>
				<p class="body-text">You have a new job request on BrodaMeko. Open the app to accept or decline.</p>
				<div class="job-card">
					<p class="meta-label">Car Owner</p>
					<p class="meta-value">%s</p>
					<hr class="meta-divider" />
					<p class="meta-label">Issue</p>
					<p class="meta-value">%s</p>
					<hr class="meta-divider" />
					<p class="meta-label">Car</p>
					<p class="meta-value">%s</p>
				</div>
				<div class="action-box">
					<p class="action-text">Open the <strong>BrodaMeko app</strong> to view the full job details, accept the request and start chatting with the car owner, or decline if you're unavailable.</p>
				</div>
				<div class="urgency-bar">⏰ Job requests are time-sensitive — respond quickly to avoid missing out.</div>
				<p class="footnote">If you did not expect this request or believe it was sent in error, you can safely decline it in the app.</p>
			</div>
			<div class="footer">
				<p class="footer-text">&copy; %d <span class="brand">BrodaMeko</span> &mdash; Your Car's Best Friend, Anywhere.<br><small>Powered by iNuud Analytics</small></p>
			</div>
		</div>
	</div>
</body>
</html>
	`, mechanicName, ownerName, issueLabel, carMake, time.Now().Year())

	return SendEmail(to, subject, body)
}
