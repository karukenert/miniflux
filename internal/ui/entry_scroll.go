// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui // import "miniflux.app/v2/internal/ui"

import (
	json_parser "encoding/json"
	"errors"
	"net/http"

	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
)

func (h *handler) scrollEntry(w http.ResponseWriter, r *http.Request) {
	entryID := request.RouteInt64Param(r, "entryID")
	if entryID == 0 {
		response.JSONBadRequest(w, r, errors.New("invalid entry ID"))
		return
	}

	type entryScrollRequest struct {
		ScrollPercent float32 `json:"scroll_percent"`
	}

	var postData entryScrollRequest
	if err := json_parser.NewDecoder(r.Body).Decode(&postData); err != nil {
		response.JSONBadRequest(w, r, err)
		return
	}

	if postData.ScrollPercent < 0 || postData.ScrollPercent > 1 {
		response.JSONBadRequest(w, r, errors.New("scroll_percent must be between 0 and 1"))
		return
	}

	entry, err := h.store.NewEntryQueryBuilder(request.UserID(r)).
		WithEntryIDs(entryID).
		GetEntry()
	if err != nil {
		response.JSONServerError(w, r, err)
		return
	}

	if entry == nil {
		response.JSONNotFound(w, r)
		return
	}

	if err := h.store.SetEntryScrollPercent(entry.UserID, entry.ID, postData.ScrollPercent); err != nil {
		response.JSONServerError(w, r, err)
		return
	}

	response.JSONCreated(w, r, map[string]string{"message": "saved"})
}
