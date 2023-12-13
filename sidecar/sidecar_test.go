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
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func Test_sidecarService_UpdateConfigReload(t *testing.T) {
	testDir := t.TempDir()
	fmt.Println("test dir:", testDir)

	configFile := filepath.Join(testDir, "blackbox.yml")
	templateConfigYaml, err := os.ReadFile(filepath.Join("..", "blackbox.yml"))
	require.NoError(t, err)

	// 准备一下文件
	err = os.WriteFile(configFile, templateConfigYaml, 0o666)
	if err != nil {
		t.Error(err)
		return
	}

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: configFile,
	}

	rt := s.GetRuntimeInfo()
	require.Equal(t, brand, rt.Brand)
	require.Equal(t, "", rt.ZoneId)
	require.Equal(t, time.Time{}, rt.LastUpdateTs)

	cmd := &UpdateConfigCmd{
		ZoneId: "default",
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
	require.NoError(t, s.UpdateConfigReload(context.TODO(), cmd, reloadCh))
	rt = s.GetRuntimeInfo()
	require.Equal(t, brand, rt.Brand)
	require.Equal(t, cmd.ZoneId, rt.ZoneId)
	require.NotEqual(t, time.Time{}, rt.LastUpdateTs)

	// 配置文件写入了
	configFileB, err := os.ReadFile(configFile)
	require.NoError(t, err)
	require.NotEqual(t, templateConfigYaml, configFileB)

}

func Test_sidecarService_UpdateConfigReload_ZoneIdMismatch(t *testing.T) {
	testDir := t.TempDir()
	fmt.Println("test dir:", testDir)

	configFile := filepath.Join(testDir, "blackbox.yml")
	templateConfigYaml, err := os.ReadFile(filepath.Join("..", "blackbox.yml"))
	require.NoError(t, err)

	// 预先准备一下文件
	err = os.WriteFile(configFile, templateConfigYaml, 0o666)
	require.NoError(t, err)

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: configFile,
	}

	rt := s.GetRuntimeInfo()
	require.Equal(t, brand, rt.Brand)
	require.Equal(t, "", rt.ZoneId)
	require.Equal(t, time.Time{}, rt.LastUpdateTs)

	{
		// 先做一次更新
		cmd := &UpdateConfigCmd{
			ZoneId: "default",
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
		require.NoError(t, err)
		rt = s.GetRuntimeInfo()
		require.Equal(t, brand, rt.Brand)
		require.Equal(t, "default", rt.ZoneId)
		require.NotEqual(t, time.Time{}, rt.LastUpdateTs)
	}

	{
		lastRt := s.GetRuntimeInfo()
		lastConfigFileB, err := os.ReadFile(configFile)
		require.NoError(t, err)

		// 下达一个 zoneId 不匹配的指令
		cmd2 := &UpdateConfigCmd{
			ZoneId: "default2",
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
		err = s.UpdateConfigReload(context.TODO(), cmd2, reloadCh)
		require.Error(t, err)

		thisRt := s.GetRuntimeInfo()
		// 配置绑定 zoneId 没有变更，时间戳也没变
		require.Equal(t, lastRt, thisRt)
		// 配置文件也没有变更
		thisConfigFileB, err := os.ReadFile(configFile)
		require.NoError(t, err)
		require.Equal(t, lastConfigFileB, thisConfigFileB)
	}
}

func Test_sidecarService_UpdateConfigReload_ErrorRestore(t *testing.T) {
	testDir := t.TempDir()
	fmt.Println("test dir:", testDir)

	configFile := filepath.Join(testDir, "blackbox.yml")
	templateConfigYaml, err := os.ReadFile(filepath.Join("..", "blackbox.yml"))
	require.NoError(t, err)

	// 预先准备一下文件
	err = os.WriteFile(configFile, templateConfigYaml, 0o666)
	require.NoError(t, err)

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: configFile,
	}

	rt := s.GetRuntimeInfo()
	require.Equal(t, brand, rt.Brand)
	require.Equal(t, "", rt.ZoneId)
	require.Equal(t, time.Time{}, rt.LastUpdateTs)

	t.Run("bad yaml", func(t *testing.T) {

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
		require.Error(t, err, "UpdateConfigReload should return err")
		rt = s.GetRuntimeInfo()
		require.Equal(t, brand, rt.Brand)
		require.Equal(t, "", rt.ZoneId)
		require.Equal(t, time.Time{}, rt.LastUpdateTs)

		afterUpdateConfigYaml, err := os.ReadFile(filepath.Join("..", "blackbox.yml"))
		require.NoError(t, err)
		require.Equal(t, templateConfigYaml, afterUpdateConfigYaml, "UpdateConfigReload fail should keep old file unchanged")

	})

	t.Run("reload error happen", func(t *testing.T) {

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
			ch <- errors.New("on purpose")
		}()
		err = s.UpdateConfigReload(context.TODO(), cmd, reloadCh)
		require.Error(t, err, "UpdateConfigReload should return err")
		// 绑定的 zoneId 依然是空，时间戳也是空
		rt = s.GetRuntimeInfo()
		require.Equal(t, brand, rt.Brand)
		require.Equal(t, "", rt.ZoneId)
		require.Equal(t, time.Time{}, rt.LastUpdateTs)

		afterUpdateConfigYaml, err := os.ReadFile(filepath.Join("..", "blackbox.yml"))
		require.NoError(t, err)
		require.Equal(t, templateConfigYaml, afterUpdateConfigYaml, "UpdateConfigReload fail should keep old file unchanged")
	})

}

