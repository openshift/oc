package main

import (
	"context"
	"net"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/operator-framework/operator-registry/pkg/api"
	health "github.com/operator-framework/operator-registry/pkg/api/grpc_health_v1"
	"github.com/operator-framework/operator-registry/pkg/lib/log"
	"github.com/operator-framework/operator-registry/pkg/server"
	"github.com/operator-framework/operator-registry/pkg/sqlite"
)

func main() {
	var rootCmd = &cobra.Command{
		Short: "configmap-server",
		Long:  `configmap-server reads a configmap and builds a sqlite database containing operator manifests and serves a grpc API to query it`,

		PreRunE: func(cmd *cobra.Command, args []string) error {
			if debug, _ := cmd.Flags().GetBool("debug"); debug {
				logrus.SetLevel(logrus.DebugLevel)
			}
			return nil
		},

		RunE: runCmdFunc,
	}

	rootCmd.Flags().Bool("debug", false, "enable debug logging")
	rootCmd.Flags().StringP("kubeconfig", "k", "", "absolute path to kubeconfig file")
	rootCmd.Flags().StringP("database", "d", "bundles.db", "name of db to output")
	rootCmd.Flags().StringP("configMapName", "c", "", "name of a configmap")
	rootCmd.Flags().StringP("configMapNamespace", "n", "", "namespace of a configmap")
	rootCmd.Flags().StringP("port", "p", "50051", "port number to serve on")
	rootCmd.Flags().StringP("termination-log", "t", "/dev/termination-log", "path to a container termination log file")
	if err := rootCmd.Flags().MarkHidden("debug"); err != nil {
		logrus.Panic(err.Error())
	}

	if err := rootCmd.Execute(); err != nil {
		logrus.Panic(err.Error())
	}
}

func runCmdFunc(cmd *cobra.Command, args []string) error {
	// Immediately set up termination log
	terminationLogPath, err := cmd.Flags().GetString("termination-log")
	if err != nil {
		return err
	}
	err = log.AddDefaultWriterHooks(terminationLogPath)
	if err != nil {
		return err
	}
	kubeconfig, err := cmd.Flags().GetString("kubeconfig")
	if err != nil {
		return err
	}
	port, err := cmd.Flags().GetString("port")
	if err != nil {
		return err
	}
	configMapName, err := cmd.Flags().GetString("configMapName")
	if err != nil {
		return err
	}
	configMapNamespace, err := cmd.Flags().GetString("configMapNamespace")
	if err != nil {
		return err
	}
	dbName, err := cmd.Flags().GetString("database")
	if err != nil {
		return err
	}
	logger := logrus.WithFields(logrus.Fields{"configMapName": configMapName, "configMapNamespace": configMapNamespace, "port": port})

	client := NewClientFromConfig(kubeconfig, logger.Logger)
	configMap, err := client.CoreV1().ConfigMaps(configMapNamespace).Get(configMapName, metav1.GetOptions{})
	if err != nil {
		logger.Fatalf("error getting configmap: %v", err)
	}

	sqlLoader, err := sqlite.NewSQLLiteLoader(dbName)
	if err != nil {
		return err
	}

	configMapPopulator := sqlite.NewSQLLoaderForConfigMap(sqlLoader, *configMap)
	if err := configMapPopulator.Populate(); err != nil {
		return err
	}

	store, err := sqlite.NewSQLLiteQuerier(dbName)
	if err != nil {
		logger.Fatalf("failed to load db: %v", err)
	}

	// sanity check that the db is available
	tables, err := store.ListTables(context.TODO())
	if err != nil {
		logger.Fatalf("couldn't list tables in db, incorrect config: %v", err)
	}
	if len(tables) == 0 {
		logger.Fatal("no tables found in db")
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		logger.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()

	api.RegisterRegistryServer(s, server.NewRegistryServer(store))
	health.RegisterHealthServer(s, server.NewHealthServer())
	reflection.Register(s)

	logger.Info("serving registry")
	if err := s.Serve(lis); err != nil {
		logger.Fatalf("failed to serve: %v", err)
	}
	return nil
}

// NewClient creates a kubernetes client or bails out on on failures.
func NewClientFromConfig(kubeconfig string, logger *logrus.Logger) kubernetes.Interface {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		logger.Infof("Loading kube client config from path %q", kubeconfig)
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		logger.Infof("Using in-cluster kube client config")
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		logger.Fatalf("Cannot load config for REST client: %v", err)
	}

	return kubernetes.NewForConfigOrDie(config)
}
