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
	"os/exec"

	"github.com/spf13/cobra"

	tools "go.opentelemetry.io/tools"
)

var (
	commitHash          string
	deleteModuleSetTags bool
)

// tagCmd represents the tag command
var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Applies Git tags to specified commit",
	Long: `Tagging script to add Git tags to a specified commit hash created by prerelease script:
- Creates new Git tags for all modules being updated.
- If tagging fails in the middle of the script, the recently created tags will be deleted.`,
	PreRun: func(cmd *cobra.Command, args []string) {
		if deleteModuleSetTags {
			// do not require commit-hash flag if deleting module set tags
			cmd.Flags().SetAnnotation("commit-hash", cobra.BashCompOneRequiredFlag, []string{"false"})
		}
	},
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("tag called")

		coreRepoRoot, err := tools.FindRepoRoot()
		if err != nil {
			log.Fatalf("unable to find repo root: %v", err)
		}

		fmt.Println("Changing to root directory...")
		os.Chdir(coreRepoRoot)

		// get new version and mod tags to update
		newVersion, _, newModTagNames, err := tools.VersionsAndModulesToUpdate(versioningFile, moduleSet, coreRepoRoot)
		if err != nil {
			log.Fatalf("unable to get modules to update: %v", err)
		}

		// if delete-module-set-tags was specified, then delete all newModTagNames
		// whose versions match the one in the versioning file
		if deleteModuleSetTags {
			modFullTagsToDelete := tools.CombineModuleTagNamesAndVersion(newModTagNames, newVersion)

			if err := deleteTags(modFullTagsToDelete); err != nil {
				log.Fatalf("unable to delete module tags: %v", err)
			}

			fmt.Println("Successfully deleted module tags")
			os.Exit(0)
		}

		if err := tagAllModules(newVersion, newModTagNames, commitHash); err != nil {
			log.Fatalf("unable to tag modules: %v", err)
		}
	},
}

func init() {
	// Plain log output, no timestamps.
	log.SetFlags(0)

	rootCmd.AddCommand(tagCmd)

	tagCmd.Flags().StringVarP(&commitHash, "commit-hash", "c", "",
		"Git commit hash to tag.",
	)
	tagCmd.MarkFlagRequired("commit-hash")

	tagCmd.Flags().BoolVarP(&deleteModuleSetTags, "delete-module-set-tags", "d", false,
		"Specify this flag to delete all module tags associated with the version listed for the module set in the versioning file. Should only be used to undo recent tagging mistakes.",
	)
}

// deleteTags removes the tags created for a certain version. This func is called to remove newly
// created tags if the new module tagging fails.
func deleteTags(modFullTags []string) error {
	for _, modFullTag := range modFullTags {
		fmt.Printf("Deleting tag %v\n", modFullTag)
		cmd := exec.Command("git", "tag", "-d", modFullTag)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("could not delete tag %v:\n%v (%v)", modFullTag, string(output), err)
		}
	}
	return nil
}

func tagAllModules(version string, modTagNames []tools.ModuleTagName, commitHash string) error {
	modFullTags := tools.CombineModuleTagNamesAndVersion(modTagNames, version)

	var addedFullTags []string

	fmt.Printf("Tagging commit %v:\n", commitHash)

	for _, newFullTag := range modFullTags {
		fmt.Printf("%v\n", newFullTag)

		cmd := exec.Command("git", "tag", "-a", newFullTag, "-s", "-m", "Version "+newFullTag, commitHash)
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Println("error creating a tag, removing all newly created tags...")

			// remove newly created tags to prevent inconsistencies
			if delTagsErr := deleteTags(addedFullTags); delTagsErr != nil {
				return fmt.Errorf("git tag failed for %v:\n%v (%v).\nCould not remove all tags: %v",
					newFullTag, string(output), err, delTagsErr,
				)
			}

			return fmt.Errorf("git tag failed for %v:\n%v (%v)", newFullTag, string(output), err)
		}

		addedFullTags = append(addedFullTags, newFullTag)
	}

	return nil
}
