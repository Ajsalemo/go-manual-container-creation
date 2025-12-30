package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func main() {
	if err := createRootFS(); err != nil {
		fmt.Println("Error creating root filesystem:", err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		run()
	default:
		panic("what??")
	}
}

// generateRandomString creates a random alphanumeric string of given length
func generateRandomString(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("length must be greater than 0")
	}

	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)

	for i := range length {
		// Generate a secure random index
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", fmt.Errorf("failed to generate random number: %v", err)
		}
		result[i] = charset[num.Int64()]
	}

	return string(result), nil
}

// Generates a random 8 character limited hostname for the container
func generateHostname() string {
	host, err := generateRandomString(8)
	if err != nil {
		fmt.Println("Error generating random hostname:", err)
		os.Exit(1)
	}

	fmt.Println("Hostname for container is " + host)
	return host
}

var hostname = generateHostname()

func run() {
	fmt.Printf("Running %v as PID %d\n", os.Args[2:], os.Getpid())
	// ---------------------------- //
	// os.Args[2] is the first command after the "run" argument
	// os.Args[3:] are the rest of the arguments
	// ---------------------------- //
	cmd := exec.Command(os.Args[2], os.Args[3:]...)
	fmt.Println("Executing " + os.Args[2] + " " + strings.Join(os.Args[3:], " "))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID,
	}
	// Change root to the new root filesystem created earlier
	if err := syscall.Chroot(fmt.Sprintf("/tmp/%s/rootfs", hostname)); err != nil {
		fmt.Println("Error changing root:", err)
		os.Exit(1)
	}
	// Change working directory after changing the root.
	if err := os.Chdir("/"); err != nil {
		fmt.Println("Error changing working directory:", err)
		os.Exit(1)
	}
	// Set hostname of the new UTS namespace
	// https://www.man7.org/linux/man-pages/man7/uts_namespaces.7.html
	// eg. HOSTNAME for the container
	if err := syscall.Sethostname([]byte(hostname)); err != nil {
		fmt.Println("Error setting hostname:", err)
		os.Exit(1)
	}

	if err := cmd.Run(); err != nil {
		fmt.Println("Error running "+os.Args[2]+" "+strings.Join(os.Args[3:], " "), err)
		os.Exit(1)
	}
}

// untar the downloaded alpine minimal filesystem since its downloaded as a tar.gz file
func untar(src, dest string) error {
	fmt.Printf("Extracting %s to %s\n", src, dest)
	// Open the source file
	file, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var tarReader *tar.Reader
	var fileReader io.Reader = file

	// If it's a .gz file, wrap with gzip reader
	if strings.HasSuffix(src, ".gz") || strings.HasSuffix(src, ".tgz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()
		fileReader = gzReader
	}

	tarReader = tar.NewReader(fileReader)

	// Iterate through the files in the archive
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return fmt.Errorf("error reading tar entry: %w", err)
		}

		targetPath := filepath.Join(dest, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory if it doesn't exist
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			outFile.Close()
		case tar.TypeSymlink:
			// Handle symbolic links
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory for symlink: %w", err)
			}
			// Remove any existing file/symlink at target path
			os.Remove(targetPath)
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}
		case tar.TypeLink:
			// Handle hard links
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory for hardlink: %w", err)
			}
			linkTarget := filepath.Join(dest, header.Linkname)
			os.Remove(targetPath)
			if err := os.Link(linkTarget, targetPath); err != nil {
				return fmt.Errorf("failed to create hardlink: %w", err)
			}
		default:
			// This is a no-op for now. May set this to a debug line in the future. Otherwise this may create a lot of noise
			// Skip unsupported types (eg. executables)
			// fmt.Printf("Skipping unsupported type: %c in %s\n", header.Typeflag, header.Name)
		}
	}

	fmt.Printf("Extraction completed successfully \n")
	fmt.Printf("Deleting initial tar.gz file")
	// Make sure the initial file exists/is still accessible before deleting it
	if _, err := os.Stat(src); os.IsNotExist(err) {
		fmt.Printf("File %q does not exist.\n", src)
		return err
	}
	// Attempt to delete the file
	ferr := os.Remove(src)
	if ferr != nil {
		fmt.Printf("Error deleting file %q: %v\n", src, err)
		return ferr
	}

	fmt.Printf("File %q deleted successfully.\n", src)

	return nil
}

// create a new root file system for the container
func createRootFS() error {
	// 1. download a minimal filesystem from alpine
	// 2. create a directory for the new root fs under /tmp/[hostname]/rootfs
	// 3. extract the downloaded filesystem into the new root fs directory
	// Create the file
	fmt.Println("Creating container root filesystem...")
	url := "https://dl-cdn.alpinelinux.org/alpine/v3.18/releases/x86_64/alpine-minirootfs-3.18.0-x86_64.tar.gz"
	filename := filepath.Base(url)
	dir := "/tmp/" + hostname + "/rootfs"
	// Build the full path in an OS-safe way
	fullPath := filepath.Join(dir, filename)
	fmt.Printf("Created rootfs directory for container with hostname " + hostname + " at directory location " + dir + "\n")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	// Create the file under /tmp/[hostname]/rootfs
	out, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	defer out.Close()
	// Get the data
	fmt.Println("Downloading minimal filesystem from Alpine Linux from " + url + "\n")
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()
	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}
	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to save file: %w", err)
	}
	fmt.Println("Alpine filesystem file downloaded successfully")
	fmt.Println("Starting extraction of the .tar.gz file...")
	// untar the downloaded file into /tmp/[hostname]/rootfs
	if err := untar(fullPath, dir); err != nil {
		return fmt.Errorf("failed to extract tar file: %w", err)
	}

	return nil
}
