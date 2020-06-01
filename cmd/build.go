package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/go-flutter-desktop/hover/internal/enginecache"

	"github.com/hashicorp/go-version"
	"github.com/otiai10/copy"
	"github.com/spf13/cobra"

	"github.com/go-flutter-desktop/hover/cmd/packaging"
	"github.com/go-flutter-desktop/hover/internal/androidmanifest"
	"github.com/go-flutter-desktop/hover/internal/build"
	"github.com/go-flutter-desktop/hover/internal/config"
	"github.com/go-flutter-desktop/hover/internal/fileutils"
	"github.com/go-flutter-desktop/hover/internal/log"
	"github.com/go-flutter-desktop/hover/internal/pubspec"
	"github.com/go-flutter-desktop/hover/internal/versioncheck"
)

var dotSlash = string([]byte{'.', filepath.Separator})

var (
	// common build flages (shared with `hover run`)
	buildOrRunFlutterTarget   string
	buildOrRunGoFlutterBranch string
	buildOrRunCachePath       string
	buildOrRunOpenGlVersion   string
	buildOrRunEngineVersion   string
	buildOrRunDocker          bool
	buildOrRunDebug           bool
	buildOrRunRelease         bool
	buildOrRunProfile         bool
	buildOrRunMode            build.Mode
)

func initCompileFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&buildOrRunFlutterTarget, "target", "t", config.BuildTargetDefault, "The main entry-point file of the application.")
	cmd.PersistentFlags().StringVarP(&buildOrRunGoFlutterBranch, "branch", "b", "", "The 'go-flutter' version to use. (@master or @v0.20.0 for example)")
	cmd.PersistentFlags().StringVar(&buildOrRunCachePath, "cache-path", enginecache.DefaultCachePath(), "The path that hover uses to cache dependencies such as the Flutter engine .so/.dll")
	cmd.PersistentFlags().StringVar(&buildOrRunOpenGlVersion, "opengl", config.BuildOpenGlVersionDefault, "The OpenGL version specified here is only relevant for external texture plugin (i.e. video_plugin).\nIf 'none' is provided, texture won't be supported. Note: the Flutter Engine still needs a OpenGL compatible context.")
	cmd.PersistentFlags().StringVar(&buildOrRunEngineVersion, "engine-version", config.BuildEngineDefault, "The flutter engine version to use.")
	cmd.PersistentFlags().BoolVar(&buildOrRunDocker, "docker", false, "Execute the go build and packaging in a docker container. The Flutter build is always run locally")
	cmd.PersistentFlags().BoolVar(&buildOrRunDebug, "debug", false, "Build a debug version of the app.")
	cmd.PersistentFlags().BoolVar(&buildOrRunRelease, "release", false, "Enable release builds. Currently very experimental and only for linux available")
	cmd.PersistentFlags().BoolVar(&buildOrRunProfile, "profile", false, "Enable profile builds. Currently very experimental and only for linux available")

	cmd.PersistentFlags().MarkHidden("branch")
}

var (
	// `hover build`-only build flags
	buildVersionNumber          string
	buildSkipEngineDownload     bool
	buildSkipFlutterBuildBundle bool
)

const mingwGccBinName = "x86_64-w64-mingw32-gcc"
const clangBinName = "o32-clang"

var engineCachePath string

