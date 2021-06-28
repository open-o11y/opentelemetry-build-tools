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

// Package tools provides helper functions used in the "releasing" Cobra application.
package tools // import "go.opentelemetry.io/tools"

import (
	"errors"
	"fmt"
	"github.com/spf13/viper"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

const (
	repoRootTag = ModuleTagName("repoRootTag")
)

type ModuleVersioning struct {
	ModSetMap ModuleSetMap
	ModPathMap ModulePathMap
	ModInfoMap ModuleInfoMap
}

func NewModuleVersioningInfo(versioningFilename string, repoRoot string) (ModuleVersioning, error) {
	var newBaseVersionStruct ModuleVersioning

	vCfg, _ := readVersioningFile(versioningFilename)

	modSetMap, err := vCfg.buildModuleSetsMap()
	if err != nil {
		return ModuleVersioning{}, fmt.Errorf("Error building module set map for NewModuleVersioningInfo: %v", err)
	}
	newBaseVersionStruct.ModSetMap = modSetMap

	modInfoMap, err := vCfg.buildModuleMap()
	if err != nil {
		return ModuleVersioning{}, fmt.Errorf("Error building module info map for NewModuleVersioningInfo: %v", err)
	}
	newBaseVersionStruct.ModInfoMap = modInfoMap

	modPathMap, err := vCfg.BuildModulePathMap(repoRoot)
	if err != nil {
		return ModuleVersioning{}, fmt.Errorf("Error building module path map for NewModuleVersioningInfo: %v", err)
	}
	newBaseVersionStruct.ModPathMap = modPathMap

	return newBaseVersionStruct, nil
}

// versionConfig is needed to parse the versions.yaml file with viper.
type versionConfig struct {
	ModuleSets      ModuleSetMap `mapstructure:"module-sets"`
	ExcludedModules []ModulePath `mapstructure:"excluded-modules"`
}

// excludedModules functions as a set containing all module paths that are excluded
// from versioning.
type excludedModulesSet map[ModulePath]struct{}

// ModuleSetMap maps the name of a module set to a ModuleSet struct.
type ModuleSetMap map[string]ModuleSet

// ModuleSet holds the version that the specified modules within the set will have.
type ModuleSet struct {
	Version	string       `mapstructure:"version"`
	Modules	[]ModulePath `mapstructure:"modules"`
}

// ModulePath holds the module import path, such as "go.opentelemetry.io/otel".
type ModulePath string

// ModuleInfoMap is a mapping from a module's import path to its ModuleInfo struct.
type ModuleInfoMap map[ModulePath]ModuleInfo

// ModuleInfo is a reverse of the ModuleSetMap, to allow for quick lookup from module
// path to its set and version.
type ModuleInfo struct {
	ModuleSetName string
	Version       string
}

// ModuleFilePath holds the file path to the go.mod file within the repo,
// including the base file name ("go.mod").
type ModuleFilePath string

// ModulePathMap is a mapping from a module's import path to its file path.
type ModulePathMap map[ModulePath]ModuleFilePath

// ModuleTagName is the simple file path to the directory of a go.mod file used for Git tagging.
// For example, the opentelemetry-go/sdk/metric/go.mod file will have a ModuleTagName "sdk/metric".
type ModuleTagName string

// readVersioningFile reads in a versioning file (typically given as versions.yaml) and returns
// a versionConfig struct.
func readVersioningFile(versioningFilename string) (versionConfig, error) {
	viper.AddConfigPath(filepath.Dir(versioningFilename))
	fileExt := filepath.Ext(versioningFilename)
	fileBaseWithoutExt := strings.TrimSuffix(filepath.Base(versioningFilename), fileExt)
	viper.SetConfigName(fileBaseWithoutExt)
	viper.SetConfigType(strings.TrimPrefix(fileExt, "."))

	var versionCfg versionConfig

	if err := viper.ReadInConfig(); err != nil {
		return versionConfig{}, fmt.Errorf("error reading versionsConfig file: %s", err)
	}

	if err := viper.Unmarshal(&versionCfg); err != nil {
		return versionConfig{}, fmt.Errorf("unable to unmarshal versionsConfig: %s", err)
	}

	return versionCfg, nil
}

// buildModuleSetsMap creates a map with module set names as keys and ModuleSet structs as values.
func (versionCfg versionConfig) buildModuleSetsMap() (ModuleSetMap, error) {
	return versionCfg.ModuleSets, nil
}

// BuildModuleMap creates a map with module paths as keys and their moduleInfo as values
// by creating and "reversing" a ModuleSetsMap.
func (versionCfg versionConfig) buildModuleMap() (ModuleInfoMap, error) {
	modMap := make(ModuleInfoMap)
	var modPath ModulePath

	for setName, moduleSet := range versionCfg.ModuleSets {
		for _, modPath = range moduleSet.Modules {
			// Check if module has already been added to the map
			if _, exists := modMap[modPath]; exists {
				return nil, fmt.Errorf("Module %v exists more than once. Exists in sets %v and %v.",
					modPath, modMap[modPath].ModuleSetName, setName)
			}

			// Check if module is in excluded modules section
			if versionCfg.shouldExcludeModule(modPath) {
				return nil, fmt.Errorf("Module %v is an excluded module and should not be versioned.", modPath)
			}
			modMap[modPath] = ModuleInfo{setName, moduleSet.Version}
		}
	}

	return modMap, nil
}

func (versionCfg versionConfig) shouldExcludeModule(modPath ModulePath) bool {
	excludedModules := versionCfg.getExcludedModules()
	_, exists := excludedModules[modPath]

	return exists
}

// getExcludedModules returns a .
func (versionCfg versionConfig) getExcludedModules() excludedModulesSet {
	excludedModules := make(excludedModulesSet)
	// add all excluded modules to the excludedModulesSet
	for _, mod := range versionCfg.ExcludedModules {
		excludedModules[mod] = struct{}{}
	}

	return excludedModules
}

// BuildModulePathMap creates a map with module paths as keys and go.mod file paths as values.
func (versionCfg versionConfig) BuildModulePathMap(root string) (ModulePathMap, error) {
	modPathMap := make(ModulePathMap)

	findGoMod := func(filePath string, info fs.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Warning: file could not be read during filepath.Walk(): %v", err)
			return nil
		}
		if filepath.Base(filePath) == "go.mod" {
			// read go.mod file into mod []byte
			mod, err := ioutil.ReadFile(filePath)
			if err != nil {
				return err
			}

			// read path of module from go.mod file
			modPathString := modfile.ModulePath(mod)

			// convert modPath, filePath string to modulePath and moduleFilePath
			modPath := ModulePath(modPathString)
			modFilePath := ModuleFilePath(filePath)

			excludedModules := versionCfg.getExcludedModules()
			if _, shouldExclude := excludedModules[ModulePath(modPath)]; !shouldExclude {
				modPathMap[modPath] = modFilePath
			}
		}
		return nil
	}

	if err := filepath.Walk(string(root), findGoMod); err != nil {
		return nil, err
	}

	return modPathMap, nil
}

