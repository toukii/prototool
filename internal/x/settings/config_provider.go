// Copyright (c) 2018 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package settings

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/tgrpc/prototool/internal/x/strs"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type configProvider struct {
	logger                   *zap.Logger
	filePathToConfig         map[string]Config
	dirPathToFilePath        map[string]string
	dirPathToExcludePrefixes map[string][]string
	lock                     sync.RWMutex
}

func newConfigProvider(options ...ConfigProviderOption) *configProvider {
	configProvider := &configProvider{
		logger:                   zap.NewNop(),
		filePathToConfig:         make(map[string]Config),
		dirPathToFilePath:        make(map[string]string),
		dirPathToExcludePrefixes: make(map[string][]string),
	}
	for _, option := range options {
		option(configProvider)
	}
	return configProvider
}

func (c *configProvider) GetForDir(dirPath string) (Config, error) {
	filePath, err := c.GetFilePathForDir(dirPath)
	if err != nil {
		return Config{}, err
	}
	if filePath == "" {
		return Config{}, nil
	}
	return c.Get(filePath)
}

func (c *configProvider) GetFilePathForDir(dirPath string) (string, error) {
	if !filepath.IsAbs(dirPath) {
		return "", fmt.Errorf("%s is not an absolute path", dirPath)
	}
	dirPath = filepath.Clean(dirPath)
	c.lock.RLock()
	filePath, ok := c.dirPathToFilePath[dirPath]
	c.lock.RUnlock()
	if !ok {
		getFilePath, dirPaths := getFilePathForDir(dirPath)
		if getFilePath == "" {
			return "", nil
		}
		c.lock.Lock()
		filePath, ok = c.dirPathToFilePath[dirPath]
		if !ok {
			filePath = getFilePath
			for _, iDirPath := range dirPaths {
				if _, ok := c.dirPathToFilePath[iDirPath]; !ok {
					c.logger.Debug("adding dir to cache", zap.String("dirPath", iDirPath))
					c.dirPathToFilePath[iDirPath] = filePath
				}
			}
		}
		c.lock.Unlock()
	}
	if ok {
		c.logger.Debug("cache dir hit", zap.String("dirPath", dirPath))
	}
	return filePath, nil
}

func (c *configProvider) Get(filePath string) (Config, error) {
	if !filepath.IsAbs(filePath) {
		return Config{}, fmt.Errorf("%s is not an absolute path", filePath)
	}
	filePath = filepath.Clean(filePath)
	var err error
	c.lock.RLock()
	config, ok := c.filePathToConfig[filePath]
	c.lock.RUnlock()
	if !ok {
		c.lock.Lock()
		config, ok = c.filePathToConfig[filePath]
		if !ok {
			config, err = get(filePath)
			if err != nil {
				c.lock.Unlock()
				return Config{}, err
			}
			c.logger.Debug("adding file to cache", zap.String("filePath", filePath), zap.Any("config", config))
			c.filePathToConfig[filePath] = config
			if filepath.Base(filePath) == DefaultConfigFilename {
				dirPath := filepath.Dir(filePath)
				if _, ok := c.dirPathToFilePath[dirPath]; !ok {
					c.logger.Debug("adding dir to cache", zap.String("dirPath", dirPath))
					c.dirPathToFilePath[dirPath] = filePath
				}
			}
		}
		c.lock.Unlock()
	}
	if ok {
		c.logger.Debug("cache file hit", zap.String("filePath", filePath))
	}
	return config, nil
}

func (c *configProvider) GetExcludePrefixesForDir(dirPath string) ([]string, error) {
	if !filepath.IsAbs(dirPath) {
		return nil, fmt.Errorf("%s is not an absolute path", dirPath)
	}
	dirPath = filepath.Clean(dirPath)
	c.lock.RLock()
	excludePrefixes, ok := c.dirPathToExcludePrefixes[dirPath]
	var err error
	c.lock.RUnlock()
	if !ok {
		c.lock.Lock()
		excludePrefixes, ok = c.dirPathToExcludePrefixes[dirPath]
		if !ok {
			excludePrefixes, err = getExcludePrefixesForDir(dirPath)
			if err != nil {
				c.lock.Unlock()
				return nil, err
			}
			c.logger.Debug("adding exclude prefixes for dir", zap.String("dirPath", dirPath), zap.Any("excludePrefixes", excludePrefixes))
			c.dirPathToExcludePrefixes[dirPath] = excludePrefixes
		}
		c.lock.Unlock()
	}
	return excludePrefixes, nil
}

