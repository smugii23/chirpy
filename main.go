package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync/atomic"
	"strings"
)

type apiConfig struct {
	fileserverHits atomic.Int32
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

func respondWithError (w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(code)
    json.NewEncoder(w).Encode(payload)
}

func cleanProfanity (w http.ResponseWriter, body requestData{}) {
	split := strings.Split(body.Body, " ")
	for i, word := range split {
		if strings.ToLower(word) == "kerfluffle" || strings.ToLower(word) == "sharbert" || strings.ToLower(word) == "fornax" {
			split[i] = "****"
		}
	}
	res := strings.Join(split, " ")
	encoder := json.NewEncoder(w)
	err := encoder.Encode(res)
	w.WriteHeader(code)
	w.Write(res)
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


func main() {
	apiCfg := &apiConfig{}
	mux := http.NewServeMux()
	server := &http.Server{
		Handler: mux,
		Addr:    "localhost:8080",
	}
	fileServer := http.FileServer(http.Dir("."))
	fileServerWithPrefix := http.StripPrefix("/app", fileServer)
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(fileServerWithPrefix))
	mux.HandleFunc("GET /admin/metrics", apiCfg.metricsHandler)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetHandler)
	mux.HandleFunc("GET /api/healthz", healthHandler)
	mux.HandleFunc("POST /api/validate_chirp", apiCfg.valid_chirp)
	server.ListenAndServe()
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
