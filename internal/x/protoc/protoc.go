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

package protoc

import (
	"fmt"
	"path/filepath"

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/tgrpc/prototool/internal/x/file"
	"github.com/tgrpc/prototool/internal/x/settings"
	"github.com/tgrpc/prototool/internal/x/text"
	"go.uber.org/zap"
)

// Downloader downloads and caches protobuf.
type Downloader interface {
	// Download protobuf.
	//
	// If already downloaded, this has no effect. This is thread-safe.
	// This will download to ${XDG_CACHE_HOME}/prototool/$(uname -s)/$(uname -m)
	// unless overridden by a DownloaderOption.
	// If ${XDG_CACHE_HOME} is not set, it defaults to ${HOME}/Library/Caches on
	// Darwin, and ${HOME}/.cache on Linux.
	// If ${HOME} is not set, an error will be returned.
	//
	// Returns the path to the downloaded protobuf artifacts.
	//
	// ProtocPath and WellKnownTypesIncludePath implicitly call this.
	Download() (string, error)

	// Get the path to protoc.
	//
	// If not downloaded, this downloads and caches protobuf. This is thread-safe.
	ProtocPath() (string, error)

	// Get the path to include for the well-known types.
	//
	// Inside this directory will be the subdirectories google/protobuf.
	//
	// If not downloaded, this downloads and caches protobuf. This is thread-safe.
	WellKnownTypesIncludePath() (string, error)

	// Delete any downloaded artifacts.
	//
	// This is not thread-safe and no calls to other functions can be reliably
	// made simultaneously.
	Delete() error
}

// DownloaderOption is an option for a new Downloader.
type DownloaderOption func(*downloader)

// DownloaderWithLogger returns a DownloaderOption that uses the given logger.
//
// The default is to use zap.NewNop().
func DownloaderWithLogger(logger *zap.Logger) DownloaderOption {
	return func(downloader *downloader) {
		downloader.logger = logger
	}
}

// DownloaderWithCachePath returns a DownloaderOption that uses the given cachePath.
//
//
// The default is ${XDG_CACHE_HOME}/prototool/$(uname -s)/$(uname -m).
func DownloaderWithCachePath(cachePath string) DownloaderOption {
	return func(downloader *downloader) {
		downloader.cachePath = cachePath
	}
}

// DownloaderWithProtocURL returns a DownloaderOption that uses the given protoc zip file URL.
//
// The default is https://github.com/google/protobuf/releases/download/vVERSION/protoc-VERSION-OS-ARCH.zip.
func DownloaderWithProtocURL(protocURL string) DownloaderOption {
	return func(downloader *downloader) {
		downloader.protocURL = protocURL
	}
}

// NewDownloader returns a new Downloader for the given config and DownloaderOptions.
func NewDownloader(config settings.Config, options ...DownloaderOption) Downloader {
	return newDownloader(config, options...)
}

// CompileResult is the result of a compile
type CompileResult struct {
	// The failures from all calls.
	Failures []*text.Failure
	// Will not be set if there are any failures.
	FileDescriptorSets []*descriptor.FileDescriptorSet
}

// Compiler compiles protobuf files.
type Compiler interface {
	// Compile the protobuf files with protoc.
	//
	// If there are compile failures, they will be returned in the slice
	// and there will be no error. The caller can determine if this is
	// an error case. If there is any other type of error, or some output
	// from protoc cannot be interpreted, an error will be returned.
	//
	// FileDescriptorSet will only be set if the CompilerWithFileDescriptorSet
	// option is used.
	Compile(...*file.ProtoSet) (*CompileResult, error)

	// Return the protoc commands that would be run on Compile.
	//
	// This will ignore the CompilerWithFileDescriptorSet option.
	ProtocCommands(...*file.ProtoSet) ([]string, error)
}

// CompilerOption is an option for a new Compiler.
type CompilerOption func(*compiler)

// CompilerWithLogger returns a CompilerOption that uses the given logger.
//
// The default is to use zap.NewNop().
func CompilerWithLogger(logger *zap.Logger) CompilerOption {
	return func(compiler *compiler) {
		compiler.logger = logger
	}
}

// CompilerWithCachePath returns a CompilerOption that uses the given cachePath.
//
//
// The default is ${XDG_CACHE_HOME}/prototool/$(uname -s)/$(uname -m).
func CompilerWithCachePath(cachePath string) CompilerOption {
	return func(compiler *compiler) {
		compiler.cachePath = cachePath
	}
}

// CompilerWithProtocURL returns a CompilerOption that uses the given protoc zip file URL.
//
// The default is https://github.com/google/protobuf/releases/download/vVERSION/protoc-VERSION-OS-ARCH.zip.
func CompilerWithProtocURL(protocURL string) CompilerOption {
	return func(compiler *compiler) {
		compiler.protocURL = protocURL
	}
}

// CompilerWithGen says to also generate the code.
func CompilerWithGen() CompilerOption {
	return func(compiler *compiler) {
		compiler.doGen = true
	}
}

// CompilerWithFileDescriptorSet says to also return the FileDescriptorSet.
func CompilerWithFileDescriptorSet() CompilerOption {
	return func(compiler *compiler) {
		compiler.doFileDescriptorSet = true
	}
}

// NewCompiler returns a new Compiler.
func NewCompiler(options ...CompilerOption) Compiler {
	return newCompiler(options...)
}

func checkAbs(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("expected absolute path but was %s", path)
	}
	return nil
}

func absClean(path string) (string, error) {
	if path == "" {
		return path, nil
	}
	var err error
	if !filepath.IsAbs(path) {
		path, err = filepath.Abs(path)
		if err != nil {
			return "", err
		}
	}
	return filepath.Clean(path), nil
}