// GetModuleSet fetches the ModuleSet info for a module set with input name
func (modVersioning ModuleVersioning) GetModuleSet(modSetName string) (ModuleSet, error) {
	modSet, exists := modVersioning.ModSetMap[modSetName]
	if !exists {
		return ModuleSet{}, fmt.Errorf("could not find module set %v in versioning file", modSetName)
	}

	return modSet, nil
}

// CombineModuleTagNamesAndVersion combines a slice of ModuleTagNames with the version number and returns
// the new full module tags.
func CombineModuleTagNamesAndVersion(modTagNames []ModuleTagName, version string) []string {
	var modFullTags []string
	for _, modTagName := range modTagNames {
		var newFullTag string
		if modTagName == repoRootTag {
			newFullTag = version
		} else {
			newFullTag = string(modTagName) + "/" + version
		}
		modFullTags = append(modFullTags, newFullTag)
	}

	return modFullTags
}

func ModulePathsToTagNames(modPaths []ModulePath, modPathMap ModulePathMap, repoRoot string) ([]ModuleTagName, error) {
	modFilePaths, err := modulePathsToFilePaths(modPaths, modPathMap)
	if err != nil {
		return  nil, fmt.Errorf("could not convert module paths to file paths: %v", err)
	}

	modTagNames, err := moduleFilePathsToTagNames(modFilePaths, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("could not convert module file paths to tag names: %v", err)
	}

	return modTagNames, nil
}

