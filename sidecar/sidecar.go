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

package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	fsutil "github.com/prometheus/blackbox_exporter/sidecar/utils/fs"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"

	bbconfig "github.com/prometheus/blackbox_exporter/config"
	"github.com/prometheus/blackbox_exporter/sidecar/errs"
)

type UpdateConfigCmd struct {
	ZoneId string `json:"zone_id"`
	Yaml   string `json:"yaml"`
}

func (cmd *UpdateConfigCmd) Validate(logger log.Logger) errs.ValidateErrors {
	ves := make(errs.ValidateErrors, 0)
	if cmd.ZoneId = strings.TrimSpace(cmd.ZoneId); cmd.ZoneId == "" {
		ves = append(ves, "ZoneId must not be blank")
	}
	if strings.TrimSpace(cmd.Yaml) == "" {
		ves = append(ves, "Yaml must not be blank")
	}

	// 验证一下配置文件有没有问题
	_, err := cmd.ParseConfig()
	if err != nil {
		ves = append(ves, errs.ValidateError(err.Error()).Prefix("Invalid Yaml: "))
	}

	return ves
}

func (cmd *UpdateConfigCmd) ParseConfig() (*bbconfig.Config, error) {
	var c = &bbconfig.Config{}

	decoder := yaml.NewDecoder(strings.NewReader(cmd.Yaml))
	decoder.KnownFields(true)

	if err := decoder.Decode(c); err != nil {
		return nil, fmt.Errorf("error parsing config file: %s", err)
	}
	return c, nil
}

type SidecarService interface {
	// UpdateConfigReload 更新 Prometheus 配置文件，并且指示 Prometheus reload
	UpdateConfigReload(ctx context.Context, cmd *UpdateConfigCmd, reloadCh chan chan error) error
	// GetRuntimeInfo 获得上一次更新配置文件的时间，以及绑定的 ZoneId
	GetRuntimeInfo() *Runtimeinfo
	// ResetConfigReload 重置 --custom.config.file 配置文件的内容
	//  解绑 ZoneId
	//  清空 “配置变更时间戳”
	//  指示 Snmp Exporter reload
	ResetConfigReload(ctx context.Context, zoneId string, reloadCh chan chan error) error
}

func New(logger log.Logger, configFile string) SidecarService {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &sidecarService{
		logger:     logger,
		configFile: configFile,
	}
}

type sidecarService struct {
	logger     log.Logger
	configFile string

	runtimeLock  sync.Mutex
	boundZoneId  string    // 当前所绑定的 zoneId
	lastUpdateTs time.Time // 上一次更新配置文件的时间戳
}

const (
	brand = "blackbox-exporter-mod"
)

type Runtimeinfo struct {
	Brand        string    `json:"brand"`
	ZoneId       string    `json:"zone_id"`
	LastUpdateTs time.Time `json:"last_update_ts"`
}

func (r *Runtimeinfo) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	return json.Marshal(map[string]interface{}{
		"brand":          r.Brand,
		"zone_id":        r.ZoneId,
		"last_update_ts": r.LastUpdateTs.UnixMilli(),
	})
}

func (s *sidecarService) GetRuntimeInfo() *Runtimeinfo {
	s.runtimeLock.Lock()
	defer s.runtimeLock.Unlock()
	return &Runtimeinfo{
		Brand:        brand,
		ZoneId:       s.boundZoneId,
		LastUpdateTs: s.lastUpdateTs,
	}
}

func (s *sidecarService) UpdateConfigReload(ctx context.Context, cmd *UpdateConfigCmd, reloadCh chan chan error) error {
	verrs := cmd.Validate(s.logger)
	if len(verrs) > 0 {
		return verrs
	}

	s.runtimeLock.Lock()
	defer s.runtimeLock.Unlock()

	if err := s.assertZoneIdMatch(cmd.ZoneId); err != nil {
		return err
	}

	cfgFileUtil := &configFileUtil{configFile: s.configFile}

	var reloadErr error
	defer func() {
		if reloadErr == nil {
			s.lastUpdateTs = time.Now()
			s.bindZoneId(cmd.ZoneId)
			// 没有出错
			s.printErr(cfgFileUtil.cleanBackupConfigFile())
		} else {
			// 出错了
			s.printErr(cfgFileUtil.restoreConfigFile())
		}
	}()

	if reloadErr = cfgFileUtil.backupConfigFile(); reloadErr != nil {
		return reloadErr
	}
	// 更新配置文件
	if reloadErr = cfgFileUtil.writeConfigFile(cmd.Yaml); reloadErr != nil {
		// 恢复旧文件
		return reloadErr
	}

	// 指示 Blackbox reload 配置文件
	reloadErr = s.doReload(reloadCh)
	return reloadErr
}

func (s *sidecarService) ResetConfigReload(ctx context.Context, zoneId string, reloadCh chan chan error) error {
	s.runtimeLock.Lock()
	defer s.runtimeLock.Unlock()

	if zoneId = strings.TrimSpace(zoneId); zoneId == "" {
		return errs.ValidateError("ZoneId must not be blank")
	}

	if err := s.assertZoneIdMatch(zoneId); err != nil {
		return err
	}

	cfgFileUtil := &configFileUtil{configFile: s.configFile}

	var reloadErr error

	defer func() {
		if reloadErr == nil {
			s.lastUpdateTs = time.Time{}
			s.bindZoneId("")
			// 没有出错
			s.printErr(cfgFileUtil.cleanBackupConfigFile())
		} else {
			// 出错了
			s.printErr(cfgFileUtil.restoreConfigFile())
		}
	}()
	if reloadErr = cfgFileUtil.backupConfigFile(); reloadErr != nil {
		return reloadErr
	}

	// 更新配置文件为空文件
	emptyCfg := &bbconfig.Config{}
	emptyCfgYamlB, err := json.Marshal(emptyCfg)
	if err != nil {
		reloadErr = err
		return err
	}
	if reloadErr = cfgFileUtil.writeConfigFile(string(emptyCfgYamlB)); reloadErr != nil {
		// 恢复旧文件
		return reloadErr
	}

	// 指示 Prometheus reload 配置文件
	reloadErr = s.doReload(reloadCh)
	return reloadErr
}

func (s *sidecarService) assertZoneIdMatch(zoneId string) error {
	if s.boundZoneId == "" {
		// 这个 prometheus 还没有和 zone 绑定过
		return nil
	}
	if s.boundZoneId != zoneId {
		return errs.ValidateErrorf("Current blackbox-exporter bound zoneId=%s, command zoneId=%s, mismatch",
			s.boundZoneId, zoneId)
	}
	return nil
}

func (s *sidecarService) bindZoneId(zoneId string) {
	s.boundZoneId = zoneId
}

func (s *sidecarService) printErr(err error) {
	if err == nil {
		return
	}

	if errList, ok := err.(fsutil.ErrorList); ok {
		for _, err2 := range errList {
			level.Warn(s.logger).Log("err", err2)
		}
	} else {
		level.Warn(s.logger).Log("err", err)
	}
}

func (s *sidecarService) doReload(reloadCh chan chan error) error {
	rc := make(chan error)
	reloadCh <- rc
	if err := <-rc; err != nil {
		return errors.Wrapf(err, "sidecar failed to reload config")
	}
	return nil
}
