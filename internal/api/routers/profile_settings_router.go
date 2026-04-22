package routers

import (
	profilesettings "leti_server/internal/api/handlers/profile_settings"
	"net/http"
)

func profileSettingsRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// ── Shared ────────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /profile/bank/list", profilesettings.GetBankList)
	mux.HandleFunc("POST /profile/bank/verify", profilesettings.VerifyBankDetails)

	mux.HandleFunc("PATCH /profile/artisan/toggle/active-status", profilesettings.ToggleArtisanOnlineStatus)

	// ── Artisan — Bank ────────────────────────────────────────────────────────
	mux.HandleFunc("POST /profile/artisan/bank", profilesettings.SaveArtisanBankDetails)
	mux.HandleFunc("GET /profile/artisan/bank", profilesettings.GetArtisanBankDetails)
	mux.HandleFunc("PATCH /profile/artisan/bank/{id}/primary", profilesettings.SetArtisanPrimaryBankAccount)
	mux.HandleFunc("DELETE /profile/artisan/bank/{id}", profilesettings.DeleteArtisanBankDetails)

	// ── Artisan — Address ─────────────────────────────────────────────────────
	mux.HandleFunc("POST /profile/artisan/address", profilesettings.AddArtisanAddress)
	mux.HandleFunc("GET /profile/artisan/address", profilesettings.GetArtisanAddresses)
	mux.HandleFunc("PATCH /profile/artisan/address/{id}", profilesettings.UpdateArtisanAddress)
	mux.HandleFunc("DELETE /profile/artisan/address/{id}", profilesettings.DeleteArtisanAddress)
	mux.HandleFunc("PATCH /profile/artisan/address/{id}/primary", profilesettings.SetArtisanPrimaryAddress)

	// ── Client — Bank ─────────────────────────────────────────────────────────
	mux.HandleFunc("POST /profile/client/bank", profilesettings.SaveClientBankDetails)
	mux.HandleFunc("GET /profile/client/bank", profilesettings.GetClientBankDetails)
	mux.HandleFunc("PATCH /profile/client/bank/{id}/primary", profilesettings.SetClientPrimaryBankAccount)
	mux.HandleFunc("DELETE /profile/client/bank/{id}", profilesettings.DeleteClientBankDetails)

	// ── Client — Address ──────────────────────────────────────────────────────
	mux.HandleFunc("POST /profile/client/address", profilesettings.AddClientAddress)
	mux.HandleFunc("GET /profile/client/address", profilesettings.GetClientAddresses)
	mux.HandleFunc("PATCH /profile/client/address/{id}", profilesettings.UpdateClientAddress)
	mux.HandleFunc("DELETE /profile/client/address/{id}", profilesettings.DeleteClientAddress)
	mux.HandleFunc("PATCH /profile/client/address/{id}/primary", profilesettings.SetClientPrimaryAddress)

	// ── Owner — Bank ──────────────────────────────────────────────────────────
	mux.HandleFunc("POST /profile/owner/bank", profilesettings.SaveOwnerBankDetails)
	mux.HandleFunc("GET /profile/owner/bank", profilesettings.GetOwnerBankDetails)
	mux.HandleFunc("PATCH /profile/owner/bank/{id}/primary", profilesettings.SetOwnerPrimaryBankAccount)
	mux.HandleFunc("DELETE /profile/owner/bank/{id}", profilesettings.DeleteOwnerBankDetails)

	// ── Owner — Address ───────────────────────────────────────────────────────
	mux.HandleFunc("POST /profile/owner/address", profilesettings.AddOwnerAddress)
	mux.HandleFunc("GET /profile/owner/address", profilesettings.GetOwnerAddresses)
	mux.HandleFunc("PATCH /profile/owner/address/{id}", profilesettings.UpdateOwnerAddress)
	mux.HandleFunc("DELETE /profile/owner/address/{id}", profilesettings.DeleteOwnerAddress)
	mux.HandleFunc("PATCH /profile/owner/address/{id}/primary", profilesettings.SetOwnerPrimaryAddress)

	return mux
}
