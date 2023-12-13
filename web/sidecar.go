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
func (h *SidecarHandler) UpdateConfig() http.HandlerFunc {
	return h.wrapSidecarApi(http.MethodPut, h.updateConfig)
}

// EXTENSION: 扩展的 sidecar 功能
func (h *SidecarHandler) GetRuntimeInfo() http.HandlerFunc {
	return h.wrapSidecarApi(http.MethodGet, h.getRuntimeInfo)
}

// EXTENSION: 扩展的 sidecar 功能
func (h *SidecarHandler) ResetConfig() http.HandlerFunc {
	return h.wrapSidecarApi(http.MethodPost, h.resetConfig)
}

func (h *SidecarHandler) updateConfig(q *http.Request) sidecarApiFuncResult {
	level.Info(h.logger).Log("msg", "Refreshing configuration")

	var cmd sidecar.UpdateConfigCmd
	err := json.NewDecoder(q.Body).Decode(&cmd)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusBadRequest, summary: "Parse request json error", err: err},
		}
	}
	err = h.sidecarSvc.UpdateConfigReload(q.Context(), &cmd, h.reloadCh)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusInternalServerError, summary: "Update configuration error", err: err},
		}
	}
	level.Info(h.logger).Log("msg", "Completed refreshing configuration")
	return sidecarApiFuncResult{}
}

func (h *SidecarHandler) getRuntimeInfo(q *http.Request) sidecarApiFuncResult {
	return sidecarApiFuncResult{data: h.sidecarSvc.GetRuntimeInfo()}
}

type ResetConfigCmd struct {
	ZoneId string `json:"zone_id"`
}

func (h *SidecarHandler) resetConfig(q *http.Request) sidecarApiFuncResult {
	level.Info(h.logger).Log("msg", "Resetting configuration")
	var cmd ResetConfigCmd
	err := json.NewDecoder(q.Body).Decode(&cmd)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusBadRequest, summary: "Parse request json error", err: err},
		}
	}
	err = h.sidecarSvc.ResetConfigReload(q.Context(), cmd.ZoneId, h.reloadCh)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusInternalServerError, summary: "Reset configuration error", err: err},
		}
	}

	level.Info(h.logger).Log("msg", "Completed resetting configuration")
	return sidecarApiFuncResult{}
}

type SidecarResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message,omitempty"`
	Error   string      `json:"error,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type sidecarApiFuncResult struct {
	data interface{}
	err  *sidecarApiError
}

type sidecarApiError struct {
	code    int
	summary string
	err     error
}

type sidecarApiFunc func(r *http.Request) sidecarApiFuncResult

func (h *SidecarHandler) wrapSidecarApi(method string, f sidecarApiFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			h.logger.Log("msg", "Http Method now allowed")
			http.Error(w, "Http Method now allowed", http.StatusInternalServerError)
			return
		}

		result := f(r)
		if result.err != nil {
			h.respondSidecarError(w, result.err, result.data)
			return
		}
		h.respondSidecar(w, result.data)
	})
}

func (h *SidecarHandler) respondSidecar(w http.ResponseWriter, data interface{}) {
	b, err := json.Marshal(&SidecarResponse{
		Code:    http.StatusOK,
		Message: "success",
		Data:    data,
	})
	if err != nil {
		level.Error(h.logger).Log("msg", "error marshaling json response", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if n, err := w.Write(b); err != nil {
		level.Error(h.logger).Log("msg", "error writing response", "bytesWritten", n, "err", err)
	}
}

func (h *SidecarHandler) respondSidecarError(w http.ResponseWriter, apiErr *sidecarApiError, data interface{}) {
	b, err := json.Marshal(&SidecarResponse{
		Code:    apiErr.code,
		Message: apiErr.summary,
		Error:   apiErr.err.Error(),
		Data:    data,
	})
	if err != nil {
		level.Error(h.logger).Log("msg", "error marshaling json response", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(apiErr.code)
	if n, err := w.Write(b); err != nil {
		level.Error(h.logger).Log("msg", "error writing response", "bytesWritten", n, "err", err)
	}
}
