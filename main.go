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
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

func UserFromDb(dbUser database.User) *User {
	return &User{
		ID:        dbUser.ID,
		CreatedAt: dbUser.CreatedAt,
		UpdatedAt: dbUser.UpdatedAt,
		Email:     dbUser.Email,
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

func (cfg *apiConfig) loginHandler(writer http.ResponseWriter, request *http.Request) {
	type loginPostBody struct {
		Email    string `json:"email"`
		Password string `json:"password"`
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

	sendJsonSuccessResponse(writer, user)
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
	dbChirps, err := cfg.db.GetChirps(request.Context())
	if err != nil {
		sendJsonInternalServerError(writer, err.Error())
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
		Body   string    `json:"body"`
		UserId uuid.UUID `json:"user_id"`
	}

	params := createChirpPostBody{}
	err := decodePostBody(request.Body, &params)
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	cleanBody, err := validateChirp(params.Body)
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	dbChirp, err := cfg.db.CreateChirp(request.Context(), database.CreateChirpParams{Body: cleanBody, UserID: params.UserId})
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	chirp := ChirpFromDb(dbChirp)

	sendJsonCreatedResponse(writer, chirp)
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

	apiCfg := apiConfig{db: dbQueries}

	mux.HandleFunc("GET /api/healthz", healthHandler)
	mux.HandleFunc("GET /api/metrics", apiCfg.metricsHandler)
	mux.HandleFunc("POST /api/reset", apiCfg.metricsResetHandler)
	mux.HandleFunc("GET /admin/metrics", apiCfg.metricsPageHandler)
	mux.HandleFunc("POST /admin/reset", apiCfg.metricsResetHandler)

	mux.HandleFunc("POST /api/users", apiCfg.createUserHandler)
	mux.HandleFunc("POST /api/login", apiCfg.loginHandler)

	mux.HandleFunc("GET /api/chirps", apiCfg.getChirpsHandler)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.getChirpHandler)
	mux.HandleFunc("POST /api/chirps", apiCfg.createChirpHandler)

	fileHandler := http.FileServer(http.Dir("."))
	mux.Handle("/app/", http.StripPrefix("/app", apiCfg.middlewareMetricsInc(fileHandler)))

	err = server.ListenAndServe()
	if err != nil {
		log.Fatal("Error running server: ", err)
	}
}
