// Copyright 2013 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package web

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/prometheus/blackbox_exporter/sidecar"
)

type SidecarHandler struct {
	logger     log.Logger
	sidecarSvc sidecar.SidecarService
	reloadCh   chan chan error
}

func NewSidecarHandler(logger log.Logger, sidecarSvc sidecar.SidecarService,
	reloadCh chan chan error) *SidecarHandler {
	return &SidecarHandler{
		logger:     logger,
		sidecarSvc: sidecarSvc,
		reloadCh:   reloadCh,
	}
}

// EXTENSION: 扩展的 sidecar 功能
func (h *SidecarHandler) UpdateConfig(w http.ResponseWriter, q *http.Request) {
	if q.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		h.logger.Log("msg", "This endpoint requires a PUT request")
		return
	}

	var cmd sidecar.UpdateConfigCmd
	err := json.NewDecoder(q.Body).Decode(&cmd)
	if err != nil {
		errmsg := fmt.Sprintf("Parse request json error: %s", err.Error())
		h.logger.Log("msg", errmsg)
		http.Error(w, errmsg, http.StatusBadRequest)
		return
	}
	err = h.sidecarSvc.UpdateConfigReload(q.Context(), &cmd, h.reloadCh)
	if err != nil {
		errmsg := fmt.Sprintf("Update configuration error: %s", err.Error())
		h.logger.Log("msg", errmsg)
		http.Error(w, errmsg, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(`{"code":200,"message":"success"}`))
	if err != nil {
		level.Error(h.logger).Log("err", err)
		return
	}

	h.logger.Log("msg", "Completed refreshing configuration")
}

// EXTENSION: 扩展的 sidecar 功能
func (h *SidecarHandler) GetLastUpdateTs(w http.ResponseWriter, q *http.Request) {
	if q.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		h.logger.Log("msg", "This endpoint requires a GET request")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte(fmt.Sprintf(`{"code":200,"message":"success","last_update_ts":%d}`, h.sidecarSvc.GetLastUpdateTs().UnixMilli())))
	if err != nil {
		level.Error(h.logger).Log("err", err)
	}
}