func init() {
	initCompileFlags(buildCmd)

	buildCmd.PersistentFlags().StringVar(&buildVersionNumber, "version-number", "", "Override the version number used in build and packaging. You may use it with $(git describe --tags)")
	buildCmd.PersistentFlags().BoolVar(&buildSkipEngineDownload, "skip-engine-download", false, "Skip donwloading the Flutter Engine and artifacts.")
	buildCmd.PersistentFlags().BoolVar(&buildSkipFlutterBuildBundle, "skip-flutter-build-bundle", false, "Skip the 'flutter build bundle' step.")

	buildCmd.AddCommand(buildLinuxCmd)
	buildCmd.AddCommand(buildLinuxSnapCmd)
	buildCmd.AddCommand(buildLinuxDebCmd)
	buildCmd.AddCommand(buildLinuxAppImageCmd)
	buildCmd.AddCommand(buildLinuxRpmCmd)
	buildCmd.AddCommand(buildLinuxPkgCmd)
	buildCmd.AddCommand(buildDarwinCmd)
	buildCmd.AddCommand(buildDarwinBundleCmd)
	buildCmd.AddCommand(buildDarwinPkgCmd)
	buildCmd.AddCommand(buildDarwinDmgCmd)
	buildCmd.AddCommand(buildWindowsCmd)
	buildCmd.AddCommand(buildWindowsMsiCmd)
	rootCmd.AddCommand(buildCmd)
}

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build a desktop release",
}

var buildLinuxCmd = &cobra.Command{
	Use:   "linux",
	Short: "Build a desktop release for linux",
	Run: func(cmd *cobra.Command, args []string) {
		subcommandBuild("linux", packaging.NoopTask)
	},
}

var buildLinuxSnapCmd = &cobra.Command{
	Use:   "linux-snap",
	Short: "Build a desktop release for linux and package it for snap",
	Run: func(cmd *cobra.Command, args []string) {
		subcommandBuild("linux", packaging.LinuxSnapTask)
	},
}

var buildLinuxDebCmd = &cobra.Command{
	Use:   "linux-deb",
	Short: "Build a desktop release for linux and package it for deb",
	Run: func(cmd *cobra.Command, args []string) {
		subcommandBuild("linux", packaging.LinuxDebTask)
	},
}

var buildLinuxAppImageCmd = &cobra.Command{
	Use:   "linux-appimage",
	Short: "Build a desktop release for linux and package it for AppImage",
	Run: func(cmd *cobra.Command, args []string) {
		subcommandBuild("linux", packaging.LinuxAppImageTask)
	},
}

var buildLinuxRpmCmd = &cobra.Command{
	Use:   "linux-rpm",
	Short: "Build a desktop release for linux and package it for rpm",
	Run: func(cmd *cobra.Command, args []string) {
		subcommandBuild("linux", packaging.LinuxRpmTask)
	},
}

var buildLinuxPkgCmd = &cobra.Command{
	Use:   "linux-pkg",
	Short: "Build a desktop release for linux and package it for pacman pkg",
	Run: func(cmd *cobra.Command, args []string) {
		subcommandBuild("linux", packaging.LinuxPkgTask)
	},
}

var buildDarwinCmd = &cobra.Command{
	Use:   "darwin",
	Short: "Build a desktop release for darwin",
	Run: func(cmd *cobra.Command, args []string) {
		subcommandBuild("darwin", packaging.NoopTask)
	},
}

var buildDarwinBundleCmd = &cobra.Command{
	Use:   "darwin-bundle",
	Short: "Build a desktop release for darwin and package it for OSX bundle",
	Run: func(cmd *cobra.Command, args []string) {
		subcommandBuild("darwin", packaging.DarwinBundleTask)
	},
}

var buildDarwinPkgCmd = &cobra.Command{
	Use:   "darwin-pkg",
	Short: "Build a desktop release for darwin and package it for OSX pkg installer",
	Run: func(cmd *cobra.Command, args []string) {
		subcommandBuild("darwin", packaging.DarwinPkgTask)
	},
}

var buildDarwinDmgCmd = &cobra.Command{
	Use:   "darwin-dmg",
	Short: "Build a desktop release for darwin and package it for OSX dmg",
	Run: func(cmd *cobra.Command, args []string) {
		subcommandBuild("darwin", packaging.DarwinDmgTask)
	},
}

var buildWindowsCmd = &cobra.Command{
	Use:   "windows",
	Short: "Build a desktop release for windows",
	Run: func(cmd *cobra.Command, args []string) {
		subcommandBuild("windows", packaging.NoopTask)
	},
}

