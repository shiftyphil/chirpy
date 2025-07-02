package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync/atomic"
)

// API
type apiConfig struct {
	fileserverHits atomic.Int32
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

func decodePostBody(body io.Reader, params interface{}) error {
	decoder := json.NewDecoder(body)
	err := decoder.Decode(&params)
	return err
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
	writer.Header().Add("Content-Type", "text/plain; charset=utf-8")
	cfg.fileserverHits.Store(0)
	writer.WriteHeader(http.StatusOK)
	writer.Write([]byte("OK"))
}

func (cfg *apiConfig) metricsPageHandler(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Add("Content-Type", "text/html")
	writer.WriteHeader(http.StatusOK)
	writer.Write([]byte(fmt.Sprintf("<html>\n  <body>\n    <h1>Welcome, Chirpy Admin</h1>\n    <p>Chirpy has been visited %d times!</p>\n  </body>\n</html>", cfg.fileserverHits.Load())))
}

func validateHandler(writer http.ResponseWriter, request *http.Request) {
	type validatePostBody struct {
		Body string `json:"body"`
	}
	type response struct {
		Valid bool `json:"valid"`
	}

	params := validatePostBody{}
	err := decodePostBody(request.Body, &params)
	if err != nil {
		sendJsonBadRequestError(writer, err.Error())
		return
	}

	if len(params.Body) > 140 {
		sendJsonBadRequestError(writer, "Chirp is too long")
		return
	}

	sendJsonSuccessResponse(writer, response{Valid: true})
}

func main() {
	var err error

	mux := http.NewServeMux()
	server := http.Server{
		Handler: mux,
		Addr:    ":8080",
	}

	apiCfg := apiConfig{}

	mux.HandleFunc("GET /api/healthz", healthHandler)
	mux.HandleFunc("GET /api/metrics", apiCfg.metricsHandler)
	mux.HandleFunc("POST /api/reset", apiCfg.metricsResetHandler)
	mux.HandleFunc("GET /admin/metrics", apiCfg.metricsPageHandler)
	mux.HandleFunc("POST /admin/reset", apiCfg.metricsResetHandler)

	mux.HandleFunc("POST /api/validate_chirp", validateHandler)

	fileHandler := http.FileServer(http.Dir("."))
	mux.Handle("/app/", http.StripPrefix("/app", apiCfg.middlewareMetricsInc(fileHandler)))

	err = server.ListenAndServe()
	if err != nil {
		log.Fatal("Error running server: ", err)
	}
}