// modulePathsToFilePaths returns a list of absolute file paths from a list of module's import paths.
func modulePathsToFilePaths(modPaths []ModulePath, modPathMap ModulePathMap) ([]ModuleFilePath, error) {
	var modFilePaths []ModuleFilePath

	for _, modPath := range modPaths {
		modFilePaths = append(modFilePaths, modPathMap[modPath])
	}

	return modFilePaths, nil
}

// moduleFilePathToTagName returns the module tag names of an input ModuleFilePath
// by removing the repoRoot prefix from the ModuleFilePath.
func moduleFilePathToTagName(modFilePath ModuleFilePath, repoRoot string) (ModuleTagName, error) {
	modTagNameWithGoMod := strings.TrimPrefix(string(modFilePath), repoRoot+"/")
	modTagName := strings.TrimSuffix(modTagNameWithGoMod, "/go.mod")

	if modTagName == string(modFilePath) {
		return "", fmt.Errorf("modFilePath %v could not be found in the repo root %v", modFilePath, repoRoot)
	}

	// if the modTagName is equal to go.mod, it is the root repo
	if modTagName == "go.mod" {
		return repoRootTag, nil
	}

	return ModuleTagName(modTagName), nil
}

// moduleFilePathsToTagNames returns a list of module tag names from the input full module file paths
// by removing the repoRoot prefix from each ModuleFilePath.
func moduleFilePathsToTagNames(modFilePaths []ModuleFilePath, repoRoot string) ([]ModuleTagName, error) {
	var modNames []ModuleTagName

	for _, modFilePath := range modFilePaths {
		modTagName, err := moduleFilePathToTagName(modFilePath, repoRoot)
		if err != nil {
			return nil, fmt.Errorf("could not convert module file path to tag name: %v", err)
		}
		modNames = append(modNames, modTagName)
	}

	return modNames, nil
}

// IsStableVersion returns true if modSet.Version is stable (i.e. version major greater than
// or equal to v1), else false.
func IsStableVersion(v string) bool {
	return semver.Compare(semver.Major(v), "v1") >= 0
}

// FindRepoRoot retrieves the root of the repository containing the current working directory.
// Beginning at the current working directory (dir), the algorithm checks if joining the ".git"
// suffix, such as "dir.get", is a valid file. Otherwise, it will continue checking the dir's
// parent directory until it reaches the repo root or returns an error if it cannot be found.
func FindRepoRoot() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := start
	for {
		_, err := os.Stat(filepath.Join(dir, ".git"))
		if errors.Is(err, os.ErrNotExist) {
			dir = filepath.Dir(dir)
			// From https://golang.org/pkg/path/filepath/#Dir:
			// The returned path does not end in a separator unless it is the root directory.
			if strings.HasSuffix(dir, string(filepath.Separator)) {
				return "", fmt.Errorf("unable to find git repository enclosing working dir %s", start)
			}
			continue
		}

		if err != nil {
			return "", err
		}

		return dir, nil
	}
}

func ChangeToRepoRoot() (string, error) {
	repoRoot, err := FindRepoRoot()
	if err != nil {
		return "", fmt.Errorf("unable to change to repo root: %v", err)
	}

	fmt.Println("Changing to root directory...")
	os.Chdir(repoRoot)

	return repoRoot, nil
}