var buildWindowsMsiCmd = &cobra.Command{
	Use:   "windows-msi",
	Short: "Build a desktop release for windows and package it for msi",
	Run: func(cmd *cobra.Command, args []string) {
		subcommandBuild("windows", packaging.WindowsMsiTask)
	},
}

// TODO: replace targetOS with a same Task type for build (build.Task) ?
func subcommandBuild(targetOS string, packagingTask packaging.Task) {
	assertHoverInitialized()
	packagingTask.AssertInitialized()
	if !buildOrRunDocker {
		packagingTask.AssertSupported()
	}

	initBuildParameters(targetOS, build.ReleaseMode)

	if !buildSkipFlutterBuildBundle {
		cleanBuildOutputsDir(targetOS)
		buildFlutterBundle(targetOS)
	}
	if buildOrRunDocker {
		var buildFlags []string
		buildFlags = append(buildFlags, commonFlags()...)
		buildFlags = append(buildFlags, "--skip-flutter-build-bundle")
		buildFlags = append(buildFlags, "--skip-engine-download")
		if buildVersionNumber != "" {
			buildFlags = append(buildFlags, "--version-number", buildVersionNumber)
		}
		if buildOrRunDebug {
			buildFlags = append(buildFlags, "--debug")
		}
		if buildOrRunRelease {
			buildFlags = append(buildFlags, "--release")
		}
		if buildOrRunProfile {
			buildFlags = append(buildFlags, "--profile")
		}
		dockerHoverBuild(targetOS, packagingTask, buildFlags, nil)
	} else {
		buildGoBinary(targetOS, nil)
		packagingTask.Pack(buildVersionNumber)
	}
}

// initBuildParameters is used to initialize all the build parameters. It sets
// fallback values based on config or defaults for values that have not
// explicitly been set through flags.
func initBuildParameters(targetOS string, defaultbuildOrRunMode build.Mode) {
	if buildOrRunCachePath == "" {
		log.Errorf("Missing cache path, cannot continue. Please see previous warning.")
		os.Exit(1)
	}

	if buildOrRunEngineVersion == config.BuildEngineDefault && config.GetConfig().Engine != "" {
		log.Warnf("changing the engine version can lead to undesirable behavior")
		buildOrRunEngineVersion = config.GetConfig().Engine
	}

	// TODO: This override doesn't work properly when the config specifies a
	// value other than the default, and the flag is used to revert back to the
	// default.
	//
	// The comment on (*FlagSet).Lookup(..).Changed isn't very clear, but we
	// could test how that behaves and switch to it.
	if buildOrRunOpenGlVersion == config.BuildOpenGlVersionDefault && config.GetConfig().OpenGL != "" {
		buildOrRunOpenGlVersion = config.GetConfig().OpenGL
	}

	if buildVersionNumber == "" {
		buildVersionNumber = pubspec.GetPubSpec().GetVersion()
	}

	numberOfbuildOrRunModeFlagsSet := 0
	for _, flag := range []bool{buildOrRunDebug, buildOrRunRelease, buildOrRunProfile} {
		if flag {
			numberOfbuildOrRunModeFlagsSet++
		}
	}
	if numberOfbuildOrRunModeFlagsSet > 1 {
		log.Errorf("Only one of --debug, --release or --profile can be set at one time")
		os.Exit(1)
	}
	if numberOfbuildOrRunModeFlagsSet == 0 {
		buildOrRunMode = defaultbuildOrRunMode
	}

	if buildOrRunDebug {
		buildOrRunMode = build.DebugMode
	}
	if buildOrRunRelease {
		buildOrRunMode = build.ReleaseMode
	}
	if buildOrRunProfile {
		buildOrRunMode = build.ProfileMode
	}

	if buildOrRunMode.IsAot && targetOS != runtime.GOOS {
		log.Errorf("AOT builds currently only work on their host OS")
		os.Exit(1)
	}

	engineCachePath = enginecache.EngineCachePath(targetOS, buildOrRunCachePath, buildOrRunMode)
	if !buildSkipEngineDownload {
		enginecache.ValidateOrUpdateEngine(targetOS, buildOrRunCachePath, buildOrRunEngineVersion, buildOrRunMode)
	}
}

