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
	"fmt"
	"os"
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
	Yaml string `json:"yaml"`
}

func (cmd *UpdateConfigCmd) Validate(logger log.Logger) errs.ValidateErrors {
	ves := make(errs.ValidateErrors, 0)
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
	// GetLastUpdateTs 获得上一次更新配置文件的时间
	GetLastUpdateTs() time.Time
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
	logger       log.Logger
	configFile   string
	lock         sync.Mutex
	lastUpdateTs time.Time // 上一次更新配置文件的时间戳
}

func (s *sidecarService) GetLastUpdateTs() time.Time {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.lastUpdateTs
}

func (s *sidecarService) UpdateConfigReload(ctx context.Context, cmd *UpdateConfigCmd, reloadCh chan chan error) error {
	verrs := cmd.Validate(s.logger)
	if len(verrs) > 0 {
		return verrs
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	oldConfigYaml, err := s.readConfigFile()
	if err != nil {
		return err
	}

	var reloadErr error
	defer func() {
		if reloadErr != nil {
			if err := s.writeConfigFile(oldConfigYaml); err != nil {
				level.Error(s.logger).Log("err", errors.Wrapf(err, "Recover config file error").Error())
			}
		}
	}()

	// 更新配置文件
	if reloadErr = s.writeConfigFile(cmd.Yaml); reloadErr != nil {
		// 恢复旧文件
		return reloadErr
	}

	// 指示 Blackbox reload 配置文件
	if reloadErr = s.doReload(reloadCh); reloadErr == nil {
		s.lastUpdateTs = time.Now()
	}
	return reloadErr
}

func (s *sidecarService) readConfigFile() (string, error) {
	configYamlB, err := os.ReadFile(s.configFile)
	if err != nil {
		return "", errors.Wrapf(err, "Write config file %q failed", s.configFile)
	}
	return string(configYamlB), nil
}

func (s *sidecarService) writeConfigFile(configYaml string) error {
	err := os.WriteFile(s.configFile, []byte(configYaml), 0o644)
	if err != nil {
		return errors.Wrapf(err, "Write config file %q failed", s.configFile)
	}
	return nil
}

func (s *sidecarService) doReload(reloadCh chan chan error) error {
	rc := make(chan error)
	reloadCh <- rc
	if err := <-rc; err != nil {
		return errors.Wrapf(err, "sidecar failed to reload config")
	}
	return nil
}
