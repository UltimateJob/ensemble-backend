// Package httpapi 是 HTTP 适配层。
package httpapi

import (
	"encoding/json"
	"net/http"
)

type envelope struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data,omitempty"`
	Error *apiError   `json:"error,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func ok(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, envelope{OK: true, Data: data})
}

func created(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusCreated, envelope{OK: true, Data: data})
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, envelope{OK: false, Error: &apiError{Code: code, Message: msg}})
}

func badRequest(w http.ResponseWriter, msg string)   { fail(w, 400, "bad_request", msg) }
func unauthorized(w http.ResponseWriter, msg string) { fail(w, 401, "unauthorized", msg) }
func forbidden(w http.ResponseWriter)                { fail(w, 403, "forbidden", "没有权限") }
func notFound(w http.ResponseWriter, msg string)     { fail(w, 404, "not_found", msg) }
func serverErr(w http.ResponseWriter, msg string)    { fail(w, 500, "internal", msg) }

func decode(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
