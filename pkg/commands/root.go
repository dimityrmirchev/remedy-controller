package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.wdf.sap.corp/kubernetes/remedy-controller/pkg/client"
	azclient "github.wdf.sap.corp/kubernetes/remedy-controller/pkg/client/azure"
	k8sclient "github.wdf.sap.corp/kubernetes/remedy-controller/pkg/client/k8s"
	"github.wdf.sap.corp/kubernetes/remedy-controller/pkg/remedies/pubips"
)

// GetRootCommand TODO
func GetRootCommand() *cobra.Command {
	var (
		kubeconfigPath, azureConfigPath, logLevel string
		cmd                                       = &cobra.Command{
			Use:  "azure-remedy-applier",
			Long: "TODO",
			Run: func(cmd *cobra.Command, args []string) {
				client.ConfigureLogger(logLevel)

				// Register a signal handler and create root context to shutdown the app with a graceperiod.
				ctx, cancel := context.WithCancel(context.Background())
				interuptCh := make(chan os.Signal, 1)
				signal.Notify(interuptCh, os.Interrupt, syscall.SIGTERM)

				k8sClientSet, err := k8sclient.GetClientSet(kubeconfigPath)
				if err != nil {
					fmt.Println(err.Error())
					os.Exit(1)
				}

				credentials, err := azclient.ReadConfig(azureConfigPath)
				if err != nil {
					fmt.Println(err.Error())
					os.Exit(1)
				}

				clients, err := azclient.NewClients(credentials)
				if err != nil {
					fmt.Println(err.Error())
					os.Exit(1)
				}

				go pubips.CleanPubIps(ctx, k8sClientSet, clients, credentials.ResourceGroup)

				select { // nolint:gosimple
				case <-interuptCh:
					signal.Stop(interuptCh)
					log.Info("Received stop signal, shutting down with grace period.")
					cancel()
					time.Sleep(time.Second * 5)
					log.Info("Shut down.")
				}
			},
		}
	)
	cmd.Flags().StringVar(&kubeconfigPath, "kubeconfig", "", "path to kubeconfig to target whatever")
	cmd.Flags().StringVar(&azureConfigPath, "infrastructure-config", "", "path to infrastructure config")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "log level: error|info|debug")

	_ = cmd.MarkFlagRequired("kubeconfig")
	_ = cmd.MarkFlagRequired("infrastructure-config")

	cmd.AddCommand(getVersionCommand())

	return cmd
}