// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"go.opentelemetry.io/tools"
)

var (
	cfgFile        string
	moduleSetName  string
	versioningFile string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "releasing",
	Short: "Enables the release of Go modules with flexible versioning",
	Long: `A Golang release versioning and tagging tool that simplifies and
automates versioning for repos with multiple Go modules.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVarP(&versioningFile, "versioning-file", "v", "",
		"Path to versioning file that contains definitions of all module sets. "+
			fmt.Sprintf("If unspecified will default to (RepoRoot)/%v.%v",
				defaultVersionsConfigName, defaultVersionsConfigType))

	if versioningFile == "" {
		repoRoot, err := tools.FindRepoRoot()
		if err != nil {
			log.Fatalf("Could not find repo root: %v", err)
		}
		versioningFile = filepath.Join(repoRoot,
			fmt.Sprintf("%v.%v", defaultVersionsConfigName, defaultVersionsConfigType))
	}

	rootCmd.PersistentFlags().StringVarP(&moduleSetName, "module-set-name", "m", "",
		"Name of module set whose version is being changed. Must be listed in the module set versioning YAML.",
	)
	rootCmd.MarkPersistentFlagRequired("module-set-name")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".releasing" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".releasing")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
