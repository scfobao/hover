package enginecache

import (
	"archive/zip"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/go-flutter-desktop/hover/internal/build"
	"github.com/go-flutter-desktop/hover/internal/flutterversion"
	"github.com/go-flutter-desktop/hover/internal/log"
)

func createSymLink(oldname, newname string) error {
	err := os.Remove(newname)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "failed to remove existing symlink")
	}

	err = os.Symlink(oldname, newname)
	if err != nil {
		return errors.Wrap(err, "failed to create symlink")
	}
	return nil
}

// Unzip will decompress a zip archive, moving all files and folders
// within the zip file (parameter 1) to an output directory (parameter 2).
func unzip(src string, dest string) ([]string, error) {
	var filenames []string

	r, err := zip.OpenReader(src)
	if err != nil {
		return filenames, err
	}
	defer r.Close()

	for _, f := range r.File {

		// Store filename/path for returning and using later on
		fpath := filepath.Join(dest, f.Name)

		// Check for ZipSlip. More Infof: http://bit.ly/2MsjAWE
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return filenames, fmt.Errorf("%s: illegal file path", fpath)
		}

		filenames = append(filenames, fpath)

		if f.FileInfo().IsDir() {
			// Make Folder
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		// Make File
		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return filenames, err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return filenames, err
		}

		rc, err := f.Open()
		if err != nil {
			return filenames, err
		}

		_, err = io.Copy(outFile, rc)

		// Close the file without defer to close before next iteration of loop
		outFile.Close()
		rc.Close()

		if err != nil {
			return filenames, err
		}
	}
	return filenames, nil
}

// Function to prind download percent completion
func printDownloadPercent(done chan chan struct{}, path string, expectedSize int64) {
	var completedCh chan struct{}
	for {
		fi, err := os.Stat(path)
		if err != nil {
			log.Warnf("%v", err)
		}

		size := fi.Size()

		if size == 0 {
			size = 1
		}

		var percent = float64(size) / float64(expectedSize) * 100

		// We use '\033[2K\r' to avoid carriage return, it will print above previous.
		fmt.Printf("\033[2K\r %.0f %% / 100 %%", percent)

		if completedCh != nil {
			close(completedCh)
			return
		}

		select {
		case completedCh = <-done:
		case <-time.After(time.Second / 60): // Flutter promises 60fps, right? ;)
		}
	}
}

func moveFile(srcPath, destPath string) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("Couldn't open src file: %s", err)
	}
	srcFileInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	perm := srcFileInfo.Mode() & os.ModePerm
	destFile, err := os.OpenFile(destPath, flag, perm)
	if err != nil {
		srcFile.Close()
		return fmt.Errorf("Couldn't open dest file: %s", err)
	}
	defer destFile.Close()
	_, err = io.Copy(destFile, srcFile)
	srcFile.Close()
	if err != nil {
		return fmt.Errorf("Writing to output file failed: %s", err)
	}
	// The copy was successful, so now delete the original file
	err = os.Remove(srcPath)
	if err != nil {
		return fmt.Errorf("Failed removing original file: %s", err)
	}
	return nil
}

// Function to download file with given path and url.
func downloadFile(filepath string, url string) error {
	// // Printf download url in case user needs it.
	// log.Printf("Downloading file from\n '%s'\n to '%s'", url, filepath)

	start := time.Now()

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	expectedSize, err := strconv.Atoi(resp.Header.Get("Content-Length"))
	if err != nil {
		return errors.Wrap(err, "failed to get Content-Length header")
	}

	doneCh := make(chan chan struct{})
	go printDownloadPercent(doneCh, filepath, int64(expectedSize))

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	// close channel to indicate we're done
	doneCompletedCh := make(chan struct{})
	doneCh <- doneCompletedCh // signal that download is done
	<-doneCompletedCh         // wait for signal that printing has completed

	elapsed := time.Since(start)
	log.Printf("\033[2K\rDownload completed in %.2fs", elapsed.Seconds())
	return nil
}

//noinspection GoNameStartsWithPackageName
func EngineCachePath(targetOS, cachePath string, mode build.Mode) string {
	return filepath.Join(cachePath, "hover", "engine", platform(targetOS, mode))
}

func basePlatform(targetOS string) string {
	// TODO: support more arch's than x64?
	return fmt.Sprintf("%s-x64", targetOS)
}