func commonFlags() []string {
	var f []string
	if buildOrRunFlutterTarget != config.BuildTargetDefault {
		f = append(f, "--target", buildOrRunFlutterTarget)
	}
	if buildOrRunGoFlutterBranch != "" {
		f = append(f, "--branch", buildOrRunGoFlutterBranch)
	}
	if buildOrRunOpenGlVersion != config.BuildOpenGlVersionDefault {
		f = append(f, "--opengl", buildOrRunOpenGlVersion)
	}
	return f
}

// assertTargetFileExists checks and adds the lib/main_desktop.dart dart entry
// point if needed
func assertTargetFileExists(targetFilename string) {
	_, err := os.Stat(targetFilename)
	if os.IsNotExist(err) {
		log.Warnf("Target file \"%s\" not found.", targetFilename)
		if targetFilename == config.BuildTargetDefault {
			log.Warnf("Let hover add the \"lib/main_desktop.dart\" file? ")
			if askForConfirmation() {
				fileutils.CopyAsset("app/main_desktop.dart", filepath.Join("lib", "main_desktop.dart"), fileutils.AssetsBox())
				log.Infof("Target file \"lib/main_desktop.dart\" has been created.")
				log.Infof("       Depending on your project, you might want to tweak it.")
				return
			}
		}
		log.Printf("You can define a custom traget by using the %s flag.", log.Au().Magenta("--target"))
		os.Exit(1)
	}
	if err != nil {
		log.Errorf("Failed to stat lib/main_desktop.dart: %v\n", err)
		os.Exit(1)
	}
}

func cleanBuildOutputsDir(targetOS string) {
	err := os.RemoveAll(build.OutputDirectoryPath(targetOS))
	log.Printf("Cleaning the build directory")
	if err != nil {
		log.Errorf("Failed to remove output directory %s: %v", build.OutputDirectoryPath(targetOS), err)
		os.Exit(1)
	}
	err = os.MkdirAll(build.OutputDirectoryPath(targetOS), 0775)
	if err != nil {
		log.Errorf("Failed to create output directory %s: %v", build.OutputDirectoryPath(targetOS), err)
		os.Exit(1)
	}
}

