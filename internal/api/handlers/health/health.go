package health

import (
	"context"
	"net/http"
	"time"

	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/cache"
	"leti_server/pkg/utils"
)

type status string

const (
	statusOK   status = "ok"
	statusFail status = "fail"
)

type componentHealth struct {
	Status  status `json:"status"`
	Latency string `json:"latency,omitempty"`
	Error   string `json:"error,omitempty"`
}

type healthResponse struct {
	Status     status                     `json:"status"`
	Uptime     string                     `json:"uptime"`
	Components map[string]componentHealth `json:"components"`
}

var startTime = time.Now()

// HealthCheck godoc
// @Summary      System health check
// @Description  Returns the health status of the server and its dependencies (database and Redis). Returns 200 if all components are healthy, 503 if any are degraded.
// @Tags         System
// @Produce      json
// @Success      200  {object}  object{status=string,uptime=string,components=object{database=object{status=string,latency=string},redis=object{status=string,latency=string}}}
// @Failure      503  {object}  object{status=string,uptime=string,components=object{database=object{status=string,error=string},redis=object{status=string,error=string}}}
// @Router       /system/health [get]
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	overall := statusOK
	components := make(map[string]componentHealth)

	components["database"] = pingDatabase(r.Context())
	if components["database"].Status == statusFail {
		overall = statusFail
	}

	components["redis"] = pingRedis(r.Context())
	if components["redis"].Status == statusFail {
		overall = statusFail
	}

	statusCode := http.StatusOK
	if overall == statusFail {
		statusCode = http.StatusServiceUnavailable
	}

	utils.WriteJSONWithStatus(w, statusCode, healthResponse{
		Status:     overall,
		Uptime:     time.Since(startTime).Round(time.Second).String(),
		Components: components,
	})
}

func pingDatabase(ctx context.Context) componentHealth {
	if sqlconnect.DB == nil {
		return componentHealth{Status: statusFail, Error: "pool is nil"}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	start := time.Now()
	if err := sqlconnect.DB.Ping(ctx); err != nil {
		return componentHealth{Status: statusFail, Error: err.Error()}
	}
	return componentHealth{Status: statusOK, Latency: time.Since(start).Round(time.Millisecond).String()}
}

func pingRedis(ctx context.Context) componentHealth {
	if cache.RDB == nil {
		return componentHealth{Status: statusFail, Error: "client is nil"}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	start := time.Now()
	if err := cache.RDB.Ping(ctx).Err(); err != nil {
		return componentHealth{Status: statusFail, Error: err.Error()}
	}
	return componentHealth{Status: statusOK, Latency: time.Since(start).Round(time.Millisecond).String()}
}
