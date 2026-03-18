package utils

import (
	"fmt"
	"time"
)

func SendJobAcceptedEmail(to, ownerName, mechanicName, issueLabel, carMake string) error {
	subject := "✅ Mechanic Accepted Your Job - BrodaMeko"

	body := fmt.Sprintf(`
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1.0" />
	<title>Job Accepted - BrodaMeko</title>
	<link href="https://fonts.googleapis.com/css2?family=Poppins:wght@400;500;600;700&display=swap" rel="stylesheet">
	<style>
		:root {
			color-scheme: light dark;
		}
		body {
			font-family: 'Poppins', Arial, sans-serif;
			margin: 0;
			padding: 0;
			background-color: #f4f4f4;
		}
		@media (prefers-color-scheme: dark) {
			body { background-color: #0f0f0f !important; }
			.wrapper { background-color: #0f0f0f !important; }
			.card { background-color: #1a1a2e !important; border-color: #2a2a4a !important; }
			.body-text { color: #ccccdd !important; }
			.footer-text { color: #888899 !important; }
			.meta-label { color: #aaaacc !important; }
			.meta-value { color: #ffffff !important; }
			.action-box { background-color: #12122a !important; border-color: #E6C714 !important; }
			.action-text { color: #bbbbcc !important; }
			.success-box { background-color: #0a1f0a !important; border-color: #44cc44 !important; }
			.success-text { color: #aaffaa !important; }
		}

		.wrapper {
			background-color: #f4f4f4;
			padding: 40px 16px;
		}
		.card {
			max-width: 560px;
			margin: 0 auto;
			background-color: #ffffff;
			border-radius: 16px;
			overflow: hidden;
			border: 1px solid #e0e0e0;
			box-shadow: 0 4px 24px rgba(0,0,0,0.08);
		}
		.header {
			background-color: #E6C714;
			padding: 28px 24px;
			text-align: center;
		}
		.header h1 {
			margin: 0;
			font-size: 22px;
			font-weight: 700;
			color: #0a0a1a;
			letter-spacing: 0.3px;
		}
		.header p {
			margin: 6px 0 0;
			font-size: 13px;
			color: #1a1a1a;
			font-weight: 500;
		}
		.body {
			padding: 32px 36px;
		}
		.greeting {
			font-size: 17px;
			font-weight: 600;
			color: #111122;
			margin: 0 0 12px;
		}
		.body-text {
			font-size: 15px;
			line-height: 1.7;
			color: #444455;
			margin: 0 0 24px;
		}
		.job-card {
			background-color: #E6C714;
			border-radius: 12px;
			padding: 20px 24px;
			margin-bottom: 24px;
		}
		.meta-label {
			font-size: 11px;
			font-weight: 700;
			text-transform: uppercase;
			letter-spacing: 1px;
			color: #333300;
			margin: 0 0 2px;
		}
		.meta-value {
			font-size: 15px;
			font-weight: 600;
			color: #0a0a1a;
			margin: 0 0 14px;
		}
		.meta-value:last-child {
			margin-bottom: 0;
		}
		.meta-divider {
			border: none;
			border-top: 1px solid rgba(0,0,0,0.12);
			margin: 12px 0;
		}
		.success-box {
			background-color: #f0fff0;
			border-left: 4px solid #44cc44;
			border-radius: 8px;
			padding: 14px 18px;
			margin-bottom: 20px;
		}
		.success-text {
			font-size: 14px;
			color: #1a4d1a;
			margin: 0;
			line-height: 1.6;
		}
		.action-box {
			background-color: #fafafa;
			border-left: 4px solid #E6C714;
			border-radius: 8px;
			padding: 14px 18px;
			margin-bottom: 24px;
		}
		.action-text {
			font-size: 14px;
			color: #555566;
			margin: 0;
			line-height: 1.6;
		}
		.action-text strong {
			color: #111122;
		}
		.footnote {
			font-size: 12px;
			color: #9999aa;
			line-height: 1.6;
			margin: 0;
		}
		.footer {
			background-color: #f9f9f9;
			border-top: 1px solid #eeeeee;
			text-align: center;
			padding: 20px 24px;
		}
		.footer-text {
			font-size: 12px;
			color: #999999;
			margin: 0;
		}
		.brand {
			color: #b89a00;
			font-weight: 600;
		}
	</style>
</head>
<body>
	<div class="wrapper">
		<div class="card">

			<div class="header">
				<h1>✅ Mechanic Accepted!</h1>
				<p>Your job request has been picked up</p>
			</div>

			<div class="body">
				<p class="greeting">Hello %s,</p>

				<p class="body-text">
					Great news — a mechanic has accepted your job request and is ready to help you out.
				</p>

				<div class="job-card">
					<p class="meta-label">Mechanic</p>
					<p class="meta-value">%s</p>
					<hr class="meta-divider" />
					<p class="meta-label">Issue</p>
					<p class="meta-value">%s</p>
					<hr class="meta-divider" />
					<p class="meta-label">Car</p>
					<p class="meta-value">%s</p>
				</div>

				<div class="success-box">
					<p class="success-text">🎉 Your conversation is now open — chat with your mechanic, discuss pricing, and get your car sorted.</p>
				</div>

				<div class="action-box">
					<p class="action-text">
						Open the <strong>BrodaMeko app</strong> to start chatting with your mechanic,
						review their quotation, and confirm the job details.
					</p>
				</div>

				<p class="footnote">
					If you no longer need this service, you can cancel the job from within the app at any time before work begins.
				</p>
			</div>

			<div class="footer">
				<p class="footer-text">
					&copy; %d <span class="brand">BrodaMeko</span> &mdash; Your Car's Best Friend, Anywhere.<br>
					<small>Powered by iNuud Analytics</small>
				</p>
			</div>

		</div>
	</div>
</body>
</html>
	`, ownerName, mechanicName, issueLabel, carMake, time.Now().Year())

	return SendEmail(to, subject, body)
}