func buildFlutterBundle(targetOS string) {
	if buildOrRunFlutterTarget == config.BuildTargetDefault && config.GetConfig().Target != "" {
		buildOrRunFlutterTarget = config.GetConfig().Target
	}
	assertTargetFileExists(buildOrRunFlutterTarget)

	runPluginGet, err := shouldRunPluginGet()
	if err != nil {
		log.Errorf("Failed to check if plugin get should be run: %v.\n", err)
		os.Exit(1)
	}
	if runPluginGet {
		log.Printf("listing available plugins:")
		if hoverPluginGet(true) {
			// TODO: change this so that it only logs when there are plugins missing..
			log.Infof(fmt.Sprintf("Run `%s` to update plugins", log.Au().Magenta("hover plugins get")))
		}
	}

	checkFlutterChannel()
	var trackWidgetCreation string
	if buildOrRunMode == build.DebugMode {
		trackWidgetCreation = "--track-widget-creation"
	}

	cmdFlutterBuild := exec.Command(build.FlutterBin(), "build", "bundle",
		"--asset-dir", filepath.Join(build.OutputDirectoryPath(targetOS), "flutter_assets"),
		"--target", buildOrRunFlutterTarget,
		trackWidgetCreation,
	)
	cmdFlutterBuild.Stderr = os.Stderr
	cmdFlutterBuild.Stdout = os.Stdout

	log.Infof("Bundling flutter app")
	err = cmdFlutterBuild.Run()
	if err != nil {
		log.Errorf("Flutter build failed: %v", err)
		os.Exit(1)
	}
	if buildOrRunMode.IsAot {
		err := os.Remove(filepath.Join(build.OutputDirectoryPath(targetOS), "flutter_assets", "isolate_snapshot_data"))
		if err != nil {
			log.Errorf("Failed to remove unused isolate_snapshot_data: %v", err)
			os.Exit(1)
		}
		err = os.Remove(filepath.Join(build.OutputDirectoryPath(targetOS), "flutter_assets", "vm_snapshot_data"))
		if err != nil {
			log.Errorf("Failed to remove unused vm_snapshot_data: %v", err)
			os.Exit(1)
		}
		err = os.Remove(filepath.Join(build.OutputDirectoryPath(targetOS), "flutter_assets", "kernel_blob.bin"))
		if err != nil {
			log.Errorf("Failed to remove unused kernel_blob.bin: %v", err)
			os.Exit(1)
		}
		dart := filepath.Join(engineCachePath, "dart-sdk", "bin", "dart"+build.ExecutableExtension(targetOS))
		var genSnapshot string
		if targetOS == "darwin" {
			genSnapshot = filepath.Join(engineCachePath, "artifacts", "gen_snapshot"+build.ExecutableExtension(targetOS))
		} else {
			genSnapshot = filepath.Join(engineCachePath, "gen_snapshot"+build.ExecutableExtension(targetOS))
		}
		kernelSnapshot := filepath.Join(build.OutputDirectoryPath(targetOS), "kernel_snapshot.dill")
		elfSnapshot := filepath.Join(build.OutputDirectoryPath(targetOS), "libapp.so")
		cmdGenerateKernelSnapshot := exec.Command(
			dart,
			filepath.Join(engineCachePath, "artifacts", "frontend_server.dart.snapshot"),
			"--sdk-root="+filepath.Join(engineCachePath, "flutter_patched_sdk_product"),
			"--target=flutter",
			"--aot",
			"--tfa",
			"-Ddart.vm.product=true",
			"--packages=.packages",
			"--output-dill="+kernelSnapshot,
			buildOrRunFlutterTarget,
		)
		cmdGenerateKernelSnapshot.Stderr = os.Stderr
		log.Infof("Generating kernel snapshot")
		output, err := cmdGenerateKernelSnapshot.Output()
		if err != nil {
			log.Errorf("Generating kernel snapshot failed: %v", err)
			log.Errorf(string(output))
			os.Exit(1)
		}
		generateAotSnapshotCommand := []string{
			genSnapshot,
			"--no-causal-async-stacks",
			"--lazy-async-stacks",
			"--deterministic",
			"--snapshot_kind=app-aot-elf",
			"--elf=" + elfSnapshot,
		}
		if buildOrRunMode == build.ReleaseMode {
			generateAotSnapshotCommand = append(generateAotSnapshotCommand, "--strip")
		}
		if targetOS == "darwin" {
			generateAotSnapshotCommand = append(generateAotSnapshotCommand,
				"--dedup-instructions",
				"--no-code-comments",
			)
		}
		generateAotSnapshotCommand = append(generateAotSnapshotCommand, kernelSnapshot)
		cmdGenerateAotSnapshot := exec.Command(
			generateAotSnapshotCommand[0],
			generateAotSnapshotCommand[1:]...,
		)
		cmdGenerateAotSnapshot.Stderr = os.Stderr
		log.Infof("Generating ELF snapshot")
		output, err = cmdGenerateAotSnapshot.Output()
		if err != nil {
			log.Errorf("Generating AOT snapshot failed: %v", err)
			log.Errorf(string(output))
			os.Exit(1)
		}
		err = os.Remove(kernelSnapshot)
		if err != nil {
			log.Errorf("Failed to remove kernel_snapshot.dill: %v", err)
			os.Exit(1)
		}
	}
}

