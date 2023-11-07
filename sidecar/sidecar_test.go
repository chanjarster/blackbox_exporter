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
	"bytes"
	"context"
	"fmt"
	"github.com/pkg/errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/go-kit/log"
)

func Test_sidecarService_UpdateConfigReload(t *testing.T) {
	testDir, err := os.MkdirTemp("", "prom-config")
	if err != nil {
		t.Error(err)
		return
	}

	fmt.Println("test dir:", testDir)
	defer os.RemoveAll(testDir)

	configFile := filepath.Join(testDir, "blackbox.yml")
	testConfigYaml, err := os.ReadFile("../test-data/blackbox.yml")
	if err != nil {
		t.Error(err)
		return
	}
	err = os.WriteFile(configFile, testConfigYaml, 0o666)
	if err != nil {
		t.Error(err)
		return
	}

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: configFile,
	}

	if !reflect.DeepEqual(time.Time{}, s.GetLastUpdateTs()) {
		t.Error("GetLastUpdateTs() not zero")
		return
	}

	cmd := &UpdateConfigCmd{
		Yaml: `
modules:
  http_2xx:
    prober: http
    http:
      preferred_ip_protocol: "ip4"
`,
	}

	reloadCh := make(chan chan error)
	go func() {
		ch := <-reloadCh
		ch <- nil
	}()
	err = s.UpdateConfigReload(context.TODO(), cmd, reloadCh)
	if err != nil {
		t.Error(err)
		return
	}
	if reflect.DeepEqual(time.Time{}, s.GetLastUpdateTs()) {
		t.Error("GetLastUpdateTs() still zero")
		return
	}
}

func Test_sidecarService_UpdateConfigReload_FailRecover(t *testing.T) {
	requireNoError := func(t *testing.T, err error) bool {
		if err != nil {
			t.Error(err)
			return false
		}
		return true
	}

	testDir, err := os.MkdirTemp("", "prom-config")
	if !requireNoError(t, err) {
		return
	}

	fmt.Println("test dir:", testDir)
	defer os.RemoveAll(testDir)

	configFile := filepath.Join(testDir, "blackbox.yml")
	beforeUpdateConfigYaml, err := os.ReadFile("../test-data/blackbox.yml")
	if !requireNoError(t, err) {
		return
	}
	err = os.WriteFile(configFile, beforeUpdateConfigYaml, 0o666)
	if !requireNoError(t, err) {
		return
	}

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: configFile,
	}

	if !reflect.DeepEqual(time.Time{}, s.GetLastUpdateTs()) {
		t.Error("GetLastUpdateTs() not zero")
		return
	}

	{
		cmd := &UpdateConfigCmd{
			Yaml: `
modules:
  http_2xx:
    prober: http
    blah blah
    http:
      preferred_ip_protocol: "ip4"
`,
		}
		// parse yaml 的时候出现错误
		reloadCh := make(chan chan error)
		go func() {
			ch := <-reloadCh
			ch <- nil
		}()
		err = s.UpdateConfigReload(context.TODO(), cmd, reloadCh)
		if err == nil {
			t.Error("UpdateConfigReload should return err")
			return
		}
		if !reflect.DeepEqual(time.Time{}, s.GetLastUpdateTs()) {
			t.Error("GetLastUpdateTs() should not be updated")
			return
		}

		afterUpdateConfigYaml, err := os.ReadFile("../test-data/blackbox.yml")
		if !requireNoError(t, err) {
			return
		}
		if !bytes.Equal(afterUpdateConfigYaml, beforeUpdateConfigYaml) {
			t.Error("UpdateConfigReload fail should keep old file unchanged")
		}
	}

	{
		cmd := &UpdateConfigCmd{
			Yaml: `
modules:
  http_2xx:
    prober: http
    http:
      preferred_ip_protocol: "ip4"
`,
		}
		// blackbox reload 时发生错误
		reloadCh := make(chan chan error)
		go func() {
			ch := <-reloadCh
			ch <- errors.New("blah blah")
		}()
		err = s.UpdateConfigReload(context.TODO(), cmd, reloadCh)
		if err == nil {
			t.Error("UpdateConfigReload should return err")
			return
		}
		if !reflect.DeepEqual(time.Time{}, s.GetLastUpdateTs()) {
			t.Error("GetLastUpdateTs() should not be updated")
			return
		}

		afterUpdateConfigYaml, err := os.ReadFile("../test-data/blackbox.yml")
		if !requireNoError(t, err) {
			return
		}
		if !bytes.Equal(afterUpdateConfigYaml, beforeUpdateConfigYaml) {
			t.Error("UpdateConfigReload fail should keep old file unchanged")
		}
	}

}
