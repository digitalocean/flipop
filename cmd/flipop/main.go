package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/digitalocean/flipop/pkg/floatingip"
	"github.com/digitalocean/flipop/pkg/leaderelection"
	"github.com/digitalocean/flipop/pkg/log"
	"github.com/digitalocean/flipop/pkg/provider"
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
var ctx context.Context
var ll logrus.FieldLogger

var kubeconfig string

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
	ll = log.FromContext(ctx)
	ctx = log.AddToContext(signalContext(ctx, ll), ll)
}

func main() {
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "debug logging")
	rootCmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file")
	rootCmd.Execute()
}

func signalContext(ctx context.Context, ll logrus.FieldLogger) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		ll.Info("got interrupt signal; shutting down")
		cancel()
		<-c
		ll.Info("got second interrupt signal; unclean shutdown")
		os.Exit(1) // exit hard for the impatient
	}()

	return ctx
}

func runMain(cmd *cobra.Command, args []string) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	providers := initProviders(ll)
	if len(providers) == 0 {
		fmt.Fprintf(os.Stdout, "No providers initialized. Set DIGITALOCEAN_ACCESS_TOKEN\n")
		os.Exit(1)
	}
	flipCtrl, err := floatingip.NewController(config, providers, ll)
	if err != nil {
		fmt.Fprintf(os.Stdout, "Failed to create Floating IP Pool controller: %s\n", err)
		os.Exit(1)
	}
	ns, _, err := config.Namespace()
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
	leaderelection.LeaderElection(ctx, ll, ns, leaderElectionResource, kubeCS, flipCtrl.Run)
}

func initProviders(ll logrus.FieldLogger) map[string]provider.Provider {
	out := make(map[string]provider.Provider)
	do := provider.NewDigitalOcean(ll)
	if do != nil {
		out[provider.DigitalOcean] = do
	}
	return out
}
