package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/smugii23/chirpy/internal/auth"
	"github.com/smugii23/chirpy/internal/database"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	DB             *database.Queries
	Platform       string
	Secret         string
	PolkaKey       string
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) metricsHandler(w http.ResponseWriter, r *http.Request) {
	reqBody := `
<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
	<p>Chirpy has been visited ` + strconv.Itoa(int(cfg.fileserverHits.Load())) + ` times!</p>
  </body>
</html>
`
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(reqBody))
}

func (cfg *apiConfig) resetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		// This is the POST request for resetting users
		// Your code to check platform and delete users
		if cfg.Platform != "dev" {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		err := cfg.DB.DeleteUsers(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Return success response
		w.WriteHeader(http.StatusOK)
		return
	}
	cfg.fileserverHits.Store(0)
}

func (cfg *apiConfig) valid_chirp(w http.ResponseWriter, r *http.Request) {
	type chirp struct {
		Body string `json:"body"`
	}

	type validResp struct {
		Valid bool `json:"valid"`
	}

	type errorResp struct {
		Error string `json:"error"`
	}

	decoder := json.NewDecoder(r.Body)
	tweet := chirp{}
	err := decoder.Decode(&tweet)
	if err != nil {
		resp := errorResp{
			Error: "Something went wrong",
		}
		res, err := json.Marshal(resp)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write(res)
		return
	}
	if len(tweet.Body) > 140 {
		resp := errorResp{
			Error: "Chirp is too long",
		}
		res, err := json.Marshal(resp)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write(res)
		return
	}
	resp := validResp{
		Valid: true,
	}
	res, err := json.Marshal(resp)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(res)
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(payload)
}

func cleanProfanity(text string) string {
	split := strings.Split(text, " ")
	for i, word := range split {
		if strings.ToLower(word) == "kerfuffle" || strings.ToLower(word) == "sharbert" || strings.ToLower(word) == "fornax" {
			split[i] = "****"
		}
	}
	res := strings.Join(split, " ")
	return res
}

func validateChirpHandler(w http.ResponseWriter, r *http.Request) {
	var requestData struct {
		Body string `json:"body"`
	}

	err := json.NewDecoder(r.Body).Decode(&requestData)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if len(requestData.Body) > 140 {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	cleanedBody := cleanProfanity(requestData.Body)

	respondWithJSON(w, http.StatusOK, map[string]string{
		"cleaned_body": cleanedBody,
	})
}

func (cfg *apiConfig) addUser(w http.ResponseWriter, r *http.Request) {
	var reqData struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&reqData)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	hashedPass, err := auth.HashPassword(reqData.Password)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	params := database.CreateUserParams{
		Email:          reqData.Email,
		HashedPassword: hashedPass,
	}
	user, err := cfg.DB.CreateUser(r.Context(), params)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	userData := struct {
		ID          uuid.UUID `json:"id"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
		Email       string    `json:"email"`
		IsChirpyRed bool      `json:"is_chirpy_red"`
	}{
		ID:          user.ID,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
		Email:       user.Email,
		IsChirpyRed: user.IsChirpyRed.Valid && user.IsChirpyRed.Bool,
	}
	jsonData, err := json.Marshal(userData)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(jsonData))
}

func (cfg *apiConfig) chirpHandler(w http.ResponseWriter, r *http.Request) {
	type chirp struct {
		Body   string `json:"body"`
		UserID string `json:"user_id"`
	}

	type validResp struct {
		Valid bool `json:"valid"`
	}

	type errorResp struct {
		Error string `json:"error"`
	}
	tokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Authentication required"})
		return
	}
	userID, err := auth.ValidateJWT(tokenString, cfg.Secret)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Authentication required"})
		return
	}

	decoder := json.NewDecoder(r.Body)
	tweet := chirp{}
	err = decoder.Decode(&tweet)
	if err != nil {
		resp := errorResp{
			Error: "Something went wrong",
		}
		res, err := json.Marshal(resp)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write(res)
		return
	}
	if len(tweet.Body) > 140 {
		resp := errorResp{
			Error: "Chirp is too long",
		}
		res, err := json.Marshal(resp)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write(res)
		return
	}
	params := database.AddChirpsParams{
		Body:   tweet.Body,
		UserID: userID,
	}
	chirpy, err := cfg.DB.AddChirps(r.Context(), params)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	type chirpResponse struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Body      string    `json:"body"`
		UserID    string    `json:"user_id"`
	}
	response := chirpResponse{
		ID:        chirpy.ID.String(),
		CreatedAt: chirpy.CreatedAt,
		UpdatedAt: chirpy.UpdatedAt,
		Body:      chirpy.Body,
		UserID:    chirpy.UserID.String(),
	}
	res, err := json.Marshal(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write(res)
}

func (cfg *apiConfig) getAllChirpsHandler(w http.ResponseWriter, r *http.Request) {
	type chirpResponse struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Body      string    `json:"body"`
		UserID    string    `json:"user_id"`
	}
	sortOrder := r.URL.Query().Get("sort")
	authorID := r.URL.Query().Get("author_id")
	var chirps []database.Chirp
	var err error

	if authorID != "" {
		uuidAuthorID, err := uuid.Parse(authorID)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": "Invalid author ID format"}`))
			return
		}
		chirps, err = cfg.DB.GetAllChirps(r.Context(), uuidAuthorID)
	} else {
		chirps, err = cfg.DB.GetAllChirpsWithoutFilter(r.Context())
	}

	if err != nil {
		fmt.Println("Error fetching chirps:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	sort.Slice(chirps, func(i, j int) bool {
		if sortOrder != "desc" {
			return chirps[i].CreatedAt.Before(chirps[j].CreatedAt)
		} else {
			return chirps[i].CreatedAt.After(chirps[j].CreatedAt)
		}
	})
	var res []chirpResponse
	for _, chirp := range chirps {
		response := chirpResponse{
			ID:        chirp.ID.String(),
			CreatedAt: chirp.CreatedAt,
			UpdatedAt: chirp.UpdatedAt,
			Body:      chirp.Body,
			UserID:    chirp.UserID.String(),
		}
		res = append(res, response)
	}
	jsonData, err := json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(jsonData)

}

