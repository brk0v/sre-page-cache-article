package main

import (
	"fmt"
	"log"
	"os"
	"syscall"
	"unsafe"
)

var (
	pageSize = int64(syscall.Getpagesize())
	mode     = os.FileMode(0600)
)

func main() {
	path := "/var/tmp/file1.db"

	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NOATIME, mode)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	stat, err := os.Lstat(path)
	if err != nil {
		log.Fatal(err)
	}
	size := stat.Size()
	pages := size / pageSize

	mm, err := syscall.Mmap(int(file.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	defer syscall.Munmap(mm)

	mmPtr := uintptr(unsafe.Pointer(&mm[0]))
	cached := make([]byte, pages)

	sizePtr := uintptr(size)
	cachedPtr := uintptr(unsafe.Pointer(&cached[0]))

	ret, _, err := syscall.Syscall(syscall.SYS_MINCORE, mmPtr, sizePtr, cachedPtr)
	if ret != 0 {
		log.Fatal("syscall SYS_MINCORE failed: %v", err)
	}

	n := 0
	for _, p := range cached {
		if p%2 == 1 {
			n++
		}
	}

	fmt.Printf("Resident Pages: %d/%d  %d/%d\n", n, pages, n*int(pageSize), size)
}
