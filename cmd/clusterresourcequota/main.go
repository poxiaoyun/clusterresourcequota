package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"xiaoshiai.cn/clusterresourcequota"
	"xiaoshiai.cn/common/config"
	"xiaoshiai.cn/common/version"
)

const ErrExitCode = 1

func main() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Println(err.Error())
		os.Exit(ErrExitCode)
	}
}

func NewRootCmd() *cobra.Command {
	options := clusterresourcequota.NewDefaultOptions()
	cmd := &cobra.Command{
		Use:     "clusterresourcequota",
		Short:   "ClusterResourceQuota controller",
		Version: version.Get().String(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.Parse(cmd.Flags()); err != nil {
				return err
			}
			ctx := signals.SetupSignalHandler()
			return clusterresourcequota.Run(ctx, options)
		},
	}
	config.RegisterFlags(cmd.Flags(), "", options)
	cmd.AddCommand(NewVersionCmd())
	return cmd
}

func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Println(version.Get().String())
			return nil
		},
	}
}
