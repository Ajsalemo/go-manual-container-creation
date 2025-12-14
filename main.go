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
	case "hey":
		child()
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

func run() {
	fmt.Printf("Running %v as PID %d\n", os.Args[2:], os.Getpid())
	// /proc/self/exe - https://www.man7.org/linux/man-pages/man5/proc.5.html
	// /proc/self/exe is a symlink that points to the exe of the current process
	fmt.Println(os.Args[2:])
	// ---------------------------- //
	// append() does the following
	// "hey" is the switch case from main()
	// os.Args[2:] is the command we want to run inside the new namespace
	// ---------------------------- //
	cmd := exec.Command("/proc/self/exe", append([]string{"hey"}, os.Args[2:]...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID,
	}

	if err := cmd.Run(); err != nil {
		fmt.Println("Error running /proc/self/exe command:", err)
		os.Exit(1)
	}
}

func generateHostname() string {
	host, err := generateRandomString(8)
	if err != nil {
		fmt.Println("Error generating random hostname:", err)
		os.Exit(1)
	}
	return host
}

var hostname = generateHostname()

func child() {
	fmt.Printf("Running %v as PID %d\n", os.Args[2:], os.Getpid())
	// Set hostname of the new UTS namespace
	// https://www.man7.org/linux/man-pages/man7/uts_namespaces.7.html - UTS namespace contains hostname and domain name
	if err := syscall.Sethostname([]byte(hostname)); err != nil {
		fmt.Println("Error setting hostname:", err)
		os.Exit(1)
	}

	cmd := exec.Command(os.Args[2], os.Args[3:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Println("Error running the child command:", err)
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
		default:
			// This is a no-op for now. May set this to a debug line in the future. Otherwise this may create a lot of noise
			// Skip unsupported types (eg. executables)
			// fmt.Printf("Skipping unsupported type: %c in %s\n", header.Typeflag, header.Name)
		}
	}

	fmt.Printf("Extraction completed successfully")
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