// getFilePathForDir tries to find a file named DefaultConfigFilename starting in the
// given directory, and going up a directory until hitting root.
//
// The directory must be an absolute path.
//
// If no such file is found, "" is returned.
// Also returns all the directories this Config applies to.
func getFilePathForDir(dirPath string) (string, []string) {
	var dirPaths []string
	for {
		dirPaths = append(dirPaths, dirPath)
		filePath := filepath.Join(dirPath, DefaultConfigFilename)
		if _, err := os.Stat(filePath); err == nil {
			return filePath, dirPaths
		}
		if dirPath == "/" {
			return "", dirPaths
		}
		dirPath = filepath.Dir(dirPath)
	}
}

// get reads the config at the given path.
//
// This is expected to be in YAML format.
func get(filePath string) (Config, error) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return Config{}, err
	}
	externalConfig := ExternalConfig{}
	if err := yaml.UnmarshalStrict(data, &externalConfig); err != nil {
		return Config{}, err
	}
	return externalConfigToConfig(externalConfig, filepath.Dir(filePath))
}

// externalConfigToConfig converts an ExternalConfig to a Config.
//
// This will return a valid Config, or an error.
func externalConfigToConfig(e ExternalConfig, dirPath string) (Config, error) {
	excludePrefixes, err := getExcludePrefixes(e.Excludes, e.NoDefaultExcludes, dirPath)
	if err != nil {
		return Config{}, err
	}
	includePaths := make([]string, 0, len(e.ProtocIncludes))
	for _, includePath := range strs.DedupeSortSlice(e.ProtocIncludes, nil) {
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(dirPath, includePath)
		}
		includePath = filepath.Clean(includePath)
		//if includePath != dirPath {
		includePaths = append(includePaths, includePath)
		//}
	}
	ignoreIDToFilePaths := make(map[string][]string)
	for id, protoFilePaths := range e.Lint.IgnoreIDToFiles {
		id = strings.ToUpper(id)
		for _, protoFilePath := range protoFilePaths {
			if !filepath.IsAbs(protoFilePath) {
				protoFilePath = filepath.Join(dirPath, protoFilePath)
			}
			protoFilePath = filepath.Clean(protoFilePath)
			if _, ok := ignoreIDToFilePaths[id]; !ok {
				ignoreIDToFilePaths[id] = make([]string, 0)
			}
			ignoreIDToFilePaths[id] = append(ignoreIDToFilePaths[id], protoFilePath)
		}
	}
	var indent string
	if len(e.Format.Indent) > 0 {
		indent, err = getIndent(e.Format.Indent)
		if err != nil {
			return Config{}, err
		}
	}

	genPlugins := make([]GenPlugin, len(e.Gen.Plugins))
	for i, plugin := range e.Gen.Plugins {
		genPluginType, err := ParseGenPluginType(plugin.Type)
		if err != nil {
			return Config{}, err
		}
		if plugin.Output == "" {
			return Config{}, fmt.Errorf("output path required for plugin %s", plugin.Name)
		}
		if filepath.IsAbs(plugin.Output) {
			return Config{}, fmt.Errorf("output path must be a relative path for plugin %s", plugin.Name)
		}
		path := ""
		if len(e.Gen.PluginOverrides) > 0 {
			if override, ok := e.Gen.PluginOverrides[plugin.Name]; ok && override != "" {
				path = override
			}
		}
		genPlugins[i] = GenPlugin{
			Name:  plugin.Name,
			Path:  path,
			Type:  genPluginType,
			Flags: plugin.Flags,
			OutputPath: OutputPath{
				RelPath: plugin.Output,
				AbsPath: filepath.Clean(filepath.Join(dirPath, plugin.Output)),
			},
		}
	}
	sort.Slice(genPlugins, func(i int, j int) bool { return genPlugins[i].Name < genPlugins[j].Name })

	config := Config{
		DirPath:         dirPath,
		ExcludePrefixes: excludePrefixes,
		Compile: CompileConfig{
			ProtobufVersion:       e.ProtocVersion,
			IncludePaths:          includePaths,
			IncludeWellKnownTypes: e.ProtocIncludeWKT,
			AllowUnusedImports:    e.AllowUnusedImports,
		},
		Lint: LintConfig{
			IDs:                 strs.DedupeSortSlice(e.Lint.IDs, strings.ToUpper),
			Group:               strings.ToLower(e.Lint.Group),
			IncludeIDs:          strs.DedupeSortSlice(e.Lint.IncludeIDs, strings.ToUpper),
			ExcludeIDs:          strs.DedupeSortSlice(e.Lint.ExcludeIDs, strings.ToUpper),
			IgnoreIDToFilePaths: ignoreIDToFilePaths,
		},
		Format: FormatConfig{
			Indent:           indent,
			RPCUseSemicolons: e.Format.RPCUseSemicolons,
			TrimNewline:      e.Format.TrimNewline,
		},
		Gen: GenConfig{
			GoPluginOptions: GenGoPluginOptions{
				ImportPath:         e.Gen.GoOptions.ImportPath,
				NoDefaultModifiers: e.Gen.GoOptions.NoDefaultModifiers,
				ExtraModifiers:     e.Gen.GoOptions.ExtraModifiers,
			},
			Plugins: genPlugins,
		},
	}

	for _, genPlugin := range config.Gen.Plugins {
		// TODO: technically protoc-gen-protoc-gen-foo is a valid
		// plugin binary with name protoc-gen-foo, but do we want
		// to error if protoc-gen- is a prefix of a name?
		// I think this will be a common enough mistake that we
		// can remove this later. Or, do we want names to include
		// the protoc-gen- part?
		if strings.HasPrefix(genPlugin.Name, "protoc-gen-") {
			return Config{}, fmt.Errorf("plugin name provided was %s, do not include the protoc-gen- prefix", genPlugin.Name)
		}
		if _, ok := _genPluginTypeToString[genPlugin.Type]; !ok {
			return Config{}, fmt.Errorf("unknown GenPluginType: %v", genPlugin.Type)
		}
		if (genPlugin.Type.IsGo() || genPlugin.Type.IsGogo()) && config.Gen.GoPluginOptions.ImportPath == "" {
			return Config{}, fmt.Errorf("go plugin %s specified but no import path provided", genPlugin.Name)
		}
	}

	if len(config.Lint.IDs) > 0 && (len(config.Lint.Group) > 0 || len(config.Lint.IncludeIDs) > 0 || len(config.Lint.ExcludeIDs) > 0) {
		return Config{}, fmt.Errorf("config was %v but can only specify either linters, or lint_group/lint_include/lint_exclude", e)
	}
	if intersection := strs.IntersectionSlice(config.Lint.IncludeIDs, config.Lint.ExcludeIDs); len(intersection) > 0 {
		return Config{}, fmt.Errorf("config had intersection of %v between lint_include and lint_exclude", intersection)
	}
	return config, nil
}

