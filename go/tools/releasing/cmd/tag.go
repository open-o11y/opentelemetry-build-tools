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
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"go.opentelemetry.io/tools"
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
		fmt.Println("Using versioning file", versioningFile)

		repoRoot, err := tools.ChangeToRepoRoot()
		if err != nil {
			log.Fatalf("unable to change to repo root: %v", err)
		}

		t, err := newTagger(versioningFile, moduleSet, repoRoot, commitHash)
		if err != nil {
			log.Fatalf("Error creating new tagger struct: %v", err)
		}

		// if delete-module-set-tags is specified, then delete all newModTagNames
		// whose versions match the one in the versioning file. Otherwise, tag all
		// modules in the given set.
		if deleteModuleSetTags {
			if err := t.deleteModuleSetTags(); err != nil {
				log.Fatalf("Error deleting tags for the specified module set: %v", err)
			}

			fmt.Println("Successfully deleted module tags")
		} else {
			if err := t.tagAllModules(); err != nil {
				log.Fatalf("unable to tag modules: %v", err)
			}
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

type tagger struct {
	prerelease
	commitHash string
}

func newTagger(versioningFilename, modSetToUpdate, repoRoot, hash string) (tagger, error) {
	prereleaseStruct, err := newPrerelease(versioningFilename, modSetToUpdate, repoRoot)
	if err != nil {
		return tagger{}, fmt.Errorf("error creating prerelease struct: %v", err)
	}

	fullCommitHash, err := getFullCommitHash(hash)
	if err != nil {
		return tagger{}, fmt.Errorf("could not get full commit hash of given hash %v: %v", hash, err)
	}

	return tagger{
		prerelease: prereleaseStruct,
		commitHash: fullCommitHash,
	}, nil
}

func getFullCommitHash(hash string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--quiet", "--verify", hash)

	// output stores the complete SHA1 of the commit hash
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("could not retrieve commit hash %v: %v", hash, err)
	}

	SHA := strings.TrimSpace(string(output))

	cmd = exec.Command("git", "merge-base", SHA, "HEAD")
	// output should match SHA
	output, err = cmd.Output()
	if err != nil {
		return "", fmt.Errorf("command 'git merge-base %v HEAD' failed: %v", SHA, err)
	}
	if strings.TrimSpace(string(output)) != SHA {
		return "", fmt.Errorf("commit %v (complete SHA: %v) not found on this branch "+
			"or not the most recent commit", hash, SHA)
	}

	return SHA, nil
}

func (t tagger) deleteModuleSetTags() error {
	modFullTagsToDelete := tools.CombineModuleTagNamesAndVersion(t.modTagNames, t.newVersion)

	if err := t.deleteTags(modFullTagsToDelete); err != nil {
		return fmt.Errorf("unable to delete module tags: %v", err)
	}

	return nil
}

// deleteTags removes the tags created for a certain version. This func is called to remove newly
// created tags if the new module tagging fails.
func (t tagger) deleteTags(modFullTags []string) error {
	for _, modFullTag := range modFullTags {
		fmt.Printf("Deleting tag %v\n", modFullTag)
		cmd := exec.Command("git", "tag", "-d", modFullTag)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("could not delete tag %v:\n%v (%v)", modFullTag, string(output), err)
		}
	}
	return nil
}

func (t tagger) tagAllModules() error {
	modFullTags := tools.CombineModuleTagNamesAndVersion(t.modTagNames, t.newVersion)

	var addedFullTags []string

	fmt.Printf("Tagging commit %v:\n", t.commitHash)

	for _, newFullTag := range modFullTags {
		fmt.Printf("%v\n", newFullTag)

		cmd := exec.Command("git", "tag", "-a", newFullTag, "-s", "-m", "Version "+newFullTag, t.commitHash)
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Println("error creating a tag, removing all newly created tags...")

			// remove newly created tags to prevent inconsistencies
			if delTagsErr := t.deleteTags(addedFullTags); delTagsErr != nil {
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