func Test_sidecarService_ResetConfig(t *testing.T) {
	testDir := t.TempDir()
	fmt.Println("test dir:", testDir)

	configFile := filepath.Join(testDir, "blackbox.yml")
	templateConfigYaml, err := os.ReadFile(filepath.Join("..", "blackbox.yml"))
	require.NoError(t, err)

	// 准备一下文件
	err = os.WriteFile(configFile, templateConfigYaml, 0o666)
	if err != nil {
		t.Error(err)
		return
	}

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: configFile,
	}

	rt := s.GetRuntimeInfo()
	require.Equal(t, brand, rt.Brand)
	require.Equal(t, "", rt.ZoneId)
	require.Equal(t, time.Time{}, rt.LastUpdateTs)

	t.Run("reset empty", func(t *testing.T) {
		reloadCh := make(chan chan error)
		go func() {
			ch := <-reloadCh
			ch <- nil
		}()
		require.NoError(t, s.ResetConfigReload(context.TODO(), "default", reloadCh))

		rt := s.GetRuntimeInfo()
		require.Equal(t, brand, rt.Brand)
		require.Equal(t, "", rt.ZoneId)
		require.Equal(t, time.Time{}, rt.LastUpdateTs)

		// 配置文件被重置
		configFileB, err := os.ReadFile(configFile)
		require.NoError(t, err)
		require.NotEqual(t, templateConfigYaml, configFileB)
	})

	t.Run("reset non-empty", func(t *testing.T) {
		cmd := &UpdateConfigCmd{
			ZoneId: "default",
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
		require.NoError(t, err)

		go func() {
			ch := <-reloadCh
			ch <- nil
		}()
		require.NoError(t, s.ResetConfigReload(context.TODO(), "default", reloadCh))

		rt := s.GetRuntimeInfo()
		require.Equal(t, brand, rt.Brand)
		require.Equal(t, "", rt.ZoneId)
		require.Equal(t, time.Time{}, rt.LastUpdateTs)

		// 配置文件被重置
		configFileB, err := os.ReadFile(configFile)
		require.NoError(t, err)
		require.NotEqual(t, []byte(cmd.Yaml), configFileB)
	})

	t.Run("reset zone_id mismatch", func(t *testing.T) {
		cmd := &UpdateConfigCmd{
			ZoneId: "default",
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
		require.NoError(t, err)

		go func() {
			ch := <-reloadCh
			ch <- nil
		}()
		require.Error(t, s.ResetConfigReload(context.TODO(), "default2", reloadCh))

		rt := s.GetRuntimeInfo()
		require.Equal(t, brand, rt.Brand)
		require.Equal(t, "default", rt.ZoneId)
		require.NotEqual(t, time.Time{}, rt.LastUpdateTs)

		// 配置文件没有被重置
		configFileB, err := os.ReadFile(configFile)
		require.NoError(t, err)
		require.Equal(t, []byte(cmd.Yaml), configFileB)
	})

	t.Run("reset reload error", func(t *testing.T) {
		cmd := &UpdateConfigCmd{
			ZoneId: "default",
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
		require.NoError(t, err)

		go func() {
			ch := <-reloadCh
			ch <- errors.New("on purpose")
		}()
		require.Error(t, s.ResetConfigReload(context.TODO(), "default", reloadCh))

		rt := s.GetRuntimeInfo()
		require.Equal(t, brand, rt.Brand)
		require.Equal(t, "default", rt.ZoneId)
		require.NotEqual(t, time.Time{}, rt.LastUpdateTs)

		// 配置文件没有被重置
		configFileB, err := os.ReadFile(configFile)
		require.NoError(t, err)
		require.Equal(t, []byte(cmd.Yaml), configFileB)
	})

}