func getExcludePrefixesForDir(dirPath string) ([]string, error) {
	filePath := filepath.Join(dirPath, DefaultConfigFilename)
	if _, err := os.Stat(filePath); err != nil {
		excludePrefixes := make([]string, 0, len(DefaultExcludePrefixes))
		for _, defaultExcludePrefix := range DefaultExcludePrefixes {
			excludePrefixes = append(excludePrefixes, filepath.Join(dirPath, defaultExcludePrefix))
		}
		return excludePrefixes, nil
	}
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	s := struct {
		ExcludePaths          []string `json:"excludes,omitempty" yaml:"excludes,omitempty"`
		NoDefaultExcludePaths bool     `json:"no_default_excludes,omitempty" yaml:"no_default_excludes,omitempty"`
	}{}
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return getExcludePrefixes(s.ExcludePaths, s.NoDefaultExcludePaths, dirPath)
}

func getExcludePrefixes(excludes []string, noDefaultExcludes bool, dirPath string) ([]string, error) {
	if !noDefaultExcludes {
		excludes = append(DefaultExcludePrefixes, excludes...)
	}
	excludePrefixes := make([]string, 0, len(excludes))
	for _, excludePrefix := range strs.DedupeSortSlice(excludes, nil) {
		if !filepath.IsAbs(excludePrefix) {
			excludePrefix = filepath.Join(dirPath, excludePrefix)
		}
		excludePrefix = filepath.Clean(excludePrefix)
		if excludePrefix == dirPath {
			return nil, fmt.Errorf("cannot exclude directory of config file: %s", dirPath)
		}
		if !strings.HasPrefix(excludePrefix, dirPath) {
			return nil, fmt.Errorf("cannot exclude directory outside of config file directory %s: %s", dirPath, excludePrefix)
		}
		excludePrefixes = append(excludePrefixes, excludePrefix)
	}
	return excludePrefixes, nil
}

func getIndent(spec string) (string, error) {
	if len(spec) < 2 {
		return "", invalidIndentSpecErrorf(spec)
	}
	base := ""
	switch spec[len(spec)-1] {
	case 't':
		base = "\t"
	case 's':
		base = " "
	default:
		return "", invalidIndentSpecErrorf(spec)
	}
	count, err := strconv.Atoi(spec[0 : len(spec)-1])
	if err != nil {
		return "", invalidIndentSpecErrorf(spec)
	}
	if count < 1 {
		return "", invalidIndentSpecErrorf(spec)
	}
	return strings.Repeat(base, count), nil
}

func invalidIndentSpecErrorf(spec string) error {
	return fmt.Errorf("invalid indent spec, must be Ns or Nt where N >= 1: %s", spec)
}