func platform(targetOS string, mode build.Mode) string {
	platform := basePlatform(targetOS)
	if mode.IsAot {
		platform += fmt.Sprintf("-%s", mode.Name)
	}
	return platform
}

// ValidateOrUpdateEngine validates the engine we have cached matches the
// flutter version, or otherwise downloads a new engine. The engine cache
// location is set by the the user.
func ValidateOrUpdateEngine(targetOS, cachePath, requiredEngineVersion string, mode build.Mode) {
	basePlatform := basePlatform(targetOS)
	platform := platform(targetOS, mode)
	engineCachePath := EngineCachePath(targetOS, cachePath, mode)

	if strings.Contains(engineCachePath, " ") {
		log.Errorf("Cannot save the engine to '%s', engine cache is not compatible with path containing spaces.", cachePath)
		log.Errorf("       Please run hover with a another engine cache path. Example:")
		log.Errorf("              %s", log.Au().Magenta("hover run --cache-path \"C:\\cache\""))
		log.Errorf("       The --cache-path flag will have to be provided to every build and run command.")
		os.Exit(1)
	}

	cachedEngineVersionPath := filepath.Join(engineCachePath, "version")
	cachedEngineVersionBytes, err := ioutil.ReadFile(cachedEngineVersionPath)
	if err != nil && !os.IsNotExist(err) {
		log.Errorf("Failed to read cached engine version: %v", err)
		os.Exit(1)
	}
	cachedEngineVersion := string(cachedEngineVersionBytes)
	if len(requiredEngineVersion) == 0 {
		requiredEngineVersion = flutterversion.FlutterRequiredEngineVersion()
	}

	if cachedEngineVersion == requiredEngineVersion {
		log.Printf("Using engine from cache")
		return
	} else {
		// Engine is outdated, we remove the old engine and continue to download
		// the new engine.
		err = os.RemoveAll(engineCachePath)
		if err != nil {
			log.Errorf("Failed to remove outdated engine: %v", err)
			os.Exit(1)
		}
	}

	err = os.MkdirAll(engineCachePath, 0775)
	if err != nil {
		log.Errorf("Failed to create engine cache directory: %v", err)
		os.Exit(1)
	}

	dir, err := ioutil.TempDir("", "hover-engine-download")
	if err != nil {
		log.Errorf("Failed to create tmp dir for engine download: %v", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	err = os.MkdirAll(dir, 0700)
	if err != nil {
		log.Warnf("%v", err)
	}

	engineZipPath := filepath.Join(dir, "engine.zip")
	engineExtractPath := filepath.Join(dir, "engine")

	targetedDomain := "https://storage.googleapis.com"
	envURLFlutter := os.Getenv("FLUTTER_STORAGE_BASE_URL")
	if envURLFlutter != "" {
		targetedDomain = envURLFlutter
	}

	artifactsDownloadURL := fmt.Sprintf("%s/flutter_infra/flutter/%s/%s/artifacts.zip", targetedDomain, requiredEngineVersion, basePlatform)

	artifactsZipPath := filepath.Join(dir, "artifacts.zip")

	log.Printf("Downloading artifacts for platform %s at version %s...", platform, requiredEngineVersion)
	err = downloadFile(artifactsZipPath, artifactsDownloadURL)
	if err != nil {
		log.Errorf("Failed to download artifacts: %v", err)
		os.Exit(1)
	}
	artifactsCachePath := filepath.Join(engineCachePath, "artifacts")
	_, err = unzip(artifactsZipPath, artifactsCachePath)
	if err != nil {
		log.Warnf("%v", err)
	}

	if mode.IsAot {

		dartSdkDownloadURL := fmt.Sprintf("%s/flutter_infra/flutter/%s/dart-sdk-%s.zip", targetedDomain, requiredEngineVersion, basePlatform)

		dartSdkZipPath := filepath.Join(dir, "dart-sdk.zip")

		log.Printf("Downloading dart-sdk for platform %s at version %s...", platform, requiredEngineVersion)
		err = downloadFile(dartSdkZipPath, dartSdkDownloadURL)
		if err != nil {
			log.Errorf("Failed to download dart-sdk: %v", err)
			os.Exit(1)
		}
		dartSdkCachePath := filepath.Join(engineCachePath)
		_, err = unzip(dartSdkZipPath, dartSdkCachePath)
		if err != nil {
			log.Warnf("%v", err)
		}

		flutterPatchedSdkDownloadURL := fmt.Sprintf("%s/flutter_infra/flutter/%s/flutter_patched_sdk_product.zip", targetedDomain, requiredEngineVersion)

		flutterPatchedSdkZipPath := filepath.Join(dir, "flutter_patched_sdk_product.zip")

		log.Printf("Downloading flutter patched sdk for platform %s at version %s...", platform, requiredEngineVersion)
		err = downloadFile(flutterPatchedSdkZipPath, flutterPatchedSdkDownloadURL)
		if err != nil {
			log.Errorf("Failed to download flutter patched sdk: %v", err)
			os.Exit(1)
		}
		flutterPatchedSdkCachePath := filepath.Join(engineCachePath)
		_, err = unzip(flutterPatchedSdkZipPath, flutterPatchedSdkCachePath)
		if err != nil {
			log.Warnf("%v", err)
		}
	}

	log.Printf("Downloading engine for platform %s at version %s...", platform, requiredEngineVersion)
	file := fmt.Sprintf("%s/", platform)
	switch targetOS {
	case "linux":
		file += "linux-x64-flutter-gtk.zip"
	case "darwin":
		file += "FlutterMacOS.framework.zip"
	case "windows":
		file += "windows-x64-flutter.zip"
	}
	engineDownloadURL := fmt.Sprintf("%s/flutter_infra/flutter/%s/%s", targetedDomain, requiredEngineVersion, file)

	err = downloadFile(engineZipPath, engineDownloadURL)
	if err != nil {
		log.Errorf("Failed to download engine: %v", err)
		log.Infof("That may mean no engine download is currently available. You'll have to wait for one to get available")
		os.Exit(1)
	}
	_, err = unzip(engineZipPath, engineExtractPath)
	if err != nil {
		log.Warnf("%v", err)
	}

	if targetOS == "darwin" {
		libraryName := build.LibraryName(targetOS)
		frameworkZipPath := filepath.Join(engineExtractPath, fmt.Sprintf("%s.framework.zip", libraryName))
		frameworkDestPath := filepath.Join(engineCachePath, fmt.Sprintf("%s.framework", libraryName))
		_, err = unzip(frameworkZipPath, frameworkDestPath)
		if err != nil {
			log.Errorf("Failed to unzip engine framework: %v", err)
			os.Exit(1)
		}

		createSymLink("A", frameworkDestPath+"/Versions/Current")
		createSymLink(fmt.Sprintf("Versions/Current/%s", libraryName), fmt.Sprintf("%s/%s", frameworkDestPath, libraryName))
		createSymLink("Versions/Current/Headers", fmt.Sprintf("%s/Headers", frameworkDestPath))
		createSymLink("Versions/Current/Modules", fmt.Sprintf("%s/Modules", frameworkDestPath))
		createSymLink("Versions/Current/Resources", fmt.Sprintf("%s/Resources", frameworkDestPath))
	} else {
		for _, engineFile := range build.EngineFiles(targetOS, mode) {
			err := moveFile(
				filepath.Join(engineExtractPath, engineFile),
				filepath.Join(engineCachePath, engineFile),
			)
			if err != nil {
				log.Errorf("Failed to move downloaded %s: %v", engineFile, err)
				os.Exit(1)
			}
		}
	}

	// Strip linux engine after download and not at every build
	if targetOS == "linux" {
		unstrippedEngineFile := filepath.Join(engineCachePath, build.EngineFiles(targetOS, mode)[0])
		err = exec.Command("strip", "-s", unstrippedEngineFile).Run()
		if err != nil {
			log.Errorf("Failed to strip %s: %v", unstrippedEngineFile, err)
			os.Exit(1)
		}
	}

	// The gen_snapshot binary comes with the artifacts for darwin
	if mode.IsAot && targetOS != "darwin" {
		err = moveFile(
			filepath.Join(engineExtractPath, "gen_snapshot"+build.ExecutableExtension(targetOS)),
			filepath.Join(engineCachePath, "gen_snapshot"+build.ExecutableExtension(targetOS)),
		)
		if err != nil {
			log.Errorf("Failed to move downloaded gen_snapshot: %v", err)
			os.Exit(1)
		}
	}

	err = ioutil.WriteFile(cachedEngineVersionPath, []byte(requiredEngineVersion), 0664)
	if err != nil {
		log.Errorf("Failed to write version file: %v", err)
		os.Exit(1)
	}
}
