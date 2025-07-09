package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"io"
	"log"
	"net/http"
	"os"
	"pjh.id.au/chirpy/v2/internal/auth"
	"pjh.id.au/chirpy/v2/internal/database"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)
import _ "github.com/lib/pq"

// API
type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
	authSecret     string
	polkaKey       string
}

func sendJsonResponse(writer http.ResponseWriter, response interface{}, status int) {
	writer.Header().Add("Content-Type", "application/json")
	jsonData, err := json.Marshal(response)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		writer.WriteHeader(500)
		return
	}
	writer.WriteHeader(status)
	writer.Write(jsonData)
}

func sendJsonSuccessResponse(writer http.ResponseWriter, response interface{}) {
	sendJsonResponse(writer, response, http.StatusOK)
}

func sendJsonCreatedResponse(writer http.ResponseWriter, response interface{}) {
	sendJsonResponse(writer, response, http.StatusCreated)
}

func sendJsonError(writer http.ResponseWriter, error string, status int) {
	type errorResponse struct {
		Error string `json:"error"`
	}
	errorData := errorResponse{Error: error}
	sendJsonResponse(writer, errorData, status)
}

func sendJsonBadRequestError(writer http.ResponseWriter, error string) {
	sendJsonError(writer, error, http.StatusBadRequest)
}

func sendJsonUnauthorizedError(writer http.ResponseWriter, error string) {
	sendJsonError(writer, error, http.StatusUnauthorized)
}

func sendJsonForbiddenError(writer http.ResponseWriter, error string) {
	sendJsonError(writer, error, http.StatusForbidden)
}

func sendJsonNotFoundError(writer http.ResponseWriter, error string) {
	sendJsonError(writer, error, http.StatusNotFound)
}

func sendJsonInternalServerError(writer http.ResponseWriter, error string) {
	sendJsonError(writer, error, http.StatusInternalServerError)
}

func decodePostBody(body io.Reader, params interface{}) error {
	decoder := json.NewDecoder(body)
	err := decoder.Decode(&params)
	return err
}

func replaceBannedWords(body string) string {
	bannedWords := []string{"kerfuffle", "sharbert", "fornax"}
	words := strings.Split(body, " ")
	for i, word := range words {
		if slices.Contains(bannedWords, strings.ToLower(word)) {
			words = slices.Replace(words, i, i+1, "****")
		}
	}
	return strings.Join(words, " ")
}

