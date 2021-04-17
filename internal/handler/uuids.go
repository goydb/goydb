package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	uuid "github.com/satori/go.uuid"
)

type UUIDs struct{}

func (s *UUIDs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	count, err := strconv.ParseInt(r.URL.Query().Get("count"), 10, 64)
	if err != nil {
		count = 1
	}

	response := &UUIDsResponse{
		Uuids: make([]string, count),
	}

	for i := int64(0); i < count; i++ {
		u2 := uuid.NewV4()
		response.Uuids[i] = u2.String()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

type UUIDsResponse struct {
	Uuids []string `json:"uuids"`
}