func (cfg *apiConfig) getChirpHandler(w http.ResponseWriter, r *http.Request) {
	type chirpResponse struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Body      string    `json:"body"`
		UserID    string    `json:"user_id"`
	}
	id := r.PathValue("chirpID")
	uuidID, err := uuid.Parse(id)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	chirp, err := cfg.DB.GetChirp(r.Context(), uuidID)
	if err == sql.ErrNoRows {
		w.WriteHeader(http.StatusNotFound)
		return
	} else if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	response := chirpResponse{
		ID:        chirp.ID.String(),
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID.String(),
	}
	jsonData, err := json.Marshal(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(jsonData)
}

func (cfg *apiConfig) authenticateUser(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	req := request{}
	err := decoder.Decode(&req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	user, err := cfg.DB.LookupUser(r.Context(), req.Email)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Incorrect email or password"}`))
		return
	}
	err = auth.CheckPasswordHash(req.Password, user.HashedPassword)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Incorrect email or password"}`))
		return
	}
	expiresIn := 3600
	token, err := auth.MakeJWT(user.ID, cfg.Secret, time.Duration(expiresIn)*time.Second)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	refresh_token, err := auth.MakeRefreshToken()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	refresh_params := database.AddRefreshTokenParams{
		Token:     refresh_token,
		CreatedAt: sql.NullTime{Time: time.Now(), Valid: true},
		UpdatedAt: sql.NullTime{Time: time.Now(), Valid: true},
		UserID:    uuid.NullUUID{UUID: user.ID, Valid: true},
		ExpiresAt: sql.NullTime{Time: time.Now().Add(60 * 24 * time.Hour), Valid: true},
		RevokedAt: sql.NullTime{Valid: false},
	}
	err = cfg.DB.AddRefreshToken(r.Context(), refresh_params)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "Failed to add refresh token"}`))
		return
	}
	type users struct {
		ID           uuid.UUID `json:"id"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
		Email        string    `json:"email"`
		Token        string    `json:"token"`
		RefreshToken string    `json:"refresh_token"`
		IsChirpyRed  bool      `json:"is_chirpy_red"`
	}
	lookup := users{
		ID:           user.ID,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
		Email:        user.Email,
		Token:        token,
		RefreshToken: refresh_token,
		IsChirpyRed:  user.IsChirpyRed.Valid && user.IsChirpyRed.Bool,
	}
	jsonData, err := json.Marshal(lookup)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(jsonData)
}

func extractToken(authHeader string) (string, error) {
	const bearerPrefix = "Bearer "
	if len(authHeader) < len(bearerPrefix) || authHeader[:len(bearerPrefix)] != bearerPrefix {
		return "", fmt.Errorf("invalid authorization header")
	}
	return authHeader[len(bearerPrefix):], nil
}

