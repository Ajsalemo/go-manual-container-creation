package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func main() {
	switch os.Args[1] {
	case "run":
		run()
	case "hey":
		hey2()
	default:
		panic("what??")
	}
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

func hey2() {
	fmt.Println("hey there!")
}

// func child() {
// 	fmt.Printf("Running %v as PID %d\n", os.Args[2:], os.Getpid())

// 	// Set hostname of the new UTS namespace
// 	if err := syscall.Sethostname([]byte("HMcontainer")); err != nil {
// 		fmt.Println("Error setting hostname:", err)
// 		os.Exit(1)
// 	}

// 	cmd := exec.Command(os.Args[2], os.Args[3:]...)
// 	cmd.Stdin = os.Stdin
// 	cmd.Stdout = os.Stdout
// 	cmd.Stderr = os.Stderr

// 	if err := cmd.Run(); err != nil {
// 		fmt.Println("Error running the child command:", err)
// 		os.Exit(1)
// 	}
// }
