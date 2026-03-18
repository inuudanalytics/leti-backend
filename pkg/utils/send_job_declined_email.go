package utils

import (
	"fmt"
	"time"
)

func SendJobDeclinedEmail(to, ownerName, mechanicName, issueLabel, carMake string) error {
	subject := "❌ Job Request Declined - BrodaMeko"

	body := fmt.Sprintf(`
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1.0" />
	<title>Job Request Declined - BrodaMeko</title>
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
			body        { background-color: #0f0f0f !important; }
			.wrapper    { background-color: #0f0f0f !important; }
			.card       { background-color: #1a1a2e !important; border-color: #2a2a4a !important; }
			.body-text  { color: #ccccdd !important; }
			.greeting   { color: #ffffff !important; }
			.job-card   { background-color: #12122a !important; border-color: #2a2a5a !important; }
			.meta-label { color: #aaaacc !important; }
			.meta-value { color: #ffffff !important; }
			.meta-divider { border-color: #2a2a5a !important; }
			.action-box { background-color: #12122a !important; border-color: #E6C714 !important; }
			.action-text { color: #bbbbcc !important; }
			.action-text strong { color: #ffffff !important; }
			.info-badge { background-color: #0f0f2a !important; color: #aaaacc !important; border-color: #2a2a5a !important; }
			.footnote   { color: #888899 !important; }
			.footer     { background-color: #111122 !important; border-color: #2a2a4a !important; }
			.footer-text { color: #777788 !important; }
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
			background-color: #ff4444;
			padding: 28px 24px;
			text-align: center;
		}
		.header h1 {
			margin: 0;
			font-size: 22px;
			font-weight: 700;
			color: #ffffff;
			letter-spacing: 0.3px;
		}
		.header p {
			margin: 6px 0 0;
			font-size: 13px;
			color: #ffe0e0;
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
			background-color: #fafafa;
			border: 1px solid #e8e8e8;
			border-radius: 12px;
			padding: 20px 24px;
			margin-bottom: 24px;
		}
		.meta-label {
			font-size: 11px;
			font-weight: 700;
			text-transform: uppercase;
			letter-spacing: 1px;
			color: #888899;
			margin: 0 0 2px;
		}
		.meta-value {
			font-size: 15px;
			font-weight: 600;
			color: #111122;
			margin: 0 0 14px;
		}
		.meta-value:last-child {
			margin-bottom: 0;
		}
		.meta-divider {
			border: none;
			border-top: 1px solid #eeeeee;
			margin: 12px 0;
		}
		.action-box {
			background-color: #fffef0;
			border-left: 4px solid #E6C714;
			border-radius: 8px;
			padding: 14px 18px;
			margin-bottom: 20px;
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
		.info-badge {
			background-color: #f5f5ff;
			border: 1px solid #e0e0f0;
			border-radius: 8px;
			padding: 12px 16px;
			text-align: center;
			font-size: 13px;
			color: #555566;
			margin-bottom: 24px;
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
				<h1>❌ Job Request Declined</h1>
				<p>Don't worry — other mechanics are available</p>
			</div>

			<div class="body">
				<p class="greeting">Hello %s,</p>

				<p class="body-text">
					Unfortunately, <strong>%s</strong> is currently unavailable and has declined your job request.
					Don't worry — there are plenty of other mechanics ready to help you.
				</p>

				<div class="job-card">
					<p class="meta-label">Issue</p>
					<p class="meta-value">%s</p>
					<hr class="meta-divider" />
					<p class="meta-label">Car</p>
					<p class="meta-value">%s</p>
				</div>

				<div class="action-box">
					<p class="action-text">
						Open the <strong>BrodaMeko app</strong> to browse other available mechanics and send a new hire request.
					</p>
				</div>

				<div class="info-badge">
					💡 Tip: Check mechanic ratings and reviews before sending your next request.
				</div>

				<p class="footnote">
					Your job is still active and available. You can hire a different mechanic at any time from the app.
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