func (cfg *apiConfig) refreshToken(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Authorization header missing"}`))
		return
	}

	token, err := extractToken(authHeader)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Invalid authorization header"}`))
		return
	}

	userID, err := cfg.DB.GetRefreshToken(r.Context(), token)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Failed to retrieve refresh token"}`))
		return
	}
	if !userID.Valid {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	newToken, err := auth.MakeJWT(userID.UUID, cfg.Secret, time.Hour)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	response := map[string]string{
		"token": newToken,
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "Failed to generate response"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(responseJSON)
}

func (cfg *apiConfig) revokeRefreshToken(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Authorization header missing"}`))
		return
	}

	token, err := extractToken(authHeader)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Invalid authorization header"}`))
		return
	}

	err = cfg.DB.RevokeRefreshToken(r.Context(), token)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "Failed to revoke refresh token"}`))
		return
	}
	w.WriteHeader(http.StatusNoContent)

}

func (cfg *apiConfig) updateUser(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Authorization header missing"}`))
		return
	}
	type updateUserRequest struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Invalid request body"}`))
		return
	}
	token, err := extractToken(authHeader)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Invalid authorization header"}`))
		return
	}
	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.Secret)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	params := database.UpdateUserPasswordParams{Email: req.Email, HashedPassword: hashedPassword, ID: userID}
	user, err := cfg.DB.UpdateUserPassword(r.Context(), params)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	type userResponse struct {
		ID          uuid.UUID `json:"id"`
		Email       string    `json:"email"`
		CreatedAt   time.Time `json:"created_at"`
		IsChirpyRed bool      `json:"is_chirpy_red"`
	}

	resp := userResponse{
		ID:          user.ID,
		Email:       user.Email,
		CreatedAt:   user.CreatedAt,
		IsChirpyRed: user.IsChirpyRed.Valid && user.IsChirpyRed.Bool,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		fmt.Printf("Error encoding response: %v", err)
	}
}

func (cfg *apiConfig) deleteChirp(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Authorization header missing"}`))
		return
	}
	token, err := extractToken(authHeader)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Invalid authorization header"}`))
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.Secret)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	chirpID := r.PathValue("chirpID")
	uuidChirpID, err := uuid.Parse(chirpID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Invalid chirp ID format"}`))
		return
	}
	chirp, err := cfg.DB.GetChirp(r.Context(), uuidChirpID)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if chirp.ID == uuid.Nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if chirp.UserID != userID {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	err = cfg.DB.DeleteChirp(r.Context(), uuidChirpID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (cfg *apiConfig) upgradeUser(w http.ResponseWriter, r *http.Request) {
	type requestData struct {
		UserID string `json:"user_id"`
	}

	type request struct {
		Event string      `json:"event"`
		Data  requestData `json:"data"`
	}
	var user request
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if user.Event != "user.upgraded" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	userUUID, err := uuid.Parse(user.Data.UserID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Invalid user ID format"}`))
		return
	}
	apiKey, err := auth.GetAPIKey(r.Header)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if apiKey != cfg.PolkaKey {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	_, err = cfg.DB.MakeUserRed(r.Context(), userUUID)
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func main() {
	godotenv.Load()
	secret := os.Getenv("SECRET")
	platform := os.Getenv("PLATFORM")
	dbURL := os.Getenv("DB_URL")
	polkaKey := os.Getenv("POLKA_KEY")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
	}
	dbQueries := database.New(db)
	apiCfg := &apiConfig{DB: dbQueries,
		Platform: platform,
		Secret:   secret,
		PolkaKey: polkaKey}
	apiCfg.fileserverHits.Store(0)
	mux := http.NewServeMux()
	server := &http.Server{
		Handler: mux,
		Addr:    "localhost:8080",
	}
	fileServer := http.FileServer(http.Dir("."))
	fileServerWithPrefix := http.StripPrefix("/app", fileServer)
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(fileServerWithPrefix))
	mux.HandleFunc("/admin/metrics", apiCfg.metricsHandler)
	mux.HandleFunc("/admin/reset", apiCfg.resetHandler)
	mux.HandleFunc("/api/healthz", healthHandler)
	mux.HandleFunc("/api/validate_chirp", validateChirpHandler)
	mux.HandleFunc("/api/users", apiCfg.addUser)
	mux.HandleFunc("POST /api/chirps", apiCfg.chirpHandler)
	mux.HandleFunc("GET /api/chirps", apiCfg.getAllChirpsHandler)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.getChirpHandler)
	mux.HandleFunc("POST /api/login", apiCfg.authenticateUser)
	mux.HandleFunc("POST /api/refresh", apiCfg.refreshToken)
	mux.HandleFunc("POST /api/revoke", apiCfg.revokeRefreshToken)
	mux.HandleFunc("PUT /api/users", apiCfg.updateUser)
	mux.HandleFunc("DELETE /api/chirps/{chirpID}", apiCfg.deleteChirp)
	mux.HandleFunc("POST /api/polka/webhooks", apiCfg.upgradeUser)
	server.ListenAndServe()
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
