/*
Copyright 2019-2020 vChain, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gw

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/codenotary/immudb/pkg/client"
)

type Options struct {
	Dir             string
	Address         string
	Port            int
	MetricsPort     int
	ImmudbAddress   string
	ImmudbPort      int
	Auditor         bool
	AuditorInterval time.Duration
	AuditorUsername string
	AuditorPassword string
	Detached        bool
	MTLs            bool
	MTLsOptions     client.MTLsOptions
	Config          string
	Pidfile         string
	Logfile         string
}

func DefaultOptions() Options {
	return Options{
		Dir:             ".",
		Address:         "127.0.0.1",
		Port:            3323,
		MetricsPort:     9498,
		ImmudbAddress:   "127.0.0.1",
		ImmudbPort:      3322,
		Auditor:         false,
		AuditorInterval: 5 * time.Minute,
		AuditorUsername: "immugwauditor",
		AuditorPassword: "",
		Detached:        false,
		MTLs:            false,
		Config:          "configs/immugw.toml",
		Pidfile:         "",
		Logfile:         "",
	}
}

// WithDir sets dir
func (o Options) WithDir(dir string) Options {
	o.Dir = dir
	return o
}

// WithAddress sets address
func (o Options) WithAddress(address string) Options {
	o.Address = address
	return o
}

// WithPort sets port
func (o Options) WithPort(port int) Options {
	o.Port = port
	return o
}

// WithImmudbAddress sets immudbAddress
func (o Options) WithImmudbAddress(immudbAddress string) Options {
	o.ImmudbAddress = immudbAddress
	return o
}

// WithImmudbPort sets immudbPort
func (o Options) WithImmudbPort(immudbPort int) Options {
	o.ImmudbPort = immudbPort
	return o
}

// WithAuditor sets Auditor
func (o Options) WithAuditor(auditor bool) Options {
	o.Auditor = auditor
	return o
}

// WithAuditorInterval sets AuditorInterval
func (o Options) WithAuditorInterval(auditorInterval time.Duration) Options {
	o.AuditorInterval = auditorInterval
	return o
}

// WithAuditorUsername sets AuditorUsername
func (o Options) WithAuditorUsername(auditorUsername string) Options {
	o.AuditorUsername = auditorUsername
	return o
}

// WithAuditorPassword sets AuditorPassword
func (o Options) WithAuditorPassword(auditorPassword string) Options {
	o.AuditorPassword = auditorPassword
	return o
}

// WithMTLs sets MTLs
func (o Options) WithMTLs(MTLs bool) Options {
	o.MTLs = MTLs
	return o
}

// WithDetached sets immugw to be run in background
func (o Options) WithDetached(detached bool) Options {
	o.Detached = detached
	return o
}

// WithMTLsOptions sets MTLsOptions
func (o Options) WithMTLsOptions(MTLsOptions client.MTLsOptions) Options {
	o.MTLsOptions = MTLsOptions
	return o
}

// WithConfig sets config
func (o Options) WithConfig(config string) Options {
	o.Config = config
	return o
}

// WithPidfile sets pidfile
func (o Options) WithPidfile(pidfile string) Options {
	o.Pidfile = pidfile
	return o
}

// WithLogfile sets logfile
func (o Options) WithLogfile(logfile string) Options {
	o.Logfile = logfile
	return o
}

func (o Options) Bind() string {
	return fmt.Sprintf("%s:%d", o.Address, o.Port)
}

// MetricsBind return metrics bind address
func (o Options) MetricsBind() string {
	return fmt.Sprintf("%s:%d", o.Address, o.MetricsPort)
}

func (o Options) String() string {
	optionsJson, err := json.Marshal(o)
	if err != nil {
		return err.Error()
	}
	return string(optionsJson)
}