func buildGoBinary(targetOS string, vmArguments []string) {
	if vmArgsFromEnv := os.Getenv("HOVER_IN_DOCKER_BUILD_VMARGS"); len(vmArgsFromEnv) > 0 {
		vmArguments = append(vmArguments, strings.Split(vmArgsFromEnv, ",")...)
	}

	fileutils.CopyDir(build.IntermediatesDirectoryPath(targetOS), build.OutputDirectoryPath(targetOS))

	for _, engineFile := range build.EngineFiles(targetOS, buildOrRunMode) {
		outputEngineFile := filepath.Join(build.OutputDirectoryPath(targetOS), engineFile)
		if _, err := os.Stat(outputEngineFile); err == nil || os.IsExist(err) {
			err = os.RemoveAll(outputEngineFile)
			if err != nil {
				log.Errorf("Failed to remove old engine: %v", err)
				os.Exit(1)
			}
		}
		err := copy.Copy(
			filepath.Join(engineCachePath, engineFile),
			outputEngineFile,
		)
		if err != nil {
			log.Errorf("Failed to copy %s: %v", engineFile, err)
			os.Exit(1)
		}
	}

	err := copy.Copy(
		filepath.Join(engineCachePath, "artifacts", "icudtl.dat"),
		filepath.Join(build.OutputDirectoryPath(targetOS), "icudtl.dat"),
	)
	if err != nil {
		log.Errorf("Failed to copy icudtl.dat: %v", err)
		os.Exit(1)
	}

	fileutils.CopyDir(
		filepath.Join(build.BuildPath, "assets"),
		filepath.Join(build.OutputDirectoryPath(targetOS), "assets"),
	)

	wd, err := os.Getwd()
	if err != nil {
		log.Errorf("Failed to get working dir: %v", err)
		os.Exit(1)
	}

	if buildOrRunGoFlutterBranch == "" {
		currentTag, err := versioncheck.CurrentGoFlutterTag(filepath.Join(wd, build.BuildPath))
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		if currentTag == "" {
			log.Warnf("Empty version found for go-flutter. Skipping upgrade check. (This may be caused by replace statement in the application go.mod)")
		} else {
			semver, err := version.NewSemver(currentTag)
			if err != nil {
				log.Errorf("Faild to parse 'go-flutter' semver: %v", err)
				os.Exit(1)
			}

			if semver.Prerelease() != "" {
				log.Infof("Upgrading 'go-flutter' to the latest release")
				// no buildBranch provided and currentTag isn't a release,
				// force update. (same behaviour as previous version of hover).
				err = upgradeGoFlutter(targetOS)
				if err != nil {
					// the upgrade can fail silently
					log.Warnf("Upgrade ignored, current 'go-flutter' version: %s", currentTag)
				}
			} else {
				// when the buildBranch is empty and the currentTag is a release.
				// Check if the 'go-flutter' needs updates.
				versioncheck.CheckForGoFlutterUpdate(filepath.Join(wd, build.BuildPath), currentTag)
			}
		}

	} else {
		log.Printf("Downloading 'go-flutter' %s", buildOrRunGoFlutterBranch)

		// when the buildBranch is set, fetch the go-flutter branch version.
		err = upgradeGoFlutter(targetOS)
		if err != nil {
			os.Exit(1)
		}
	}

	versioncheck.CheckForHoverUpdate(hoverVersion())

	if buildOrRunOpenGlVersion == "none" {
		log.Warnf("The '--opengl=none' flag makes go-flutter incompatible with texture plugins!")
	}

	buildCommandString := buildCommand(targetOS, vmArguments, build.OutputBinaryPath(config.GetConfig().GetExecutableName(pubspec.GetPubSpec().Name), targetOS))
	cmdGoBuild := exec.Command(buildCommandString[0], buildCommandString[1:]...)
	cmdGoBuild.Dir = filepath.Join(wd, build.BuildPath)
	cmdGoBuild.Env = append(os.Environ(),
		buildEnv(targetOS, engineCachePath)...,
	)

	cmdGoBuild.Stderr = os.Stderr
	cmdGoBuild.Stdout = os.Stdout

	log.Infof("Compiling 'go-flutter' and plugins")
	err = cmdGoBuild.Run()
	if err != nil {
		log.Errorf("Go build failed: %v", err)
		os.Exit(1)
	}
	log.Infof("Successfully compiled executable binary for %s", targetOS)
}

