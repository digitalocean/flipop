// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2021 Digital Ocean, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/digitalocean/flipop/pkg/floatingip"
	"github.com/digitalocean/flipop/pkg/leaderelection"
	logutil "github.com/digitalocean/flipop/pkg/log"
	"github.com/digitalocean/flipop/pkg/nodedns"
	"github.com/digitalocean/flipop/pkg/provider"
	"github.com/digitalocean/flipop/pkg/version"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/sirupsen/logrus"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	leaderElectionResource = "floating-ip-pool-controller-leader-election"
)

var debug bool
var keepLastRecord bool
var ctx context.Context
var log logrus.FieldLogger

var kubeconfig string
var healthBindAddr string

var rootCmd = &cobra.Command{
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if debug {
			logrus.SetLevel(logrus.DebugLevel)
		}
		if isatty.IsTerminal(os.Stdout.Fd()) {
			logrus.SetFormatter(&logrus.TextFormatter{})
		}
	},
	Run: runMain,
}

func init() {
	viper.SetEnvPrefix("flipop")

	logrus.SetFormatter(&logrus.JSONFormatter{})
	logrus.SetLevel(logrus.InfoLevel)

	ctx = context.Background()
	log = logutil.FromContext(ctx).WithField("version", version.Version)
	ctx = logutil.AddToContext(signalContext(ctx, log), log)
}

func main() {
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "debug logging")
	rootCmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file")
	rootCmd.Flags().StringVar(&healthBindAddr, "metrics-bind-addr", ":8080", "bind addr for metrics server, set empty string to disable")
	rootCmd.Flags().BoolVar(&keepLastRecord, "keep-last-record", false, "to avoid NXDomain, keep the last DNS record for a record name even if unhealthy")
	rootCmd.Execute()
}

func signalContext(ctx context.Context, log logrus.FieldLogger) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Info("got interrupt signal; shutting down")
		cancel()
		<-c
		log.Info("got second interrupt signal; unclean shutdown")
		os.Exit(1) // exit hard for the impatient
	}()

	return ctx
}

func runMain(cmd *cobra.Command, args []string) {
	promRegistry := prometheus.NewPedanticRegistry()
	promRegistry.MustRegister(
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
		prometheus.NewGoCollector(),
	)
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{}))

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})

	providers := provider.NewRegistry(
		provider.WithLogger(log),
		provider.WithKeepLastDNSRecord(keepLastRecord),
	)
	if err := providers.Init(); err != nil {
		fmt.Fprintln(os.Stdout, err.Error())
		os.Exit(1)
	}
	if err := promRegistry.Register(providers); err != nil {
		fmt.Fprintf(os.Stdout, "Failed to register providers with prometheus: %s\n", err)
		os.Exit(1)
	}

	flipCtrl, err := floatingip.NewController(config, providers, log, promRegistry)
	if err != nil {
		fmt.Fprintf(os.Stdout, "Failed to create Floating IP Pool controller: %s\n", err)
		os.Exit(1)
	}
	if err := promRegistry.Register(flipCtrl); err != nil {
		fmt.Fprintf(os.Stdout, "Failed to register Floating IP Pool controller with prometheus: %s\n", err)
		os.Exit(1)
	}

	nodednsCtrl, err := nodedns.NewController(config, providers, log)
	if err != nil {
		fmt.Fprintf(os.Stdout, "Failed to create NodeDNSRecordSet controller: %s\n", err)
		os.Exit(1)
	}
	if err := promRegistry.Register(nodednsCtrl); err != nil {
		fmt.Fprintf(os.Stdout, "Failed to register NodeDNSRecordSet controller with prometheus: %s\n", err)
		os.Exit(1)
	}

	ns, _, err := config.Namespace()
	if err != nil {
		fmt.Fprintf(os.Stdout, "determining kubernetes namespace: %s\n", err)
		os.Exit(1)
	}
	clientConfig, err := config.ClientConfig()
	if err != nil {
		fmt.Fprintf(os.Stdout, "building kubernetes client config: %s\n", err)
		os.Exit(1)
	}
	kubeCS, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		fmt.Fprintf(os.Stdout, "creating kubernetes client: %s\n", err)
		os.Exit(1)
	}

	if healthBindAddr != "" {
		go func() {
			if err := http.ListenAndServe(healthBindAddr, metricsMux); err != nil {
				fmt.Fprintf(os.Stdout, "failed to start metrics server (metrics-bind-addr=%q): %s\n", healthBindAddr, err)
				os.Exit(1)
			}
		}()
	}

	leaderelection.LeaderElection(ctx, log, ns, leaderElectionResource, kubeCS, flipCtrl.Run, nodednsCtrl.Run)
}
