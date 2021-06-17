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
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	tools "go.opentelemetry.io/tools"
)

var (
	fromExistingBranch string
	skipMake           bool
)

// prereleaseCmd represents the prerelease command
var prereleaseCmd = &cobra.Command{
	Use:   "prerelease",
	Short: "Prepares files for new version release",
	Long: `Updates version numbers and commits to a new branch for release:
- Checks that Git tags do not already exist for the new module set version.
- Checks that the working tree is clean.
- Switches to a new branch called pre_release_<module set name>_<new version>.
- Updates module versions in all go.mod files.
- 'make lint' and 'make ci' are called
- Adds and commits changes to Git`,
	Run: func(cmd *cobra.Command, args []string) {

		coreRepoRoot, err := tools.FindRepoRoot()
		if err != nil {
			log.Fatalf("unable to find repo root: %v", err)
		}

		fmt.Println("Changing to root directory...")
		os.Chdir(coreRepoRoot)

		// get new version and mod tags to update
		newVersion, newModPaths, newModTagNames, err := tools.VersionsAndModulesToUpdate(versioningFile, moduleSet, coreRepoRoot)
		if err != nil {
			log.Fatalf("unable to get modules to update: %v", err)
		}

		fmt.Println("Checking for tags", newModTagNames)

		if err = verifyGitTagsDoNotAlreadyExist(newVersion, newModTagNames, coreRepoRoot); err != nil {
			log.Fatalf("verifyGitTagsDoNotAlreadyExist failed: %v", err)
		}

		if err = verifyWorkingTreeClean(); err != nil {
			log.Fatalf("verifyWorkingTreeClean failed: %v", err)
		}

		if err = createPrereleaseBranch(moduleSet, newVersion, fromExistingBranch); err != nil {
			log.Fatalf("createPrereleaseBranch failed: %v", err)
		}

		modPathMap, err := tools.BuildModulePathMap(versioningFile, coreRepoRoot)

		// TODO: what to do with version.go and references to otel.Version()
		if err = updateVersionGo(); err != nil {
			log.Fatalf("updateVersionGo failed: %v", err)
		}

		if err = updateAllGoModFiles(newVersion, newModPaths, modPathMap); err != nil {
			log.Fatalf("updateAllGoModFiles failed: %v", err)
		}

		if skipMake {
			fmt.Println("Skipping 'make lint'...")
		} else {
			if err = runMakeLint(); err != nil {
				log.Fatalf("runMakeLint failed: %v", err)
			}
		}

		if err = commitChanges(newVersion, skipMake); err != nil {
			log.Fatalf("commitChanges failed: %v", err)
		}

		fmt.Println("\nPrerelease finished successfully. Now run the following to verify the changes:")
		fmt.Println("\ngit diff main\n")
		fmt.Println("Then, push the changes to upstream.")
	},
}

func init() {
	// Plain log output, no timestamps.
	log.SetFlags(0)

	rootCmd.AddCommand(prereleaseCmd)

	prereleaseCmd.Flags().StringVarP(&fromExistingBranch, "from-existing-branch", "f", "",
		"Name of existing branch from which to base the pre-release branch. If unspecified, defaults to current branch.",
	)

	prereleaseCmd.Flags().BoolVarP(&skipMake, "skip-make", "s", false,
		"Specify this flag to skip the 'make lint' and 'make ci' steps. "+
			"To be used for debugging purposes. Should not be skipped during actual release.",
	)

	if fromExistingBranch == "" {
		// get current branch
		cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		output, err := cmd.Output()
		if err != nil {
			log.Fatalf("could not get current branch: %v", err)
		}

		fromExistingBranch = strings.TrimSpace(string(output))
	}
}

// verifyGitTagsDoNotAlreadyExist checks if Git tags have already been created that match the specific module tag name
// and version number for the modules being updated. If the tag already exists, an error is returned.
func verifyGitTagsDoNotAlreadyExist(version string, modTagNames []tools.ModuleTagName, coreRepoRoot string) error {
	modFullTags := tools.CombineModuleTagNamesAndVersion(modTagNames, version)

	for _, newFullTag := range modFullTags {
		cmd := exec.Command("git", "tag", "-l", newFullTag)
		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("could not execute git tag -l %v: %v", newFullTag, err)
		}

		outputTag := strings.TrimSpace(string(output))
		if outputTag == newFullTag {
			return fmt.Errorf("git tag already exists for %v", newFullTag)
		}
	}

	return nil
}