func buildEnv(targetOS string, engineCachePath string) []string {
	var cgoLdflags string
	var cgoCflags string

	outputDirPath := build.OutputDirectoryPath(targetOS)

	switch targetOS {
	case "darwin":
		cgoLdflags = fmt.Sprintf("-F%s -Wl,-rpath,@executable_path", engineCachePath)
		cgoLdflags += fmt.Sprintf(" -F%s -L%s", outputDirPath, outputDirPath)
		cgoLdflags += " -mmacosx-version-min=10.10"
		cgoLdflags += fmt.Sprintf(" -framework %s", build.LibraryName(targetOS))
		cgoCflags = "-mmacosx-version-min=10.10"
	case "linux":
		cgoLdflags = fmt.Sprintf("-L%s -L%s", engineCachePath, outputDirPath)
		cgoLdflags += fmt.Sprintf(" -l%s -Wl,-rpath,$ORIGIN", build.LibraryName(targetOS))
	case "windows":
		cgoLdflags = fmt.Sprintf("-L%s -L%s", engineCachePath, outputDirPath)
		cgoLdflags += fmt.Sprintf(" -l%s", build.LibraryName(targetOS))
	default:
		log.Errorf("Target platform %s is not supported, cgo_ldflags not implemented.", targetOS)
		os.Exit(1)
	}
	env := []string{
		"GO111MODULE=on",
		"CGO_LDFLAGS=" + cgoLdflags,
		"CGO_CFLAGS=" + cgoCflags,
		"GOOS=" + targetOS,
		"GOARCH=amd64",
		"CGO_ENABLED=1",
	}
	if runtime.GOOS == "linux" {
		if targetOS == "windows" {
			env = append(env,
				"CC="+mingwGccBinName,
			)
		}
		if targetOS == "darwin" {
			env = append(env,
				"CC="+clangBinName,
			)
		}
	}
	return env
}

func buildCommand(targetOS string, vmArguments []string, outputBinaryPath string) []string {
	abspath, err := filepath.Abs(build.BuildPath)
	if err != nil {
		log.Errorf("unable to detect absolute path: %s - %v", build.BuildPath, err)
		os.Exit(1)
	}

	currentTag, err := versioncheck.CurrentGoFlutterTag(abspath)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	var ldflags []string
	if buildOrRunMode != build.DebugMode {
		vmArguments = append(vmArguments, "--disable-dart-asserts")
		vmArguments = append(vmArguments, "--disable-observatory")

		if targetOS == "windows" {
			ldflags = append(ldflags, "-H=windowsgui")
		}
		ldflags = append(ldflags, "-s")
		ldflags = append(ldflags, "-w")
	}
	ldflags = append(ldflags, fmt.Sprintf("-X main.vmArguments=%s", strings.Join(vmArguments, ";")))
	// overwrite go-flutter build-constants values
	ldflags = append(ldflags, fmt.Sprintf(
		"-X 'github.com/go-flutter-desktop/go-flutter.ProjectVersion=%s' "+
			" -X 'github.com/go-flutter-desktop/go-flutter.PlatformVersion=%s' "+
			" -X 'github.com/go-flutter-desktop/go-flutter.ProjectName=%s' "+
			" -X 'github.com/go-flutter-desktop/go-flutter.ProjectOrganizationName=%s'",
		buildVersionNumber,
		currentTag,
		config.GetConfig().GetApplicationName(pubspec.GetPubSpec().Name),
		androidmanifest.AndroidOrganizationName()))

	outputCommand := []string{
		"go",
		"build",
		"-tags=opengl" + buildOrRunOpenGlVersion,
		"-o", outputBinaryPath,
		"-v",
	}
	outputCommand = append(outputCommand, fmt.Sprintf("-ldflags=%s", strings.Join(ldflags, " ")))
	outputCommand = append(outputCommand, dotSlash+"cmd")
	return outputCommand
}
