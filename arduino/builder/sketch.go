// This file is part of arduino-cli.
//
// Copyright 2020 ARDUINO SA (http://www.arduino.cc/)
//
// This software is released under the GNU General Public License version 3,
// which covers the main part of arduino-cli.
// The terms of this license can be found at:
// https://www.gnu.org/licenses/gpl-3.0.en.html
//
// You can be released from the requirements of the above licenses by purchasing
// a commercial license. Buying such a license is mandatory if you want to
// modify or otherwise use the software for commercial activities involving the
// Arduino software without disclosing the source code of your own applications.
// To purchase a commercial license, send an email to license@arduino.cc.

package builder

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/arduino/arduino-cli/arduino/builder/compilation"
	"github.com/arduino/arduino-cli/arduino/builder/cpp"
	"github.com/arduino/arduino-cli/arduino/builder/logger"
	"github.com/arduino/arduino-cli/arduino/builder/progress"
	"github.com/arduino/arduino-cli/arduino/builder/utils"
	"github.com/arduino/arduino-cli/arduino/sketch"
	"github.com/arduino/arduino-cli/i18n"
	f "github.com/arduino/arduino-cli/internal/algorithms"
	rpc "github.com/arduino/arduino-cli/rpc/cc/arduino/cli/commands/v1"
	"github.com/arduino/go-paths-helper"
	"github.com/arduino/go-properties-orderedmap"

	"github.com/pkg/errors"
)

var (
	includesArduinoH = regexp.MustCompile(`(?m)^\s*#\s*include\s*[<\"]Arduino\.h[>\"]`)
	tr               = i18n.Tr
)

// Sketch fixdoc
func (b *Builder) Sketch() *sketch.Sketch {
	return b.sketch
}

// PrepareSketchBuildPath copies the sketch source files in the build path.
// The .ino files are merged together to create a .cpp file (by the way, the
// .cpp file still needs to be Arduino-preprocessed to compile).
func (b *Builder) PrepareSketchBuildPath(sourceOverrides map[string]string, buildPath *paths.Path) (int, error) {
	if err := buildPath.MkdirAll(); err != nil {
		return 0, errors.Wrap(err, tr("unable to create a folder to save the sketch"))
	}

	offset, mergedSource, err := b.sketchMergeSources(sourceOverrides)
	if err != nil {
		return 0, err
	}

	destFile := buildPath.Join(b.sketch.MainFile.Base() + ".cpp")
	if err := destFile.WriteFile([]byte(mergedSource)); err != nil {
		return 0, err
	}

	if err := b.sketchCopyAdditionalFiles(buildPath, sourceOverrides); err != nil {
		return 0, err
	}

	return offset, nil
}

// sketchMergeSources merges all the .ino source files included in a sketch to produce
// a single .cpp file.
func (b *Builder) sketchMergeSources(overrides map[string]string) (int, string, error) {
	lineOffset := 0
	mergedSource := ""

	getSource := func(f *paths.Path) (string, error) {
		path, err := b.sketch.FullPath.RelTo(f)
		if err != nil {
			return "", errors.Wrap(err, tr("unable to compute relative path to the sketch for the item"))
		}
		if override, ok := overrides[path.String()]; ok {
			return override, nil
		}
		data, err := f.ReadFile()
		if err != nil {
			return "", fmt.Errorf(tr("reading file %[1]s: %[2]s"), f, err)
		}
		return string(data), nil
	}

	// add Arduino.h inclusion directive if missing
	mainSrc, err := getSource(b.sketch.MainFile)
	if err != nil {
		return 0, "", err
	}
	if !includesArduinoH.MatchString(mainSrc) {
		mergedSource += "#include <Arduino.h>\n"
		lineOffset++
	}

	mergedSource += "#line 1 " + cpp.QuoteString(b.sketch.MainFile.String()) + "\n"
	mergedSource += mainSrc + "\n"
	lineOffset++

	for _, file := range b.sketch.OtherSketchFiles {
		src, err := getSource(file)
		if err != nil {
			return 0, "", err
		}
		mergedSource += "#line 1 " + cpp.QuoteString(file.String()) + "\n"
		mergedSource += src + "\n"
	}

	return lineOffset, mergedSource, nil
}

// sketchCopyAdditionalFiles copies the additional files for a sketch to the
// specified destination directory.
func (b *Builder) sketchCopyAdditionalFiles(buildPath *paths.Path, overrides map[string]string) error {
	for _, file := range b.sketch.AdditionalFiles {
		relpath, err := b.sketch.FullPath.RelTo(file)
		if err != nil {
			return errors.Wrap(err, tr("unable to compute relative path to the sketch for the item"))
		}

		targetPath := buildPath.JoinPath(relpath)
		// create the directory containing the target
		if err = targetPath.Parent().MkdirAll(); err != nil {
			return errors.Wrap(err, tr("unable to create the folder containing the item"))
		}

		var sourceBytes []byte
		if override, ok := overrides[relpath.String()]; ok {
			// use override source
			sourceBytes = []byte(override)
		} else {
			// read the source file
			s, err := file.ReadFile()
			if err != nil {
				return errors.Wrap(err, tr("unable to read contents of the source item"))
			}
			sourceBytes = s
		}

		// tag each addtional file with the filename of the source it was copied from
		sourceBytes = append([]byte("#line 1 "+cpp.QuoteString(file.String())+"\n"), sourceBytes...)

		err = writeIfDifferent(sourceBytes, targetPath)
		if err != nil {
			return errors.Wrap(err, tr("unable to write to destination file"))
		}
	}

	return nil
}

func writeIfDifferent(source []byte, destPath *paths.Path) error {
	// Check whether the destination file exists
	if destPath.NotExist() {
		// Write directly
		return destPath.WriteFile(source)
	}

	// Read the destination file if it exists
	existingBytes, err := destPath.ReadFile()
	if err != nil {
		return errors.Wrap(err, tr("unable to read contents of the destination item"))
	}

	// Overwrite if contents are different
	if !bytes.Equal(existingBytes, source) {
		return destPath.WriteFile(source)
	}

	// Source and destination are the same, don't write anything
	return nil
}

// SketchBuilder fixdoc
func SketchBuilder(
	sketchBuildPath *paths.Path,
	buildProperties *properties.Map,
	includesFolders paths.PathList,
	onlyUpdateCompilationDatabase bool,
	compilationDatabase *compilation.Database,
	jobs int,
	builderLogger *logger.BuilderLogger,
	progress *progress.Struct, progressCB rpc.TaskProgressCB,
) (paths.PathList, error) {
	includes := f.Map(includesFolders.AsStrings(), cpp.WrapWithHyphenI)

	if err := sketchBuildPath.MkdirAll(); err != nil {
		return nil, errors.WithStack(err)
	}

	sketchObjectFiles, err := utils.CompileFiles(
		sketchBuildPath, sketchBuildPath, buildProperties, includes,
		onlyUpdateCompilationDatabase,
		compilationDatabase,
		jobs,
		builderLogger,
		progress, progressCB,
	)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// The "src/" subdirectory of a sketch is compiled recursively
	sketchSrcPath := sketchBuildPath.Join("src")
	if sketchSrcPath.IsDir() {
		srcObjectFiles, err := utils.CompileFilesRecursive(
			sketchSrcPath, sketchSrcPath, buildProperties, includes,
			onlyUpdateCompilationDatabase,
			compilationDatabase,
			jobs,
			builderLogger,
			progress, progressCB,
		)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		sketchObjectFiles.AddAll(srcObjectFiles)
	}

	return sketchObjectFiles, nil
}
