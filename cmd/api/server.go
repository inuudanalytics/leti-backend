package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"leti_server/internal/api/handlers"
	adminHandlers "leti_server/internal/api/handlers/admins"
	"leti_server/internal/api/handlers/calls"
	chatHandler "leti_server/internal/api/handlers/chat"
	paymentwebhook "leti_server/internal/api/handlers/payment_webhook"
	shortletchathandler "leti_server/internal/api/handlers/shortlet_chat"
	supportHandler "leti_server/internal/api/handlers/support"
	mw "leti_server/internal/api/middlewares"
	"leti_server/internal/api/routers"
	chathub "leti_server/internal/chathub"
	cronjobs "leti_server/internal/cron_jobs"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/internal/shortlethub"
	supporthub "leti_server/internal/supporthub"
	"leti_server/pkg/cache"
	"leti_server/pkg/config"
	"leti_server/pkg/utils"

	"github.com/joho/godotenv"
)

// @title           Leti API
// @version         1.0
// @description     Leti Server REST API
// @host            leti-backend.onrender.com
// @BasePath        /api/v1
// @schemes         https
//
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	if err := godotenv.Load(); err != nil {
		utils.Logger.Warn("No .env file found, using environment defaults")
	}

	utils.InitLogger()

	if err := sqlconnect.ConnectDb(); err != nil {
		utils.Logger.Fatal("DB connection failed: ", err)
	}
	db := sqlconnect.DB

	if err := cache.InitRedis(); err != nil {
		utils.Logger.Fatal("Redis connection failed: ", err)
	}

	// Seed bloom filter
	go func() {
		ctx := context.Background()
		usernames, err := sqlconnect.GetAllUsernames(ctx)
		if err != nil {
			utils.Logger.Warnf("failed to fetch usernames for bloom seed: %v", err)
			return
		}
		cache.SeedUsernameBloom(ctx, usernames)
	}()

	config.InitFirebase()

	// ================= CHAT SETUP =================
	chatHub := chathub.New()
	go chatHub.Run()
	chatHandler.Hub = chatHub
	chathub.PushNotifier = handlers.SendPushToUser

	// ================= SHORTLET CHAT HUB =================
	shortletHub := shortlethub.New()
	go shortletHub.Run()
	shortletchathandler.Hub = shortletHub
	shortlethub.PushNotifier = handlers.SendPushToUser

	// ── Support hub (admin ↔ user live chat) ─────────────────────────────────
	supHub := supporthub.New()
	go supHub.Run()
	supportHandler.Hub = supHub
	adminHandlers.SupportHub = supHub
	supporthub.PushNotifier = handlers.SendPushToUser

	// ================= WEBHOOK WORKER =================
	webhookCtx, webhookCancel := context.WithCancel(context.Background())

	go calls.StartRingingTimeoutWorker(webhookCtx)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		paymentwebhook.StartWebhookRetryWorker(webhookCtx, db)
	}()

	// ================= CRON JOBS =================
	cleanupCron := cronjobs.StartCronJobs()
	defer cleanupCron.Stop()

	port := os.Getenv("SERVER_PORT")

	// ================= MIDDLEWARES =================
	rl := mw.NewRateLimiter(5000, time.Minute)

	hppOptions := mw.HPPOptions{
		CheckQuery:                  true,
		CheckBody:                   true,
		CheckBodyOnlyForContentType: "application/x-www-form-urlencoded",
		Whitelist: []string{
			"sortby", "limit", "page", "sortOrder",
			"id", "agent_id", "agent_code", "property_id",
			"city", "state", "code", "reference", "trxref", "scope", "authuser", "property_type", "status", "is_approved", "role", "search",
			"school_nearby", "min_price", "max_price", "min_views", "max_views",
			"min_rating", "max_rating", "min_rating_quantity", "max_rating_quantity",
			"title_search", "description_search", "address_search",
			"created_after", "created_before", "updated_after", "updated_before",
			"token", "category", "store_id", "part_id", "order_id", "from", "to",
		},
	}

	router := routers.MainRouter()

	jwtMiddleware := mw.MiddlewaresExcludePaths(
		mw.JWTMiddleware,
		"/swagger/",
		"/api/v1/auth/signup",
		"/api/v1/auth/refresh",
		"/api/v1/admin/auth/refresh",
		"/api/v1/auth/apple",
		"/api/v1/auth/google",
		"/api/v1/auth/login",
		"/api/v1/admin/auth/login",
		"/api/v1/auth/verify-otp",
		"/api/v1/auth/resend-otp",
		"/api/v1/auth/forgot-password",
		"/api/v1/agents/reset-password/reset",
		"/api/v1/webhooks/paystack",
		"/api/v1/wallets/verify/payment",
		"/api/v1/system/health",
	)

	secureMux := utils.ApplyMiddlewares(
		router,
		mw.Cors,
		mw.SecurityHeaders,
		mw.Compression,
		mw.Hpp(hppOptions),
		jwtMiddleware,
		mw.ResponseTimeMiddleware,
		rl.Middleware,
	)

	// ================= SERVER =================
	server := &http.Server{
		Addr:    port,
		Handler: secureMux,
	}

	go func() {
		utils.Logger.Infof("Server running on port %s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			utils.Logger.Fatalf("Server error: %v", err)
		}
	}()

	// ================= GRACEFUL SHUTDOWN =================
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	<-stop
	utils.Logger.Info("Shutting down server...")

	httpCtx, httpCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer httpCancel()

	if err := server.Shutdown(httpCtx); err != nil {
		utils.Logger.Fatalf("Server forced to shutdown: %v", err)
	}

	// Stop webhook worker
	utils.Logger.Info("Stopping webhook retry worker...")
	webhookCancel()
	wg.Wait()
	utils.Logger.Info("Webhook retry worker stopped")

	sqlconnect.CloseDb()
	utils.Logger.Info("Server exited gracefully")
}