func healthHandler(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Add("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	writer.Write([]byte("OK"))
}

// Metrics
func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) metricsHandler(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Add("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	writer.Write([]byte(fmt.Sprintf("Hits: %d", cfg.fileserverHits.Load())))
}

func (cfg *apiConfig) metricsResetHandler(writer http.ResponseWriter, request *http.Request) {
	if os.Getenv("PLATFORM") != "dev" {
		writer.WriteHeader(http.StatusForbidden)
		return
	}

	cfg.fileserverHits.Store(0)
	err := cfg.db.DeleteAllUsers(request.Context())
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	writer.Header().Add("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	writer.Write([]byte("OK"))
}

func (cfg *apiConfig) metricsPageHandler(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Add("Content-Type", "text/html")
	writer.WriteHeader(http.StatusOK)
	writer.Write([]byte(fmt.Sprintf("<html>\n  <body>\n    <h1>Welcome, Chirpy Admin</h1>\n    <p>Chirpy has been visited %d times!</p>\n  </body>\n</html>", cfg.fileserverHits.Load())))
}

// Users
type User struct {
	ID          uuid.UUID `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Email       string    `json:"email"`
	IsChirpyRed bool      `json:"is_chirpy_red"`
}

func UserFromDb(dbUser database.User) *User {
	return &User{
		ID:          dbUser.ID,
		CreatedAt:   dbUser.CreatedAt,
		UpdatedAt:   dbUser.UpdatedAt,
		Email:       dbUser.Email,
		IsChirpyRed: dbUser.IsChirpyRed,
	}
}

func (cfg *apiConfig) createUserHandler(writer http.ResponseWriter, request *http.Request) {
	type createUserPostBody struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	params := createUserPostBody{}
	err := decodePostBody(request.Body, &params)
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	password, err := auth.HashPassword(params.Password)
	if err != nil {
		sendJsonInternalServerError(writer, err.Error())
		return
	}

	dbUser, err := cfg.db.CreateUser(request.Context(), database.CreateUserParams{Email: params.Email, HashedPassword: password})
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	user := UserFromDb(dbUser)

	sendJsonCreatedResponse(writer, user)
}

func (cfg *apiConfig) updateUserHandler(writer http.ResponseWriter, request *http.Request) {
	type updateUserPostBody struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	jwt, err := auth.GetBearerToken(request.Header)
	if err != nil {
		sendJsonUnauthorizedError(writer, "Unauthorized")
		return
	}

	userId, err := auth.ValidateJWT(jwt, cfg.authSecret)
	if err != nil {
		sendJsonUnauthorizedError(writer, "Unauthorized")
		return
	}

	params := updateUserPostBody{}
	err = decodePostBody(request.Body, &params)
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	password, err := auth.HashPassword(params.Password)
	if err != nil {
		sendJsonInternalServerError(writer, err.Error())
		return
	}

	dbUser, err := cfg.db.UpdateUser(request.Context(), database.UpdateUserParams{ID: userId, Email: params.Email, HashedPassword: password})
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	user := UserFromDb(dbUser)

	sendJsonSuccessResponse(writer, user)
}

func (cfg *apiConfig) loginHandler(writer http.ResponseWriter, request *http.Request) {
	type loginPostBody struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	type loginResponse struct {
		Id           uuid.UUID `json:"id"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
		Email        string    `json:"email"`
		IsChirpyRed  bool      `json:"is_chirpy_red"`
		Token        string    `json:"token"`
		RefreshToken string    `json:"refresh_token"`
	}

	params := loginPostBody{}
	err := decodePostBody(request.Body, &params)
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	dbUser, err := cfg.db.GetUserByEmail(request.Context(), params.Email)
	if err != nil {
		sendJsonUnauthorizedError(writer, "Incorrect email or password")
		return
	}

	err = auth.CheckPasswordHash(params.Password, dbUser.HashedPassword)
	if err != nil {
		sendJsonUnauthorizedError(writer, "Incorrect email or password")
		return
	}

	user := UserFromDb(dbUser)

	jwt, err := auth.MakeJWT(user.ID, cfg.authSecret, time.Duration(3600)*time.Second)
	if err != nil {
		sendJsonInternalServerError(writer, err.Error())
		return
	}

	refreshToken, err := auth.MakeRefreshToken()
	if err != nil {
		sendJsonInternalServerError(writer, err.Error())
		return
	}

	_, err = cfg.db.CreateRefreshToken(request.Context(), database.CreateRefreshTokenParams{Token: refreshToken, UserID: user.ID, Secs: 60 * 60 * 60 * 24})
	if err != nil {
		sendJsonInternalServerError(writer, err.Error())
		return
	}

	sendJsonSuccessResponse(writer, loginResponse{
		Id:           user.ID,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
		Email:        user.Email,
		IsChirpyRed:  user.IsChirpyRed,
		Token:        jwt,
		RefreshToken: refreshToken,
	})
}

func (cfg *apiConfig) refreshHandler(writer http.ResponseWriter, request *http.Request) {
	type refreshResponse struct {
		Token string `json:"token"`
	}

	refreshToken, err := auth.GetBearerToken(request.Header)
	if err != nil {
		sendJsonUnauthorizedError(writer, "Unauthorized")
		return
	}

	dbRefreshToken, err := cfg.db.GetRefreshToken(request.Context(), refreshToken)
	if err != nil {
		sendJsonUnauthorizedError(writer, "Unauthorized")
		return
	}

	if dbRefreshToken.RevokedAt.Valid || dbRefreshToken.ExpiresAt.Before(time.Now()) {
		sendJsonUnauthorizedError(writer, "Unauthorized")
		return
	}

	token, err := auth.MakeJWT(dbRefreshToken.UserID, cfg.authSecret, time.Duration(3600)*time.Second)
	if err != nil {
		sendJsonInternalServerError(writer, err.Error())
		return
	}

	sendJsonSuccessResponse(writer, refreshResponse{Token: token})
}

func (cfg *apiConfig) revokeHandler(writer http.ResponseWriter, request *http.Request) {
	refreshToken, err := auth.GetBearerToken(request.Header)
	if err != nil {
		sendJsonUnauthorizedError(writer, "Unauthorized")
		return
	}

	_, err = cfg.db.RevokeRefreshToken(request.Context(), refreshToken)
	if err != nil {
		sendJsonUnauthorizedError(writer, "Unauthorized")
		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

// Chirps
type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserId    uuid.UUID `json:"user_id"`
}

func ChirpFromDb(dbChirp database.Chirp) *Chirp {
	return &Chirp{
		ID:        dbChirp.ID,
		CreatedAt: dbChirp.CreatedAt,
		UpdatedAt: dbChirp.UpdatedAt,
		UserId:    dbChirp.UserID,
		Body:      dbChirp.Body,
	}
}

func validateChirp(body string) (string, error) {
	if len(body) > 140 {
		return "", errors.New("chirp is too long")
	}

	return replaceBannedWords(body), nil
}

func (cfg *apiConfig) getChirpsHandler(writer http.ResponseWriter, request *http.Request) {
	var err error
	var dbChirps []database.Chirp

	authorIdString := request.URL.Query().Get("author_id")
	if authorIdString != "" {
		authorId, err := uuid.Parse(authorIdString)
		if err != nil {
			sendJsonBadRequestError(writer, err.Error())
			return
		}
		dbChirps, err = cfg.db.GetChirpsByAuthor(request.Context(), authorId)
		if err != nil {
			sendJsonInternalServerError(writer, err.Error())
			return
		}

	} else {
		dbChirps, err = cfg.db.GetChirps(request.Context())
		if err != nil {
			sendJsonInternalServerError(writer, err.Error())
		}
	}

	chirps := make([]*Chirp, 0, len(dbChirps))
	for _, dbChirp := range dbChirps {
		chirps = append(chirps, ChirpFromDb(dbChirp))
	}

	sendJsonSuccessResponse(writer, chirps)
}

func (cfg *apiConfig) getChirpHandler(writer http.ResponseWriter, request *http.Request) {
	id, err := uuid.Parse(request.PathValue("chirpID"))
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	dbChirp, err := cfg.db.GetChirp(request.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		sendJsonNotFoundError(writer, "Chirp not found.")
		return
	}
	if err != nil {
		sendJsonInternalServerError(writer, err.Error())
		return
	}

	chirp := ChirpFromDb(dbChirp)

	sendJsonSuccessResponse(writer, chirp)
}

func (cfg *apiConfig) createChirpHandler(writer http.ResponseWriter, request *http.Request) {
	type createChirpPostBody struct {
		Body string `json:"body"`
	}

	jwt, err := auth.GetBearerToken(request.Header)
	if err != nil {
		sendJsonUnauthorizedError(writer, "Unauthorized")
		return
	}

	userId, err := auth.ValidateJWT(jwt, cfg.authSecret)
	if err != nil {
		sendJsonUnauthorizedError(writer, "Unauthorized")
		return
	}

	params := createChirpPostBody{}
	err = decodePostBody(request.Body, &params)
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	cleanBody, err := validateChirp(params.Body)
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	dbChirp, err := cfg.db.CreateChirp(request.Context(), database.CreateChirpParams{Body: cleanBody, UserID: userId})
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	chirp := ChirpFromDb(dbChirp)

	sendJsonCreatedResponse(writer, chirp)
}

func (cfg *apiConfig) deleteChirpHandler(writer http.ResponseWriter, request *http.Request) {
	jwt, err := auth.GetBearerToken(request.Header)
	if err != nil {
		sendJsonUnauthorizedError(writer, "Unauthorized")
		return
	}

	userId, err := auth.ValidateJWT(jwt, cfg.authSecret)
	if err != nil {
		sendJsonUnauthorizedError(writer, "Unauthorized")
		return
	}

	id, err := uuid.Parse(request.PathValue("chirpID"))
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	dbChirp, err := cfg.db.GetChirp(request.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		sendJsonNotFoundError(writer, "Chirp not found.")
		return
	}
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}
	if dbChirp.UserID != userId {
		sendJsonForbiddenError(writer, "Unauthorized")
		return
	}

	rows, err := cfg.db.DeleteChirpWithUser(request.Context(), database.DeleteChirpWithUserParams{ID: id, UserID: userId})
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}
	if rows == 0 {
		sendJsonBadRequestError(writer, "Delete failed. Chirp not found or not owned by user?")
		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

func (cfg *apiConfig) polkaWebHookHandler(writer http.ResponseWriter, request *http.Request) {
	type polkaWebHookPostBody struct {
		Event string `json:"event"`
		Data  struct {
			UserID string `json:"user_id"`
		} `json:"data"`
	}

	key, err := auth.GetAPIKey(request.Header)
	if err != nil || key != cfg.polkaKey {
		sendJsonUnauthorizedError(writer, "Unauthorized")
		return
	}

	params := polkaWebHookPostBody{}
	err = decodePostBody(request.Body, &params)
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	if params.Event == "user.upgraded" {
		userId, err := uuid.Parse(params.Data.UserID)
		if err != nil {
			sendJsonBadRequestError(writer, err.Error())
			return
		}
		rows, err := cfg.db.UpgradeUser(request.Context(), userId)
		if err != nil {
			sendJsonBadRequestError(writer, err.Error())
			return
		}
		if rows == 0 {
			sendJsonBadRequestError(writer, "Upgrade failed. User not found?")
		}

	}

	writer.WriteHeader(http.StatusNoContent)
}

func main() {
	var err error

	err = godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file: ", err)
	}

	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("Error opening database: ", err)
		return
	}
	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			log.Fatal("Error closing database: ", err)
		}
	}(db)
	dbQueries := database.New(db)

	mux := http.NewServeMux()
	server := http.Server{
		Handler: mux,
		Addr:    ":8080",
	}

	authSecret := os.Getenv("AUTH_SECRET")
	polkaKey := os.Getenv("POLKA_KEY")

	apiCfg := apiConfig{db: dbQueries, authSecret: authSecret, polkaKey: polkaKey}

	mux.HandleFunc("GET /api/healthz", healthHandler)
	mux.HandleFunc("GET /api/metrics", apiCfg.metricsHandler)
	mux.HandleFunc("POST /api/reset", apiCfg.metricsResetHandler)
	mux.HandleFunc("GET /admin/metrics", apiCfg.metricsPageHandler)
	mux.HandleFunc("POST /admin/reset", apiCfg.metricsResetHandler)

	mux.HandleFunc("POST /api/users", apiCfg.createUserHandler)
	mux.HandleFunc("PUT /api/users", apiCfg.updateUserHandler)
	mux.HandleFunc("POST /api/login", apiCfg.loginHandler)
	mux.HandleFunc("POST /api/refresh", apiCfg.refreshHandler)
	mux.HandleFunc("POST /api/revoke", apiCfg.revokeHandler)

	mux.HandleFunc("GET /api/chirps", apiCfg.getChirpsHandler)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.getChirpHandler)
	mux.HandleFunc("POST /api/chirps", apiCfg.createChirpHandler)
	mux.HandleFunc("DELETE /api/chirps/{chirpID}", apiCfg.deleteChirpHandler)

	mux.HandleFunc("POST /api/polka/webhooks", apiCfg.polkaWebHookHandler)

	fileHandler := http.FileServer(http.Dir("."))
	mux.Handle("/app/", http.StripPrefix("/app", apiCfg.middlewareMetricsInc(fileHandler)))

	err = server.ListenAndServe()
	if err != nil {
		log.Fatal("Error running server: ", err)
	}
}