// verifyWorkingTreeClean checks if the working tree is clean (i.e. running 'git diff --exit-code' gives exit code 0).
// If the working tree is not clean, the git diff output is printed, and an error is returned.
func verifyWorkingTreeClean() error {
	cmd := exec.Command("git", "diff", "--exit-code")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("working tree is not clean, can't proceed with the release process:\n\n%v",
			string(output),
		)
	}

	return nil
}

func createPrereleaseBranch(modSet, newVersion, fromExistingBranch string) error {
	branchNameElements := []string{"pre_release", modSet, newVersion}
	branchName := strings.Join(branchNameElements, "_")
	fmt.Printf("git checkout -b %v %v\n", branchName, fromExistingBranch)
	cmd := exec.Command("git", "checkout", "-b", branchName, fromExistingBranch)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("could not create new branch %v: %v (%v)", branchName, string(output), err)
	}

	return nil
}

// TODO: figure out what to do with version.go file
func updateVersionGo() error {
	return nil
}

func filePathToRegex(fpath string) string {
	replacedSlashes := strings.Replace(fpath, string(filepath.Separator), `\/`, -1)
	replacedPeriods := strings.Replace(replacedSlashes, ".", `\.`, -1)
	return replacedPeriods
}

// updateGoModVersions reads the fromFile (a go.mod file), replaces versions
// for all specified modules in newModPaths, and writes the new go.mod to the toFile file.
func updateGoModVersions(newVersion string, newModPaths []tools.ModulePath, modFilePath tools.ModuleFilePath) error {
	newGoModFile, err := ioutil.ReadFile(string(modFilePath))
	if err != nil {
		panic(err)
	}

	for _, modPath := range newModPaths {
		oldVersionRegex := filePathToRegex(string(modPath)) + ` v[0-9]*\.[0-9]*\.[0-9]`
		r, err := regexp.Compile(oldVersionRegex)
		if err != nil {
			return fmt.Errorf("error compiling regex: %v", err)
		}

		newModVersionString := string(modPath) + " " + newVersion

		newGoModFile = r.ReplaceAll(newGoModFile, []byte(newModVersionString))
	}

	// once all module versions have been updated, overwrite the go.mod file
	ioutil.WriteFile(string(modFilePath), newGoModFile, 0644)

	return nil
}

// updateAllGoModFiles updates ALL modules' requires sections to use the newVersion number
// for the modules given in newModPaths.
func updateAllGoModFiles(newVersion string, newModPaths []tools.ModulePath, modPathMap tools.ModulePathMap) error {
	fmt.Println("Updating all module versions in go.mod files...")
	for _, modFilePath := range modPathMap {
		if err := updateGoModVersions(newVersion, newModPaths, modFilePath); err != nil {
			return fmt.Errorf("could not update module versions in file %v: %v", modFilePath, err)
		}
	}
	return nil
}

// runMakeLint runs 'make lint' to automatically update go.sum files.
func runMakeLint() error {
	fmt.Println("Updating go.sum with 'make lint'...")

	cmd := exec.Command("make", "lint")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("'make lint' failed: %v (%v)", string(output), err)
	}

	return nil
}

func commitChanges(newVersion string, skipMake bool) error {
	commitMessage := "Prepare for releasing " + newVersion

	// add changes to git
	cmd := exec.Command("git", "add", ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("'git add .' failed: %v (%v)", string(output), err)
	}

	// make ci
	if skipMake {
		fmt.Println("Skipping 'make ci'...")
	} else {
		fmt.Println("Running 'make ci'...")
		cmd = exec.Command("make", "ci")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("'make ci' failed: %v (%v)", string(output), err)
		}
	}

	// commit changes to git
	fmt.Printf("Commit changes to git with message '%v'...\n", commitMessage)
	cmd = exec.Command("git", "commit", "-m", commitMessage)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit failed: %v (%v)", string(output), err)
	}

	cmd = exec.Command("git", "log", `--pretty=format:"%h"`, "-1")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("WARNING: could not automatically get last commit hash.")
	}

	fmt.Println("Commit successful. Hash of commit:")
	os.Stdout.Write(output)

	return nil
}
