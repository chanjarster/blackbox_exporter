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
	"github.com/pkg/errors"
	fsutil "github.com/prometheus/blackbox_exporter/sidecar/utils/fs"
	"os"
)

type configFileUtil struct {
	configFile string
}

func (s *configFileUtil) backupConfigFile() error {
	return fsutil.BackupFile(s.configFile)
}

func (s *configFileUtil) cleanBackupConfigFile() error {
	return fsutil.CleanBackupFile(s.configFile)
}

func (s *configFileUtil) restoreConfigFile() error {
	return fsutil.RestoreFile(s.configFile)
}

func (s *configFileUtil) writeConfigFile(configYaml string) error {
	err := os.WriteFile(s.configFile, []byte(configYaml), 0o644)
	if err != nil {
		return errors.Wrapf(err, "Write config file %q failed", s.configFile)
	}
	return nil
}
