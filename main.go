package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func main() {
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

func child() {
	fmt.Printf("Running %v as PID %d\n", os.Args[2:], os.Getpid())
	// Set hostname generation to a length of 8
	hostname, err := generateRandomString(8)
	if err != nil {
		fmt.Println("Error generating random hostname:", err)
		os.Exit(1)
	}
	//
	fmt.Println("Generated random hostname:", hostname)
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
